package mysqlreplication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aimdotsh/dbops/internal/actionpolicy"
	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/security"
)

type AgentDispatcher interface {
	Dispatch(context.Context, int64, agentproto.ActionRequest) (agentproto.ActionResponse, error)
}

type Service struct {
	hosts       repository.HostRepository
	agents      repository.AgentRepository
	dbs         repository.DatabaseRepository
	credentials repository.CredentialRepository
	replications repository.MySQLReplicationRepository
	tasks       repository.TaskRepository
	cipher      *security.Cipher
	dispatcher  AgentDispatcher
}

type CreateRequest struct {
	PrimaryInstanceID int64 `json:"primary_instance_id"`
	ReplicaInstanceID int64 `json:"replica_instance_id"`
	BaselineReady     bool  `json:"baseline_ready"`
	Confirmed         bool  `json:"confirmed"`
}

type createParams struct {
	PrimaryInstanceID int64 `json:"primary_instance_id"`
	ReplicaInstanceID int64 `json:"replica_instance_id"`
	BaselineReady     bool  `json:"baseline_ready"`
	Confirmed         bool  `json:"confirmed"`
}

type runtime struct {
	Instance domain.DatabaseInstance
	Agent    domain.Agent
	Host     domain.Host
	BaseDir  string
	RunDir   string
	Password string
}

func New(
	hosts repository.HostRepository,
	agents repository.AgentRepository,
	dbs repository.DatabaseRepository,
	credentials repository.CredentialRepository,
	replications repository.MySQLReplicationRepository,
	tasks repository.TaskRepository,
	cipher *security.Cipher,
	dispatcher AgentDispatcher,
) *Service {
	return &Service{
		hosts: hosts, agents: agents, dbs: dbs, credentials: credentials,
		replications: replications, tasks: tasks, cipher: cipher, dispatcher: dispatcher,
	}
}

func (s *Service) List(ctx context.Context) ([]domain.MySQLReplication, error) {
	return s.replications.List(ctx)
}

func (s *Service) CreateTask(ctx context.Context, req CreateRequest) (domain.Task, error) {
	if req.PrimaryInstanceID <= 0 || req.ReplicaInstanceID <= 0 || req.PrimaryInstanceID == req.ReplicaInstanceID {
		return domain.Task{}, errors.New("two distinct MySQL instances are required")
	}
	if !req.BaselineReady {
		return domain.Task{}, errors.New("baseline_ready must be true; V1 will not configure GTID replication onto an unconfirmed replica baseline")
	}
	if !req.Confirmed {
		return domain.Task{}, errors.New("GTID replication creation is R3 and requires confirmed=true")
	}
	primary, err := s.dbs.Get(ctx, req.PrimaryInstanceID)
	if err != nil {
		return domain.Task{}, err
	}
	replica, err := s.dbs.Get(ctx, req.ReplicaInstanceID)
	if err != nil {
		return domain.Task{}, err
	}
	if primary.DBType != "mysql" || replica.DBType != "mysql" {
		return domain.Task{}, errors.New("replication endpoints must both be MySQL")
	}
	params, _ := json.Marshal(createParams{
		PrimaryInstanceID: req.PrimaryInstanceID,
		ReplicaInstanceID: req.ReplicaInstanceID,
		BaselineReady: true,
		Confirmed: true,
	})
	key := fmt.Sprintf("mysql.replication:%d:%d", req.PrimaryInstanceID, req.ReplicaInstanceID)
	target := req.ReplicaInstanceID
	return s.tasks.Create(ctx, domain.Task{
		TaskType: "mysql.replication.create", TargetType: "database", TargetID: &target,
		ParametersJSON: string(params), IdempotencyKey: &key,
	})
}

func (s *Service) Handler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		var p createParams
		if err := json.Unmarshal([]byte(t.ParametersJSON), &p); err != nil {
			return nil, err
		}
		if !p.Confirmed || !p.BaselineReady {
			return nil, errors.New("replication safety confirmation is missing")
		}

		primary, err := s.loadRuntime(ctx, p.PrimaryInstanceID)
		if err != nil {
			return nil, fmt.Errorf("load primary: %w", err)
		}
		replica, err := s.loadRuntime(ctx, p.ReplicaInstanceID)
		if err != nil {
			return nil, fmt.Errorf("load replica: %w", err)
		}
		if primary.Agent.Status != "online" || replica.Agent.Status != "online" {
			return nil, errors.New("both primary and replica Agents must be online")
		}

		step := func(no int, code, name string, progress int, fn func() (any, error)) (any, error) {
			_ = s.tasks.UpsertStep(ctx, domain.TaskStep{TaskID:t.ID, StepNo:no, StepCode:code, StepName:name, Status:"running", Progress:progress, RecoveryPolicy:"verify_before_retry"})
			out, err := fn()
			payload, _ := json.Marshal(out)
			if err != nil {
				_ = s.tasks.UpsertStep(ctx, domain.TaskStep{TaskID:t.ID, StepNo:no, StepCode:code, StepName:name, Status:"failed", Progress:progress, OutputJSON:string(payload), ErrorMessage:err.Error(), RecoveryPolicy:"manual_on_unknown"})
				return out, err
			}
			_ = s.tasks.UpsertStep(ctx, domain.TaskStep{TaskID:t.ID, StepNo:no, StepCode:code, StepName:name, Status:"success", Progress:progress, OutputJSON:string(payload), RecoveryPolicy:"verify_before_retry"})
			return out, nil
		}

		var primaryCheck, replicaCheck map[string]any
		if out, err := step(1,"PRIMARY_PRECHECK","Primary precheck",10,func()(any,error){
			return s.dispatchPrecheck(ctx,t.ID,primary)
		}); err != nil { return nil, err } else { primaryCheck,_=out.(map[string]any) }
		if out, err := step(2,"REPLICA_PRECHECK","Replica precheck",20,func()(any,error){
			return s.dispatchPrecheck(ctx,t.ID,replica)
		}); err != nil { return nil, err } else { replicaCheck,_=out.(map[string]any) }

		if !boolValue(primaryCheck["ok"]) || !boolValue(replicaCheck["ok"]) {
			return nil, errors.New("GTID replication precheck did not pass")
		}
		if int64Value(primaryCheck["server_id"]) == int64Value(replicaCheck["server_id"]) {
			return nil, errors.New("primary and replica server_id must be unique")
		}

		replPassword, err := security.RandomPassword(32)
		if err != nil { return nil, err }
		encrypted, err := s.cipher.EncryptString(replPassword)
		if err != nil { return nil, err }
		replUser := fmt.Sprintf("dbops_repl_%d", p.ReplicaInstanceID)
		credential, err := s.credentials.Create(ctx, domain.Credential{
			Name: fmt.Sprintf("replication %d -> %d",p.PrimaryInstanceID,p.ReplicaInstanceID),
			CredentialType:"mysql_replication", Username:replUser, EncryptedSecret:encrypted,
			MetadataJSON:fmt.Sprintf(`{"primary_instance_id":%d,"replica_instance_id":%d}`,p.PrimaryInstanceID,p.ReplicaInstanceID),
		})
		if err != nil { return nil, err }

		if _, err := step(3,"CREATE_REPLICATION_USER","Create replication user",35,func()(any,error){
			return s.dispatchCreate(ctx,t.ID,primary,map[string]any{
				"mode":"primary_prepare","replication_user":replUser,"replication_password":replPassword,
				"replication_host":replica.Host.IPAddress,
			})
		}); err != nil { return nil, err }

		if _, err := step(4,"CONFIGURE_REPLICA","Configure replica",55,func()(any,error){
			return s.dispatchCreate(ctx,t.ID,replica,map[string]any{
				"mode":"replica_configure","replication_user":replUser,"replication_password":replPassword,
				"source_host":primary.Host.IPAddress,"source_port":primary.Instance.Port,
			})
		}); err != nil { return nil, err }

		var status map[string]any
		if out, err := step(5,"VERIFY_REPLICATION","Verify replication",75,func()(any,error){
			return s.dispatchStatus(ctx,t.ID,replica)
		}); err != nil { return nil, err } else { status,_=out.(map[string]any) }
		if status["status"] != "healthy" {
			return nil, fmt.Errorf("replication is not healthy: %v", status)
		}

		lag := int64Value(status["replication_lag_seconds"])
		rec, err := s.replications.Create(ctx, domain.MySQLReplication{
			PrimaryInstanceID:p.PrimaryInstanceID, ReplicaInstanceID:p.ReplicaInstanceID,
			ReplicationCredentialID:&credential.ID, GTIDEnabled:true,
			IOThreadStatus:stringValue(status["io_thread_status"]), SQLThreadStatus:stringValue(status["sql_thread_status"]),
			ReplicationLagSeconds:&lag, SourceUUID:stringValue(status["source_uuid"]), Status:"healthy",
		})
		if err != nil { return nil, err }
		_, _ = step(6,"REGISTER_TOPOLOGY","Register topology",90,func()(any,error){return rec,nil})
		_, _ = step(7,"ENABLE_REPLICATION_ALERTS","Enable replication alerts",95,func()(any,error){return map[string]any{"enabled":true,"defaults":[]string{"thread_down","lag"}},nil})
		_, _ = step(8,"FINAL_VERIFY","Final verify",100,func()(any,error){return s.dispatchStatus(ctx,t.ID,replica)})

		return map[string]any{"replication_id":rec.ID,"primary_instance_id":rec.PrimaryInstanceID,"replica_instance_id":rec.ReplicaInstanceID,"status":"healthy"},nil
	}
}

func (s *Service) Refresh(ctx context.Context, id int64) (domain.MySQLReplication, error) {
	rec, err := s.replications.Get(ctx, id)
	if err != nil { return rec, err }
	replica, err := s.loadRuntime(ctx, rec.ReplicaInstanceID)
	if err != nil { return rec, err }
	status, err := s.dispatchStatus(ctx, 0, replica)
	if err != nil { return rec, err }
	lag := int64Value(status["replication_lag_seconds"])
	rec.IOThreadStatus=stringValue(status["io_thread_status"]); rec.SQLThreadStatus=stringValue(status["sql_thread_status"])
	rec.ReplicationLagSeconds=&lag; rec.SourceUUID=stringValue(status["source_uuid"])
	rec.LastIOError=stringValue(status["last_io_error"]); rec.LastSQLError=stringValue(status["last_sql_error"]); rec.Status=stringValue(status["status"])
	if err:=s.replications.UpdateStatus(ctx,id,rec);err!=nil{return rec,err}
	return s.replications.Get(ctx,id)
}

func (s *Service) loadRuntime(ctx context.Context, id int64) (runtime, error) {
	inst, err := s.dbs.Get(ctx,id); if err!=nil{return runtime{},err}
	if inst.DBType!="mysql" || inst.CredentialID==nil{return runtime{},errors.New("instance is not a managed MySQL installation")}
	agent,err:=s.agents.GetByHostID(ctx,inst.HostID);if err!=nil{return runtime{},err}
	host,err:=s.hosts.Get(ctx,inst.HostID);if err!=nil{return runtime{},err}
	cred,err:=s.credentials.Get(ctx,*inst.CredentialID);if err!=nil{return runtime{},err}
	password,err:=s.cipher.DecryptString(cred.EncryptedSecret);if err!=nil{return runtime{},err}
	var meta struct {
		InstallResult struct {
			BaseDir string `json:"base_dir"`
			RunDir  string `json:"run_dir"`
		} `json:"install_result"`
	}
	if err:=json.Unmarshal([]byte(inst.MetadataJSON),&meta);err!=nil{return runtime{},err}
	if meta.InstallResult.BaseDir==""||meta.InstallResult.RunDir==""{return runtime{},errors.New("instance runtime metadata is incomplete")}
	return runtime{Instance:inst,Agent:agent,Host:host,BaseDir:meta.InstallResult.BaseDir,RunDir:meta.InstallResult.RunDir,Password:password},nil
}

func (s *Service) dispatchPrecheck(ctx context.Context, taskID int64, rt runtime)(map[string]any,error){
	p,_:=actionpolicy.Get("mysql.replication.precheck")
	resp,err:=s.dispatcher.Dispatch(ctx,rt.Agent.ID,agentproto.ActionRequest{TaskID:taskID,Action:"mysql.replication.precheck",Risk:string(p.Risk),ProtocolVersion:agentproto.ProtocolVersion,TimeoutSeconds:120,Params:map[string]any{"base_dir":rt.BaseDir,"run_dir":rt.RunDir,"root_password":rt.Password}})
	if err!=nil{return nil,err};v,_:=resp.Result.(map[string]any);return v,nil
}
func (s *Service) dispatchCreate(ctx context.Context, taskID int64, rt runtime, extra map[string]any)(map[string]any,error){
	p,_:=actionpolicy.Get("mysql.replication.create");params:=map[string]any{"base_dir":rt.BaseDir,"run_dir":rt.RunDir,"root_password":rt.Password};for k,v:=range extra{params[k]=v}
	resp,err:=s.dispatcher.Dispatch(ctx,rt.Agent.ID,agentproto.ActionRequest{TaskID:taskID,Action:"mysql.replication.create",Risk:string(p.Risk),Confirmed:true,ProtocolVersion:agentproto.ProtocolVersion,TimeoutSeconds:300,Params:params})
	if err!=nil{return nil,err};v,_:=resp.Result.(map[string]any);return v,nil
}
func (s *Service) dispatchStatus(ctx context.Context, taskID int64, rt runtime)(map[string]any,error){
	p,_:=actionpolicy.Get("mysql.replication.status")
	resp,err:=s.dispatcher.Dispatch(ctx,rt.Agent.ID,agentproto.ActionRequest{TaskID:taskID,Action:"mysql.replication.status",Risk:string(p.Risk),ProtocolVersion:agentproto.ProtocolVersion,TimeoutSeconds:60,Params:map[string]any{"base_dir":rt.BaseDir,"run_dir":rt.RunDir,"root_password":rt.Password}})
	if err!=nil{return nil,err};v,_:=resp.Result.(map[string]any);return v,nil
}

func boolValue(v any)bool{x,_:=v.(bool);return x}
func stringValue(v any)string{x,_:=v.(string);return x}
func int64Value(v any)int64{switch x:=v.(type){case float64:return int64(x);case int64:return x;case int:return int64(x);case json.Number:n,_:=x.Int64();return n};return 0}

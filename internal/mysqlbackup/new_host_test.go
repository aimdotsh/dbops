package mysqlbackup

import (
	"context"
	"strings"
	"testing"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/mysqlinstall"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/aimdotsh/dbops/internal/security"
)

type newHostAgents struct{ repository.AgentRepository }

func (newHostAgents) Get(_ context.Context, id int64) (domain.Agent, error) {
	return domain.Agent{ID: id, HostID: &id, Status: "online", Architecture: "arm64"}, nil
}
func (a newHostAgents) GetByHostID(ctx context.Context, id int64) (domain.Agent, error) {
	return a.Get(ctx, id)
}

type newHostDatabases struct{ repository.DatabaseRepository }

func (newHostDatabases) Get(context.Context, int64) (domain.DatabaseInstance, error) {
	id := int64(1)
	return domain.DatabaseInstance{ID: 1, HostID: 1, Version: "8.0.test", DBType: "mysql", CredentialID: &id, MetadataJSON: `{"install_result":{"base_dir":"/base","run_dir":"/run"}}`}, nil
}

type newHostCredentials struct {
	repository.CredentialRepository
	secret string
}

func (c newHostCredentials) Get(context.Context, int64) (domain.Credential, error) {
	return domain.Credential{EncryptedSecret: c.secret}, nil
}

type newHostPackages struct {
	repository.SoftwarePackageRepository
}

func (newHostPackages) Get(context.Context, int64) (domain.SoftwarePackage, error) {
	return domain.SoftwarePackage{SoftwareName: "mysql", Status: "available", Version: "8.0.test", OSFamily: "linux", Architecture: "arm64", PackageType: "tar.gz"}, nil
}

func TestNewHostRequiresDifferentHostButNoConfirmation(t *testing.T) {
	cipher, err := security.NewCipher("new-host-test-key-123")
	if err != nil {
		t.Fatal(err)
	}
	secret, err := cipher.EncryptString("test-password")
	if err != nil {
		t.Fatal(err)
	}
	agents := newHostAgents{}
	s := &Service{agents: agents, dbs: newHostDatabases{}, credentials: newHostCredentials{secret: secret}, cipher: cipher, backups: restoreBackups{}, tasks: restoreTasks{parameters: `{"databases":["orders"]}`}}
	s.installer = mysqlinstall.New(agents, nil, newHostPackages{}, nil, nil, nil, nil, nil, nil, false)
	req := NewHostRequest{BackupID: 1, InstallRequest: mysqlinstall.InstallRequest{AgentID: 2, PackageID: 1, Port: 13312}}
	_, _, normalized, err := s.validateNewHost(context.Background(), req)
	if err != nil || normalized.DataDir != "/opt/dbops/mysql/13312/data" {
		t.Fatalf("new host rejected: %v", err)
	}
	req.AgentID = 1
	if _, _, _, err := s.validateNewHost(context.Background(), req); err == nil || !strings.Contains(err.Error(), "different host") {
		t.Fatalf("same host bypassed confirmation: %v", err)
	}
}

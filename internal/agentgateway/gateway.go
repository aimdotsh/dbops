package agentgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Gateway struct {
	agents           repository.AgentRepository
	tasks            repository.TaskRepository
	logger           *slog.Logger
	heartbeatTimeout time.Duration

	mu      sync.RWMutex
	clients map[int64]*client
}

type client struct {
	agent     domain.Agent
	conn      *websocket.Conn
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan agentproto.ActionResponse
	done      chan struct{}
	closeOnce sync.Once
	lastSeen  atomic.Int64
}

func New(agents repository.AgentRepository, tasks repository.TaskRepository, logger *slog.Logger, heartbeatTimeoutSeconds int) *Gateway {
	if heartbeatTimeoutSeconds <= 0 {
		heartbeatTimeoutSeconds = 90
	}
	return &Gateway{
		agents:           agents,
		tasks:            tasks,
		logger:           logger,
		heartbeatTimeout: time.Duration(heartbeatTimeoutSeconds) * time.Second,
		clients:          make(map[int64]*client),
	}
}

func (g *Gateway) Start(ctx context.Context) {
	interval := g.heartbeatTimeout / 3
	if interval < 5*time.Second {
		interval = 5 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				g.closeAll()
				return
			case <-ticker.C:
				g.closeStale()
			}
		}
	}()
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	upgrader := websocket.Upgrader{
		HandshakeTimeout: 10 * time.Second,
		CheckOrigin: func(*http.Request) bool {
			return true
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		g.logger.Warn("agent websocket upgrade failed", "error", err)
		return
	}
	conn.SetReadLimit(2 << 20)
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	var env agentproto.Envelope
	if err := conn.ReadJSON(&env); err != nil {
		_ = conn.Close()
		return
	}
	if env.Type != "hello" {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "hello required"), time.Now().Add(time.Second))
		_ = conn.Close()
		return
	}

	var hello agentproto.Hello
	if err := json.Unmarshal(env.Data, &hello); err != nil || hello.AgentUUID == "" {
		_ = conn.Close()
		return
	}
	if hello.ProtocolVersion != agentproto.ProtocolVersion {
		_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "protocol mismatch"), time.Now().Add(time.Second))
		_ = conn.Close()
		return
	}

	agent, err := g.agents.Upsert(r.Context(), domain.Agent{
		AgentUUID:    hello.AgentUUID,
		Version:      hello.Version,
		Architecture: hello.Architecture,
		Status:       "online",
	})
	if err != nil {
		g.logger.Error("register agent", "uuid", hello.AgentUUID, "error", err)
		_ = conn.Close()
		return
	}
	if _, err := g.agents.BindHostByIdentity(r.Context(), hello.AgentUUID, hello.Hostname, hello.IPAddress); err != nil {
		g.logger.Warn("bind agent host", "uuid", hello.AgentUUID, "error", err)
	}
	if refreshed, err := g.agents.GetByUUID(r.Context(), hello.AgentUUID); err == nil {
		agent = refreshed
	}

	c := &client{
		agent:   agent,
		conn:    conn,
		pending: make(map[string]chan agentproto.ActionResponse),
		done:    make(chan struct{}),
	}
	c.lastSeen.Store(time.Now().UnixNano())
	_ = conn.SetReadDeadline(time.Time{})

	g.register(c)
	g.logger.Info("agent connected", "agent_id", agent.ID, "uuid", agent.AgentUUID, "host_id", agent.HostID)
	g.readLoop(r.Context(), c)
	g.unregister(c)
}

func (g *Gateway) Dispatch(ctx context.Context, agentID int64, req agentproto.ActionRequest) (agentproto.ActionResponse, error) {
	c := g.get(agentID)
	if c == nil {
		return agentproto.ActionResponse{}, fmt.Errorf("agent %d offline", agentID)
	}

	if req.RequestID == "" {
		req.RequestID = uuid.NewString()
	}
	if req.ProtocolVersion == "" {
		req.ProtocolVersion = agentproto.ProtocolVersion
	}
	if req.TimeoutSeconds <= 0 {
		req.TimeoutSeconds = 30
	}

	responses := make(chan agentproto.ActionResponse, 32)
	c.pendingMu.Lock()
	c.pending[req.RequestID] = responses
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, req.RequestID)
		c.pendingMu.Unlock()
	}()

	if err := c.writeEnvelope("action_request", req); err != nil {
		return agentproto.ActionResponse{}, err
	}

	timer := time.NewTimer(time.Duration(req.TimeoutSeconds) * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return agentproto.ActionResponse{}, ctx.Err()
		case <-c.done:
			return agentproto.ActionResponse{}, errors.New("agent disconnected")
		case <-timer.C:
			return agentproto.ActionResponse{}, errors.New("agent action timeout")
		case resp := <-responses:
			switch resp.Status {
			case "success":
				return resp, nil
			case "failed", "cancelled", "timeout":
				if resp.Error != "" {
					return resp, errors.New(resp.Error)
				}
				if resp.Message != "" {
					return resp, errors.New(resp.Message)
				}
				return resp, fmt.Errorf("agent action %s", resp.Status)
			}
		}
	}
}

func (g *Gateway) readLoop(ctx context.Context, c *client) {
	defer c.close()
	for {
		var env agentproto.Envelope
		if err := c.conn.ReadJSON(&env); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				g.logger.Debug("agent read ended", "agent_id", c.agent.ID, "error", err)
			}
			return
		}
		c.lastSeen.Store(time.Now().UnixNano())

		switch env.Type {
		case "heartbeat":
			var hb agentproto.Heartbeat
			if err := json.Unmarshal(env.Data, &hb); err != nil {
				continue
			}
			_ = g.agents.Heartbeat(ctx, c.agent.AgentUUID, hb.Capabilities)
		case "action_response":
			var resp agentproto.ActionResponse
			if err := json.Unmarshal(env.Data, &resp); err != nil {
				continue
			}
			g.recordResponse(ctx, resp)
			c.pendingMu.Lock()
			ch := c.pending[resp.RequestID]
			c.pendingMu.Unlock()
			if ch != nil {
				select {
				case ch <- resp:
				default:
					g.logger.Warn("dropping agent response because pending channel is full", "task_id", resp.TaskID)
				}
			}
		}
	}
}

func (g *Gateway) recordResponse(ctx context.Context, resp agentproto.ActionResponse) {
	if resp.TaskID <= 0 {
		return
	}
	_ = g.tasks.UpdateProgress(ctx, resp.TaskID, resp.Progress)

	payload, _ := json.Marshal(resp.Result)
	_ = g.tasks.AddEvent(ctx, domain.TaskEvent{
		TaskID:      resp.TaskID,
		EventType:   "agent_response",
		StepCode:    resp.Step.Code,
		Level:       responseLevel(resp.Status),
		Message:     resp.Message,
		PayloadJSON: string(payload),
	})

	if resp.Step.Code != "" {
		status := "running"
		if resp.Status == "success" {
			status = "success"
		} else if resp.Status == "failed" || resp.Status == "cancelled" || resp.Status == "timeout" {
			status = "failed"
		}
		_ = g.tasks.UpsertStep(ctx, domain.TaskStep{
			TaskID:         resp.TaskID,
			StepNo:         1,
			StepCode:       resp.Step.Code,
			StepName:       resp.Step.Name,
			Status:         status,
			Progress:       resp.Progress,
			OutputJSON:     string(payload),
			ErrorMessage:   resp.Error,
			RecoveryPolicy: "verify_before_retry",
		})
	}
}

func responseLevel(status string) string {
	if status == "failed" || status == "timeout" {
		return "ERROR"
	}
	return "INFO"
}

func (g *Gateway) register(c *client) {
	g.mu.Lock()
	old := g.clients[c.agent.ID]
	g.clients[c.agent.ID] = c
	g.mu.Unlock()
	if old != nil {
		old.close()
		_ = old.conn.Close()
	}
}

func (g *Gateway) unregister(c *client) {
	g.mu.Lock()
	if current := g.clients[c.agent.ID]; current == c {
		delete(g.clients, c.agent.ID)
	}
	g.mu.Unlock()
	c.close()
	_ = c.conn.Close()
	_ = g.agents.MarkOffline(context.Background(), c.agent.AgentUUID)
	g.logger.Info("agent disconnected", "agent_id", c.agent.ID, "uuid", c.agent.AgentUUID)
}

func (g *Gateway) get(agentID int64) *client {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.clients[agentID]
}

func (g *Gateway) closeStale() {
	now := time.Now()
	g.mu.RLock()
	clients := make([]*client, 0, len(g.clients))
	for _, c := range g.clients {
		clients = append(clients, c)
	}
	g.mu.RUnlock()

	for _, c := range clients {
		last := time.Unix(0, c.lastSeen.Load())
		if now.Sub(last) > g.heartbeatTimeout {
			g.logger.Warn("agent heartbeat timeout", "agent_id", c.agent.ID, "age", now.Sub(last))
			_ = c.conn.Close()
		}
	}
}

func (g *Gateway) closeAll() {
	g.mu.RLock()
	clients := make([]*client, 0, len(g.clients))
	for _, c := range g.clients {
		clients = append(clients, c)
	}
	g.mu.RUnlock()
	for _, c := range clients {
		_ = c.conn.Close()
	}
}

func (c *client) writeEnvelope(kind string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(agentproto.Envelope{Type: kind, Data: raw})
}

func (c *client) close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
}

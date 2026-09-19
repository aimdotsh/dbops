package agentclient

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aimdotsh/dbops/internal/agentproto"
	"github.com/gorilla/websocket"
)

const Version = "0.3.0"

type Client struct {
	cfg      Config
	agentID  string
	executor *Executor
	logger   *slog.Logger
	sem      chan struct{}
	running  atomic.Int64
	writeMu  sync.Mutex
}

func New(cfg Config, agentID string, executor *Executor, logger *slog.Logger) *Client {
	return &Client{
		cfg: cfg, agentID: agentID, executor: executor, logger: logger,
		sem: make(chan struct{}, cfg.Executor.MaxConcurrentTasks),
	}
}

func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := c.runOnce(ctx); err != nil && ctx.Err() == nil {
			c.logger.Warn("agent connection ended", "error", err, "retry_in", backoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
			if backoff > 30*time.Second {
				backoff = 30 * time.Second
			}
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	wsURL, err := websocketURL(c.cfg.Server.URL)
	if err != nil {
		return err
	}
	credential, bootstrapToken, err := LoadAuth(c.cfg)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: !c.cfg.Security.VerifyServerTLS, //nolint:gosec
		},
	}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	hostname, _ := os.Hostname()
	hello := agentproto.Hello{
		AgentUUID:       c.agentID,
		Version:         Version,
		Hostname:        hostname,
		IPAddress:       primaryIP(),
		Architecture:    runtime.GOARCH,
		ProtocolVersion: agentproto.ProtocolVersion,
		Auth: agentproto.Auth{
			BootstrapToken: bootstrapToken,
			Credential:     credential,
		},
	}
	if err := c.writeEnvelope(conn, "hello", hello); err != nil {
		return err
	}

	var registeredEnv agentproto.Envelope
	if err := conn.ReadJSON(&registeredEnv); err != nil {
		return fmt.Errorf("registration response: %w", err)
	}
	if registeredEnv.Type != "registered" {
		return fmt.Errorf("unexpected registration response %q", registeredEnv.Type)
	}
	var registration agentproto.Registration
	if err := json.Unmarshal(registeredEnv.Data, &registration); err != nil {
		return fmt.Errorf("decode registration: %w", err)
	}
	if registration.AgentUUID != c.agentID {
		return fmt.Errorf("registration identity mismatch")
	}
	if registration.Credential != "" {
		if err := SaveCredential(c.cfg, registration.Credential); err != nil {
			return fmt.Errorf("persist credential: %w", err)
		}
		c.logger.Info("agent enrollment completed", "agent_uuid", c.agentID, "agent_id", registration.AgentID)
	} else {
		c.logger.Info("connected to dbops server", "server", wsURL, "agent_uuid", c.agentID, "agent_id", registration.AgentID)
	}

	go func() {
		if err := c.heartbeatLoop(ctx, conn); err != nil && ctx.Err() == nil {
			_ = conn.Close()
		}
	}()

	for {
		var env agentproto.Envelope
		if err := conn.ReadJSON(&env); err != nil {
			return err
		}
		if env.Type != "action_request" {
			continue
		}
		var req agentproto.ActionRequest
		if err := json.Unmarshal(env.Data, &req); err != nil {
			continue
		}
		go c.handleAction(ctx, conn, req)
	}
}

func (c *Client) heartbeatLoop(ctx context.Context, conn *websocket.Conn) error {
	ticker := time.NewTicker(time.Duration(c.cfg.Agent.HeartbeatSeconds) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			hb := agentproto.Heartbeat{
				AgentUUID:    c.agentID,
				RunningTasks: int(c.running.Load()),
				Capabilities: c.executor.Capabilities(),
			}
			if err := c.writeEnvelope(conn, "heartbeat", hb); err != nil {
				return err
			}
		}
	}
}

func (c *Client) handleAction(parent context.Context, conn *websocket.Conn, req agentproto.ActionRequest) {
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-parent.Done():
		return
	}
	c.running.Add(1)
	defer c.running.Add(-1)

	defaultStep := agentproto.Step{
		No: 1,
		Code: strings.ToUpper(strings.ReplaceAll(req.Action, ".", "_")),
		Name: req.Action,
		Status: "running",
	}
	if req.Action != "mysql.install" {
		_ = c.writeEnvelope(conn, "action_response", agentproto.ActionResponse{
			RequestID: req.RequestID,
			TaskID:    req.TaskID,
			Status:    "running",
			Progress:  10,
			Step:      defaultStep,
			Message:   "action started",
		})
	}

	lastStep := defaultStep
	reporter := func(resp agentproto.ActionResponse) {
		if resp.Step.Code != "" {
			lastStep = resp.Step
		}
		resp.RequestID = req.RequestID
		resp.TaskID = req.TaskID
		if resp.Status == "" {
			resp.Status = "running"
		}
		_ = c.writeEnvelope(conn, "action_response", resp)
	}

	result, err := c.executor.ExecuteWithReporter(parent, req, reporter)
	if err != nil {
		if req.Action == "mysql.install" {
			defaultStep = lastStep
			defaultStep.Status = "failed"
		} else {
			defaultStep.Status = "failed"
		}
		_ = c.writeEnvelope(conn, "action_response", agentproto.ActionResponse{
			RequestID: req.RequestID,
			TaskID:    req.TaskID,
			Status:    "failed",
			Progress:  100,
			Step:      defaultStep,
			Message:   "action failed",
			Error:     err.Error(),
		})
		return
	}

	if req.Action == "mysql.install" {
		defaultStep = lastStep
		defaultStep.Status = "success"
	} else {
		defaultStep.Status = "success"
	}
	_ = c.writeEnvelope(conn, "action_response", agentproto.ActionResponse{
		RequestID: req.RequestID,
		TaskID:    req.TaskID,
		Status:    "success",
		Progress:  100,
		Step:      defaultStep,
		Message:   "action completed",
		Result:    result,
	})
}

func (c *Client) writeEnvelope(conn *websocket.Conn, kind string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	err = conn.WriteJSON(agentproto.Envelope{Type: kind, Data: raw})
	_ = conn.SetWriteDeadline(time.Time{})
	return err
}

func websocketURL(server string) (string, error) {
	u, err := url.Parse(server)
	if err != nil {
		return "", err
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("unsupported server scheme %q", u.Scheme)
	}
	base := strings.TrimSuffix(u.Path, "/")
	u.Path = base + "/api/v1/agent/ws"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func primaryIP() string {
	conn, err := net.DialTimeout("udp", "8.8.8.8:80", time.Second)
	if err != nil {
		return ""
	}
	defer conn.Close()
	if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
		return addr.IP.String()
	}
	return ""
}

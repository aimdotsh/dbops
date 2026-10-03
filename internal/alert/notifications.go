package alert

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

type NotificationConfig struct {
	WebhookURL      string   `yaml:"webhook_url"`
	WebhookTokenEnv string   `yaml:"webhook_token_env"`
	SMTPAddress     string   `yaml:"smtp_address"`
	SMTPUsername    string   `yaml:"smtp_username"`
	SMTPPasswordEnv string   `yaml:"smtp_password_env"`
	From            string   `yaml:"from"`
	To              []string `yaml:"to"`
	WebhookToken    string   `yaml:"-"`
	SMTPPassword    string   `yaml:"-"`
}

func (e *Engine) ConfigureNotifications(cfg NotificationConfig) { e.notifications = cfg }
func (e *Engine) enqueue(ctx context.Context, id int64, status string) error {
	for _, channel := range []string{"webhook", "email"} {
		if channel == "webhook" && e.notifications.WebhookURL == "" {
			continue
		}
		if channel == "email" && (e.notifications.SMTPAddress == "" || len(e.notifications.To) == 0) {
			continue
		}
		if _, err := e.db.ExecContext(ctx, `INSERT OR IGNORE INTO alert_outbox(alert_id,state,channel,attempts,next_attempt) VALUES(?,?,?,0,?)`, id, status, channel, time.Now().UTC().Format(time.RFC3339)); err != nil {
			return err
		}
	}
	return nil
}
func (e *Engine) DeliverPending(ctx context.Context) error {
	rows, err := e.db.QueryContext(ctx, `SELECT o.id,o.alert_id,o.state,o.channel,o.attempts,a.fingerprint,a.severity,a.message FROM alert_outbox o JOIN alert_events a ON a.id=o.alert_id WHERE o.delivered_at IS NULL AND o.next_attempt<=? AND NOT EXISTS(SELECT 1 FROM alert_silences s WHERE s.fingerprint=a.fingerprint AND s.until_time>?) ORDER BY o.id LIMIT 20`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return err
	}
	type delivery struct {
		ID, AlertID                    int64
		State, Channel                 string
		Attempts                       int
		Fingerprint, Severity, Message string
	}
	items := []delivery{}
	for rows.Next() {
		var d delivery
		if err = rows.Scan(&d.ID, &d.AlertID, &d.State, &d.Channel, &d.Attempts, &d.Fingerprint, &d.Severity, &d.Message); err != nil {
			rows.Close()
			return err
		}
		items = append(items, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, d := range items {
		payload, _ := json.Marshal(map[string]any{"notification_id": d.ID, "alert_id": d.AlertID, "status": d.State, "fingerprint": d.Fingerprint, "severity": d.Severity, "message": d.Message})
		deliveryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if d.Channel == "webhook" {
			err = sendWebhook(deliveryCtx, e.notifications, payload)
		} else {
			err = sendEmail(deliveryCtx, e.notifications, payload)
		}
		cancel()
		if err == nil {
			_, err = e.db.ExecContext(ctx, "UPDATE alert_outbox SET delivered_at=?,last_error='' WHERE id=?", time.Now().UTC().Format(time.RFC3339), d.ID)
		} else {
			// Do not persist transport errors containing URLs or credentials.
			delay := time.Minute * time.Duration(1<<min(d.Attempts, 6))
			_, err = e.db.ExecContext(ctx, "UPDATE alert_outbox SET attempts=attempts+1,next_attempt=?,last_error='delivery failed' WHERE id=?", time.Now().UTC().Add(delay).Format(time.RFC3339), d.ID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
func sendWebhook(ctx context.Context, cfg NotificationConfig, body []byte) error {
	if cfg.WebhookURL == "" {
		return errors.New("webhook disabled")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.WebhookToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.WebhookToken)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}
func sendEmail(ctx context.Context, cfg NotificationConfig, body []byte) error {
	host, _, err := net.SplitHostPort(cfg.SMTPAddress)
	if err != nil {
		return err
	}
	from, err := mail.ParseAddress(cfg.From)
	if err != nil {
		return err
	}
	to := []string{}
	for _, address := range cfg.To {
		v, err := mail.ParseAddress(address)
		if err != nil {
			return err
		}
		to = append(to, v.Address)
	}
	if len(to) == 0 {
		return errors.New("email recipients required")
	}
	conn, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", cfg.SMTPAddress)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
		return err
	}
	if cfg.SMTPUsername != "" {
		if err = client.Auth(smtp.PlainAuth("", cfg.SMTPUsername, cfg.SMTPPassword, host)); err != nil {
			return err
		}
	}
	if err = client.Mail(from.Address); err != nil {
		return err
	}
	for _, address := range to {
		if err = client.Rcpt(address); err != nil {
			return err
		}
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	message := "From: " + from.Address + "\r\nTo: " + strings.Join(to, ",") + "\r\nSubject: DBOps alert state changed\r\nMIME-Version: 1.0\r\nContent-Type: application/json; charset=utf-8\r\n\r\n" + string(body) + "\r\n"
	if _, err = io.WriteString(writer, message); err != nil {
		writer.Close()
		return err
	}
	return writer.Close()
}
func (e *Engine) Silence(ctx context.Context, id int64, duration time.Duration) error {
	if duration < time.Minute || duration > 30*24*time.Hour {
		return errors.New("silence duration must be between one minute and 30 days")
	}
	var fingerprint string
	if err := e.db.QueryRowContext(ctx, "SELECT fingerprint FROM alert_events WHERE id=?", id).Scan(&fingerprint); err != nil {
		return err
	}
	_, err := e.db.ExecContext(ctx, `INSERT INTO alert_silences(fingerprint,until_time) VALUES(?,?) ON CONFLICT(fingerprint) DO UPDATE SET until_time=excluded.until_time`, fingerprint, time.Now().UTC().Add(duration).Format(time.RFC3339))
	return err
}

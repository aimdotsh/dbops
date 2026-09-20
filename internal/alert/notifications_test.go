package alert

import (
	"context"
	"github.com/aimdotsh/dbops/internal/storage"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestNotificationRetryAndSilence(t *testing.T) {
	dir := t.TempDir()
	stores, err := storage.Open(filepath.Join(dir, "m.db"), filepath.Join(dir, "s.db"), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer stores.Close()
	if err = storage.Migrate(stores.Metadata, stores.Metrics); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing auth")
		}
		if n == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer server.Close()
	e := New(slog.Default(), true, 1, stores.Metadata, nil)
	e.ConfigureNotifications(NotificationConfig{WebhookURL: server.URL, WebhookToken: "fixture"})
	ctx := context.Background()
	if err = e.upsertFiring(ctx, "host", 1, "fixture", "P1", "down", "{}", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = e.DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("notification missing")
	}
	if err = e.DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("retry backoff ignored")
	}
	_, _ = stores.Metadata.Exec("UPDATE alert_outbox SET next_attempt='2000-01-01T00:00:00Z'")
	if err = e.Silence(ctx, 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err = e.DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("silence ignored")
	}
	_, _ = stores.Metadata.Exec("DELETE FROM alert_silences")
	if err = e.DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("retry missing")
	}
	if err = e.upsertFiring(ctx, "host", 1, "fixture", "P1", "down", "{}", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = e.DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("duplicate notification")
	}
	if err = e.resolve(ctx, "fixture", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err = e.DeliverPending(ctx); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatal("recovery notification missing")
	}
}

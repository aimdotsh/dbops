// Package platformbackup makes independently consistent SQLite snapshots.
// Secrets remain encrypted; the original master key is required after restore.
package platformbackup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aimdotsh/dbops/internal/domain"
	"github.com/aimdotsh/dbops/internal/repository"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

type Manifest struct {
	ID        string            `json:"id"`
	CreatedAt time.Time         `json:"created_at"`
	Files     map[string]string `json:"files"`
}
type Service struct {
	Metadata, Metrics *sql.DB
	Directory         string
	Tasks             repository.TaskRepository
}

func (s *Service) CreateTask(ctx context.Context) (domain.Task, error) {
	key := "platform.backup"
	return s.Tasks.Create(ctx, domain.Task{TaskType: key, IdempotencyKey: &key, ParametersJSON: "{}"})
}
func (s *Service) Handler() func(context.Context, domain.Task) (any, error) {
	return func(ctx context.Context, t domain.Task) (any, error) {
		m, err := s.Create(ctx)
		state := "success"
		message := ""
		if err != nil {
			state = "failed"
			message = err.Error()
		}
		if e := s.Tasks.UpsertStep(ctx, domain.TaskStep{TaskID: t.ID, StepNo: 1, StepCode: "SQLITE_SNAPSHOT", StepName: "Snapshot and integrity verification", Status: state, Progress: 100, ErrorMessage: message, RecoveryPolicy: "safe_retry"}); e != nil && err == nil {
			err = e
		}
		return m, err
	}
}
func (s *Service) Create(ctx context.Context) (Manifest, error) {
	m := Manifest{ID: time.Now().UTC().Format("20060102T150405Z") + "-" + uuid.NewString(), CreatedAt: time.Now().UTC(), Files: map[string]string{}}
	if err := os.MkdirAll(s.Directory, 0700); err != nil {
		return m, err
	}
	tmp, err := os.MkdirTemp(s.Directory, ".snapshot-")
	if err != nil {
		return m, err
	}
	defer os.RemoveAll(tmp)
	for name, db := range map[string]*sql.DB{"dbops.db": s.Metadata, "metrics.db": s.Metrics} {
		path := filepath.Join(tmp, name)
		if _, err := db.ExecContext(ctx, "VACUUM INTO ?", path); err != nil {
			return m, err
		}
		if err := os.Chmod(path, 0600); err != nil {
			return m, err
		}
		if err := check(ctx, path); err != nil {
			return m, err
		}
		sum, err := checksum(path)
		if err != nil {
			return m, err
		}
		m.Files[name] = sum
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return m, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "manifest.json"), raw, 0600); err != nil {
		return m, err
	}
	return m, os.Rename(tmp, filepath.Join(s.Directory, m.ID))
}
func (s *Service) List(ctx context.Context) ([]Manifest, error) {
	entries, err := os.ReadDir(s.Directory)
	if os.IsNotExist(err) {
		return []Manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Manifest{}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(s.Directory, e.Name(), "manifest.json"))
		if err != nil {
			return nil, err
		}
		var m Manifest
		if err = json.Unmarshal(raw, &m); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

// Restore only creates a new directory. It never replaces a running platform.
func Restore(ctx context.Context, source, destination string) error {
	if _, err := os.Lstat(destination); !os.IsNotExist(err) {
		return errors.New("restore destination must not exist")
	}
	raw, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		return err
	}
	var m Manifest
	if err = json.Unmarshal(raw, &m); err != nil {
		return err
	}
	if len(m.Files) != 2 {
		return errors.New("snapshot must contain both SQLite databases")
	}
	for _, name := range []string{"dbops.db", "metrics.db"} {
		info, err := os.Lstat(filepath.Join(source, name))
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("snapshot files must be regular files")
		}
		got, err := checksum(filepath.Join(source, name))
		if err != nil {
			return err
		}
		if got != m.Files[name] {
			return fmt.Errorf("snapshot checksum mismatch: %s", name)
		}
		if err = check(ctx, filepath.Join(source, name)); err != nil {
			return err
		}
	}
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(destination), ".restore-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, name := range []string{"dbops.db", "metrics.db"} {
		src, err := os.Open(filepath.Join(source, name))
		if err != nil {
			return err
		}
		dst, err := os.OpenFile(filepath.Join(tmp, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(dst, src)
		src.Close()
		syncErr := dst.Sync()
		closeErr := dst.Close()
		if err != nil {
			return err
		}
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	// Reserve a fresh destination exclusively; never replace existing data.
	if err = os.Mkdir(destination, 0700); err != nil {
		return err
	}
	for _, name := range []string{"dbops.db", "metrics.db"} {
		if err = os.Rename(filepath.Join(tmp, name), filepath.Join(destination, name)); err != nil {
			for _, owned := range []string{"dbops.db", "metrics.db"} {
				_ = os.Remove(filepath.Join(destination, owned))
			}
			_ = os.Remove(destination)
			return err
		}
	}

	return nil
}
func checksum(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func check(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&immutable=1")
	if err != nil {
		return err
	}
	defer db.Close()
	var result string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("snapshot integrity: %s", result)
	}
	return nil
}

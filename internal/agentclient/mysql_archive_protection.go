package agentclient

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func checkArchiveThreads(ctx context.Context, baseDir, runDir, password string, limit int) error {
	out, err := runMySQLQuery(ctx, baseDir, runDir, password, "SHOW GLOBAL STATUS LIKE 'Threads_running';", false)
	if err != nil {
		return fmt.Errorf("archive load check failed: %w", err)
	}
	parts := strings.Fields(out)
	if len(parts) != 2 || parts[0] != "Threads_running" {
		return errors.New("cannot read Threads_running")
	}
	count, err := strconv.Atoi(parts[1])
	if err != nil {
		return errors.New("invalid Threads_running value")
	}
	if count > limit {
		return fmt.Errorf("Threads_running %d exceeds limit %d", count, limit)
	}
	return nil
}

func watchArchiveThreads(ctx context.Context, cancel context.CancelFunc, baseDir, runDir, password string, limit int, failures chan<- error) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			probeCtx, stop := context.WithTimeout(ctx, 15*time.Second)
			err := checkArchiveThreads(probeCtx, baseDir, runDir, password, limit)
			stop()
			if err != nil && ctx.Err() == nil {
				select {
				case failures <- err:
				default:
				}
				cancel()
				return
			}
		}
	}
}

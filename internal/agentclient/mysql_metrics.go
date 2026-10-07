package agentclient

import (
	"context"
	"strconv"
	"strings"
)

func mysqlMetrics(ctx context.Context, params map[string]any) (map[string]any, error) {
	base, run, password, err := mysqlRuntimeParams(params)
	if err != nil {
		return nil, err
	}
	out, err := runMySQLQuery(ctx, base, run, password, "SHOW GLOBAL STATUS WHERE Variable_name IN ('Threads_connected','Threads_running','Questions','Com_commit','Com_rollback','Uptime','Innodb_buffer_pool_pages_total','Innodb_buffer_pool_pages_free');", false)
	if err != nil {
		return nil, err
	}
	result := map[string]any{"up": 1}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		f := strings.Split(line, "\t")
		if len(f) == 2 {
			value, err := strconv.ParseFloat(f[1], 64)
			if err == nil {
				result[strings.ToLower(f[0])] = value
			}
		}
	}
	return result, nil
}

package agentclient

import (
	"context"
	"fmt"
	"strings"
)

func oracleDataGuardStatus(ctx context.Context, workDir string, params map[string]any) (map[string]any, error) {
	out, err := runOracleSQL(ctx, workDir, params, `
SELECT 'ROLE|'||database_role||'|'||open_mode FROM v$database;
SELECT 'LAG|'||name||'|'||value||'|'||unit
FROM v$dataguard_stats
WHERE name IN ('transport lag','apply lag')
ORDER BY name;
SELECT 'PROC|'||process||'|'||status||'|'||thread#||'|'||sequence#
FROM v$managed_standby
WHERE process IN ('MRP0','RFS')
ORDER BY process;`)
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"database_role": "", "open_mode": "",
		"transport_lag": "", "apply_lag": "",
		"processes": []map[string]any{},
	}
	var processes []map[string]any
	for _, line := range nonEmptyLines(out) {
		f := strings.Split(line, "|")
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "ROLE":
			if len(f) >= 3 {
				result["database_role"] = f[1]
				result["open_mode"] = f[2]
			}
		case "LAG":
			if len(f) >= 4 {
				key := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(f[1])), " ", "_")
				result[key] = strings.TrimSpace(f[2])
				result[key+"_unit"] = strings.TrimSpace(f[3])
			}
		case "PROC":
			if len(f) >= 5 {
				processes = append(processes, map[string]any{
					"process": f[1], "status": f[2],
					"thread": parseInt64(f[3]), "sequence": parseInt64(f[4]),
				})
			}
		}
	}
	result["processes"] = processes
	if result["database_role"] == "" {
		return nil, fmt.Errorf("Data Guard role query returned no database role")
	}
	return result, nil
}

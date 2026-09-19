package agentclient

import (
	"bufio"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func collectHostMetrics() map[string]any {
	out := map[string]any{
		"cpu_cores": runtime.NumCPU(),
	}
	if b, err := os.ReadFile("/proc/loadavg"); err == nil {
		fields := strings.Fields(string(b))
		if len(fields) >= 3 {
			if v, err := strconv.ParseFloat(fields[0], 64); err == nil {
				out["load_1"] = v
			}
			if v, err := strconv.ParseFloat(fields[1], 64); err == nil {
				out["load_5"] = v
			}
			if v, err := strconv.ParseFloat(fields[2], 64); err == nil {
				out["load_15"] = v
			}
		}
	}

	var memTotal, memAvailable uint64
	if f, err := os.Open("/proc/meminfo"); err == nil {
		defer f.Close()
		s := bufio.NewScanner(f)
		for s.Scan() {
			fields := strings.Fields(s.Text())
			if len(fields) < 2 {
				continue
			}
			switch fields[0] {
			case "MemTotal:":
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				memTotal = kb * 1024
			case "MemAvailable:":
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				memAvailable = kb * 1024
			}
		}
	}
	if memTotal > 0 {
		out["memory_total_bytes"] = memTotal
		out["memory_available_bytes"] = memAvailable
		out["memory_used_pct"] = float64(memTotal-memAvailable) * 100 / float64(memTotal)
	}

	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err == nil && st.Blocks > 0 {
		total := uint64(st.Blocks) * uint64(st.Bsize)
		free := uint64(st.Bavail) * uint64(st.Bsize)
		out["root_total_bytes"] = total
		out["root_free_bytes"] = free
		out["root_used_pct"] = float64(total-free) * 100 / float64(total)
	}
	return out
}

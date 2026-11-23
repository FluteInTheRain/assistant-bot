package ociwatch

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// agoPhrase renders "just now" or "X ago" for the /status header lines.
func agoPhrase(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	return formatAgo(d) + " ago"
}

// statusRow is one line of the instances block in /status.
type statusRow struct {
	name string
	note string
}

// StatusText renders the /status reply from the latest snapshot — it never
// calls OCI live (design 5.4). Expected shape:
//
//	side-projects · up 1d 4h · checked 2 min ago
//	disk 31% · mem avail 72% · load 0.12 (4 cores)
//	hunter: active · heartbeat 12 min ago
//	instances: micro-spare   not yet
//	           micro-monitor not yet
//	           side-projects RUNNING
//	OCI API: ok
func (w *Watcher) StatusText() string {
	s := w.status
	if s.CheckedAt.IsZero() {
		return "No check has completed yet."
	}
	now := w.now()

	var b strings.Builder
	host := w.Host
	if host == "" {
		host = "host"
	}
	fmt.Fprintf(&b, "%s · up %s · checked %s\n", host, s.HostUp, agoPhrase(now.Sub(s.CheckedAt)))

	load := fmt.Sprintf("%.2f", s.LoadPerCore)
	if s.NumCPU > 0 {
		load = fmt.Sprintf("%.2f (%d cores)", s.LoadPerCore, s.NumCPU)
	}
	fmt.Fprintf(&b, "disk %.0f%% · mem avail %.0f%% · load %s\n", s.DiskUsedPct, s.MemAvailPct, load)

	hunter := "inactive"
	if s.HunterActive {
		hunter = "active"
	}
	heartbeat := "unknown"
	if !s.HeartbeatAt.IsZero() {
		heartbeat = agoPhrase(now.Sub(s.HeartbeatAt))
	}
	fmt.Fprintf(&b, "hunter: %s · heartbeat %s\n", hunter, heartbeat)

	rows := make([]statusRow, 0, len(s.Instances)+2)
	haveSpare, haveMonitor := false, false
	for _, inst := range s.Instances {
		rows = append(rows, statusRow{inst.Name, inst.State})
		if inst.Name == MicroSpare {
			haveSpare = true
		}
		if inst.Name == MicroMonitor {
			haveMonitor = true
		}
	}
	if !haveSpare {
		rows = append(rows, statusRow{MicroSpare, "not yet"})
	}
	if !haveMonitor {
		rows = append(rows, statusRow{MicroMonitor, "not yet"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	if len(rows) == 0 {
		b.WriteString("instances: (none)\n")
	} else {
		width := 0
		for _, r := range rows {
			if len(r.name) > width {
				width = len(r.name)
			}
		}
		for i, r := range rows {
			if i == 0 {
				fmt.Fprintf(&b, "instances: %-*s %s\n", width, r.name, r.note)
			} else {
				fmt.Fprintf(&b, "%-*s %-*s %s\n", len("instances:"), "", width, r.name, r.note)
			}
		}
	}

	if s.OCIOK {
		b.WriteString("OCI API: ok")
	} else {
		fmt.Fprintf(&b, "OCI API: failing — %s", truncate(s.OCIError, 80))
	}
	return b.String()
}

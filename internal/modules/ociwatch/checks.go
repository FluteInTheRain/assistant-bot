package ociwatch

import (
	"bufio"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Thresholds are Go constants in v1 — no config knobs until one is needed
// (design 5.2). They are tuned so the hourly anti-idle stress job
// (CPU ~70% for 8 min, 6 GB RAM of 24 GB) never trips them.
const (
	diskWarnPct     = 85.0  // disk used above this → alert
	memWarnAvailPct = 10.0  // MemAvailable below this, memStrikes checks in a row → alert
	cpuWarnPerCore  = 0.9   // 5-min load per core above this, cpuStrikes checks in a row → alert
	hunterStale     = 45 * time.Minute // heartbeat is every 30 min

	memStrikes    = 2 // ~10 min
	cpuStrikes    = 3 // ~15 min
	ociStrikes    = 3 // ~15 min
	hunterStrikes = 2 // ~10 min, absorbs the seconds-long daily restart gaps
)

// The two Micro VMs the hunter is trying to create.
const (
	MicroSpare   = "micro-spare"
	MicroMonitor = "micro-monitor"
)

// CheckInterval is how often every check runs (design 5.2).
const CheckInterval = 5 * time.Minute

// exists reports whether an instance counts as "existing": any lifecycle
// state other than TERMINATING/TERMINATED. This is the same rule the Mac
// watcher uses, so PROVISIONING already counts and the bot never calls an
// exited hunter "dead" while a Micro is still starting (design 5.2).
func exists(state string) bool {
	switch strings.ToUpper(state) {
	case "TERMINATING", "TERMINATED":
		return false
	}
	return true
}

// parseMemAvailPct parses /proc/meminfo content and returns
// MemAvailable/MemTotal as a percentage.
func parseMemAvailPct(content string) (float64, error) {
	var total, avail float64
	sawTotal, sawAvail := false, false
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			v, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return 0, fmt.Errorf("meminfo MemTotal: %w", err)
			}
			total, sawTotal = v, true
		case "MemAvailable:":
			v, err := strconv.ParseFloat(fields[1], 64)
			if err != nil {
				return 0, fmt.Errorf("meminfo MemAvailable: %w", err)
			}
			avail, sawAvail = v, true
		}
	}
	if !sawTotal || !sawAvail {
		return 0, errors.New("meminfo: MemTotal/MemAvailable missing")
	}
	if total <= 0 {
		return 0, errors.New("meminfo: MemTotal is zero")
	}
	return avail / total * 100, nil
}

// parseLoad5 parses /proc/loadavg content and returns the 5-minute load.
func parseLoad5(content string) (float64, error) {
	fields := strings.Fields(content)
	if len(fields) < 2 {
		return 0, errors.New("loadavg: too short")
	}
	return strconv.ParseFloat(fields[1], 64)
}

// parseUptime parses /proc/uptime content and returns the boot uptime.
func parseUptime(content string) (time.Duration, error) {
	fields := strings.Fields(content)
	if len(fields) < 1 {
		return 0, errors.New("uptime: too short")
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("uptime: %w", err)
	}
	return time.Duration(secs * float64(time.Second)), nil
}

// formatUp renders an uptime the way the design's /status example does:
// "1d 4h", "3h 20m", "12m", "40s".
func formatUp(d time.Duration) string {
	if d < time.Minute {
		return strconv.FormatInt(int64(d.Seconds()), 10) + "s"
	}
	if d < time.Hour {
		return strconv.FormatInt(int64(d.Minutes()), 10) + "m"
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
}

// formatAgo renders a duration as "2 min", "3 h 5 m", "1 d 4 h" for the
// "checked/heartbeat X ago" lines in /status.
func formatAgo(d time.Duration) string {
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%d d %d h", int(d.Hours())/24, int(d.Hours())%24)
	}
}

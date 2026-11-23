package ociwatch

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"time"
)

// Facts about the hunter, pinned by the design (§3): systemd service
// `grab-servers`, log at /home/ubuntu/grab/grab.log. The bot only reads
// them — an anti-goal is to never interfere with the hunter.
const (
	hunterServiceName = "grab-servers"
	grabLogPath       = "/home/ubuntu/grab/grab.log"
)

const (
	rootFS     = "/"
	procMeminfo = "/proc/meminfo"
	procLoadavg = "/proc/loadavg"
	procUptime  = "/proc/uptime"
)

// readDiskUsedPct returns the used percentage of / in df's terms,
// used / (used + available), so it matches what `df -h` shows.
func readDiskUsedPct() (float64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(rootFS, &st); err != nil {
		return 0, err
	}
	used := uint64(st.Blocks) - uint64(st.Bfree)
	avail := uint64(st.Bavail)
	total := used + avail
	if total == 0 {
		return 0, errors.New("statfs: zero usable blocks")
	}
	return float64(used) / float64(total) * 100, nil
}

// readMemAvailPct reads MemAvailable from /proc/meminfo.
func readMemAvailPct() (float64, error) {
	data, err := os.ReadFile(procMeminfo)
	if err != nil {
		return 0, err
	}
	return parseMemAvailPct(string(data))
}

// readLoadPerCore reads /proc/loadavg and divides the 5-min load by the CPU
// count (4 on the A1 VM).
func readLoadPerCore() (float64, int, error) {
	data, err := os.ReadFile(procLoadavg)
	if err != nil {
		return 0, 0, err
	}
	load, err := parseLoad5(string(data))
	if err != nil {
		return 0, 0, err
	}
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return load / float64(n), n, nil
}

// hostUptime reads the boot uptime from /proc/uptime.
func hostUptime() time.Duration {
	data, err := os.ReadFile(procUptime)
	if err != nil {
		return 0
	}
	d, err := parseUptime(string(data))
	if err != nil {
		return 0
	}
	return d
}

// hunterServiceActive reports whether grab-servers is active right now.
// Any error (including systemctl itself missing) counts as inactive, and the
// 2-strike rule absorbs the seconds-long daily restart gaps (design 5.2).
func hunterServiceActive() bool {
	return exec.Command("systemctl", "is-active", "--quiet", hunterServiceName).Run() == nil
}

// hunterLogAge returns the age of grab.log's last modification.
func hunterLogAge(now time.Time) (time.Duration, error) {
	st, err := os.Stat(grabLogPath)
	if err != nil {
		return 0, err
	}
	return now.Sub(st.ModTime()), nil
}

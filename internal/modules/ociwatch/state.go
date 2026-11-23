package ociwatch

import "time"

// Instance is one OCI instance as the watcher sees it.
type Instance struct {
	Name  string `json:"name"`
	State string `json:"state"` // OCI lifecycle state, e.g. RUNNING, PROVISIONING
}

// StatusSnapshot is the latest reading, kept for /status so the command answers
// from memory and never calls OCI live (design 5.4).
type StatusSnapshot struct {
	CheckedAt    time.Time  `json:"checked_at"`
	HostUp       string     `json:"host_up"`       // e.g. "1d 4h"
	DiskUsedPct  float64    `json:"disk_used_pct"`
	MemAvailPct  float64    `json:"mem_avail_pct"`
	LoadPerCore  float64    `json:"load_per_core"`
	NumCPU       int        `json:"num_cpu"`
	HunterActive bool       `json:"hunter_active"`
	HeartbeatAt  time.Time  `json:"heartbeat_at"` // zero = unknown
	Instances    []Instance `json:"instances"`
	OCIOK        bool       `json:"oci_ok"`
	OCIError     string     `json:"oci_error,omitempty"`
}

// persistedState is everything saved between runs so a restart neither repeats
// old alerts nor misses a Micro created while the bot was down (design 5.3).
// Strike counters are deliberately not persisted: losing them costs at most
// one extra check before an alert fires after a restart.
type persistedState struct {
	Version        int               `json:"version"`
	Instances      map[string]string `json:"instances"` // name → lifecycle state
	Checks         map[string]bool   `json:"checks"`    // check name → true = PROBLEM
	HunterDoneSaid bool              `json:"hunter_done_said"`
	Status         StatusSnapshot    `json:"status"`
}

const stateVersion = 1

// Check names used in persistedState.Checks.
const (
	checkDisk       = "disk"
	checkMem        = "mem"
	checkCPU        = "cpu"
	checkHunter     = "hunter"
	checkHunterStuck = "hunter_stuck"
	checkOCI        = "oci"
)

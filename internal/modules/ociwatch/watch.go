package ociwatch

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Reading is one tick's raw inputs, gathered by the collectors (real, or
// faked in tests). All alert decisions are made from a Reading, so the logic
// below is pure and unit-tested; real OCI/Discord only appear in the manual
// smoke test (design 5.5).
type Reading struct {
	Now time.Time

	// OCI
	OCIOK     bool
	OCIError  string     // set when !OCIOK
	Instances []Instance // valid only when OCIOK

	// Host
	DiskUsedPct float64 // df-style used % of /
	MemAvailPct float64 // MemAvailable as % of MemTotal
	LoadPerCore float64 // 5-min load divided by CPU count
	HostUp      string  // e.g. "1d 4h"
	NumCPU     int

	// Hunter
	HunterActive bool
	// HunterLogAge is the age of grab.log's mtime; negative means the log
	// is missing or unreadable.
	HunterLogAge time.Duration
}

// OutMsg is a message one tick wants delivered.
type OutMsg struct {
	Alert bool // true → mention the owner (design 5.3)
	Text  string
}

// Watcher holds the tick-to-tick state and turns Readings into messages.
// Every check is a tiny state machine: OK→PROBLEM sends one alert,
// PROBLEM→OK sends one recovery, staying put sends nothing. Instance
// changes are events, not states: each change is reported once (design 5.3).
type Watcher struct {
	// Host labels /status; not persisted (re-read at boot).
	Host string
	// now is swapped in tests for deterministic /status ages.
	now func() time.Time

	// persisted across restarts
	instances      map[string]string
	checks         map[string]bool
	hunterDoneSaid bool
	status         StatusSnapshot

	// strike counters (in-memory only; a restart costs at most one extra
	// check before an alert fires)
	mem, cpu, oci, hunter int

	// baselinePending is true until the first successful instance list:
	// the baseline must be taken silently, and a failed first call must not
	// turn every pre-existing instance into a fake "appeared" event later.
	baselinePending bool
	// startedSaid is true once the "bot started" summary has been posted
	// (first run only — a restart with a state file stays quiet, design 5.3).
	startedSaid bool
}

func NewWatcher(now func() time.Time) *Watcher {
	if now == nil {
		now = time.Now
	}
	return &Watcher{
		now:             now,
		instances:       map[string]string{},
		checks:          newCheckMap(),
		baselinePending: true,
	}
}

// Load restores the state saved by a previous run so a restart neither
// repeats old alerts nor misses a Micro created while the bot was down.
func (w *Watcher) Load(p persistedState) {
	w.instances = map[string]string{}
	for k, v := range p.Instances {
		w.instances[k] = v
	}
	w.checks = newCheckMap()
	for k, v := range p.Checks {
		w.checks[k] = v
	}
	w.hunterDoneSaid = p.HunterDoneSaid
	w.status = p.Status
	w.baselinePending = false
	w.startedSaid = true
}

// Persisted returns the state to save for the next run.
func (w *Watcher) Persisted() persistedState {
	return persistedState{
		Version:        stateVersion,
		Instances:      w.instances,
		Checks:         w.checks,
		HunterDoneSaid: w.hunterDoneSaid,
		Status:         w.status,
	}
}

func newCheckMap() map[string]bool {
	return map[string]bool{
		checkDisk:        false,
		checkMem:        false,
		checkCPU:        false,
		checkHunter:     false,
		checkHunterStuck: false,
		checkOCI:        false,
	}
}

// flip records a check state and reports the transitions.
func (w *Watcher) flip(check string, problem bool) (becameProblem, recovered bool) {
	was := w.checks[check]
	w.checks[check] = problem
	return problem && !was, !problem && was
}

// Tick consumes one Reading, updates all state and returns the messages to
// deliver. It performs no I/O, so unit tests drive it with fake Readings.
func (w *Watcher) Tick(r Reading) []OutMsg {
	var msgs []OutMsg
	add := func(alert bool, format string, args ...any) {
		msgs = append(msgs, OutMsg{Alert: alert, Text: fmt.Sprintf(format, args...)})
	}

	// --- OCI reachable: 3 consecutive failures ≈ 15 min; one blip is normal.
	ociProblem := false
	if r.OCIOK {
		w.oci = 0
	} else {
		w.oci++
		if w.oci >= ociStrikes {
			ociProblem = true
		}
	}
	if became, recovered := w.flip(checkOCI, ociProblem); became {
		add(true, "🚨 OCI API unreachable for ~%d min: %s", w.oci*int(CheckInterval/time.Minute), truncate(r.OCIError, 200))
	} else if recovered {
		add(false, "✅ OCI API reachable again")
	}

	// --- instances. Only a successful call updates the snapshot, so an API
	// outage followed by recovery never fabricates mass changes.
	current := map[string]string{}
	if r.OCIOK {
		for _, inst := range r.Instances {
			if exists(inst.State) {
				current[inst.Name] = inst.State
			}
		}
	} else {
		for k, v := range w.instances {
			current[k] = v
		}
	}
	// "Hunter done" is derived live on every tick, not a one-way latch
	// (design 5.2): both Micros exist → say it once; a Micro disappears →
	// the flag clears so a later re-appearance says it again.
	bothExist := current[MicroSpare] != "" && current[MicroMonitor] != ""

	// First run: record the baseline silently, then post one summary and
	// stop — check state machines start from a clean OK on the next tick.
	if !w.startedSaid {
		w.startedSaid = true
		if r.OCIOK {
			w.baselinePending = false
			w.instances = current
		}
		if bothExist {
			w.hunterDoneSaid = true
		}
		w.updateStatus(r, current)
		return []OutMsg{{Alert: false, Text: w.startedSummary()}}
	}

	if r.OCIOK {
		if w.baselinePending {
			w.baselinePending = false
			w.instances = current
		}
		for _, inst := range sortedInstances(current) {
			old, ok := w.instances[inst.Name]
			switch {
			case !ok:
				add(true, "🎉 instance appeared: **%s** (%s)", inst.Name, inst.State)
			case old != inst.State:
				add(true, "🔁 **%s**: %s → %s", inst.Name, old, inst.State)
			}
		}
		for _, inst := range sortedInstances(w.instances) {
			if _, ok := current[inst.Name]; !ok {
				text := fmt.Sprintf("🔴 instance gone: **%s** (was %s)", inst.Name, inst.State)
				if inst.Name == MicroSpare || inst.Name == MicroMonitor {
					if w.hunterDoneSaid {
						text += "\nThe hunter exited after it finished — restart `grab-servers` by hand if you want it hunting again."
					} else {
						text += "\nIf the hunter already exited, restart `grab-servers` by hand."
					}
					w.hunterDoneSaid = false
				}
				msgs = append(msgs, OutMsg{Alert: true, Text: text})
			}
		}
		w.instances = current
	}

	if bothExist && !w.hunterDoneSaid {
		w.hunterDoneSaid = true
		add(true, "🎯 hunter finished: both `micro-spare` and `micro-monitor` exist. `grab-servers` exited on purpose — restart it by hand if a Micro ever disappears.")
	}

	// --- hunter alive: inactive for 2 checks in a row, and not both Micros
	// (a finished hunter is expected to be inactive).
	hunterProblem := false
	if !r.HunterActive && !bothExist {
		w.hunter++
		if w.hunter >= hunterStrikes {
			hunterProblem = true
		}
	} else {
		w.hunter = 0
	}
	if became, recovered := w.flip(checkHunter, hunterProblem); became {
		add(true, "🚨 `grab-servers` not active for ~%d min and the Micros are not both there yet", w.hunter*int(CheckInterval/time.Minute))
	} else if recovered {
		add(false, "✅ `grab-servers` is active again")
	}

	// --- hunter stuck: log stale while the service runs (heartbeat is every
	// 30 min). When the service is inactive this check resets silently —
	// a dead service is the alive check's business.
	if !r.HunterActive {
		w.checks[checkHunterStuck] = false
	} else {
		stuck := false
		detail := ""
		switch {
		case r.HunterLogAge < 0:
			stuck, detail = true, "`grab.log` is missing while the service runs"
		case r.HunterLogAge > hunterStale:
			stuck = true
			detail = fmt.Sprintf("`grab.log` has been stale for %s while the service runs (hung?)", formatUp(r.HunterLogAge))
		}
		if became, recovered := w.flip(checkHunterStuck, stuck); became {
			add(true, "⚠️ hunter looks stuck: %s", detail)
		} else if recovered {
			add(false, "✅ hunter log is moving again")
		}
	}

	// --- disk: one reading above the threshold is enough (df-style %).
	if became, recovered := w.flip(checkDisk, r.DiskUsedPct > diskWarnPct); became {
		add(true, "🚨 disk %.0f%% used (> %v%%)", r.DiskUsedPct, diskWarnPct)
	} else if recovered {
		add(false, "✅ disk back to %.0f%%", r.DiskUsedPct)
	}

	// --- memory: below 10% available twice in a row; the anti-idle job
	// takes 6 GB of 24 GB (25%) so it stays clear.
	memProblem := false
	if r.MemAvailPct < memWarnAvailPct {
		w.mem++
		if w.mem >= memStrikes {
			memProblem = true
		}
	} else {
		w.mem = 0
	}
	if became, recovered := w.flip(checkMem, memProblem); became {
		add(true, "🚨 only %.0f%% RAM available (< %v%%) for ~%d min", r.MemAvailPct, memWarnAvailPct, w.mem*int(CheckInterval/time.Minute))
	} else if recovered {
		add(false, "✅ RAM back to %.0f%% available", r.MemAvailPct)
	}

	// --- CPU: load per core above 0.9 for 3 checks (~15 min); the anti-idle
	// burst is ~70% for 8 min so it stays clear.
	cpuProblem := false
	if r.LoadPerCore > cpuWarnPerCore {
		w.cpu++
		if w.cpu >= cpuStrikes {
			cpuProblem = true
		}
	} else {
		w.cpu = 0
	}
	if became, recovered := w.flip(checkCPU, cpuProblem); became {
		add(true, "🚨 CPU load %.2f per core (> %.2f) for ~%d min", r.LoadPerCore, cpuWarnPerCore, w.cpu*int(CheckInterval/time.Minute))
	} else if recovered {
		add(false, "✅ CPU load back to %.2f", r.LoadPerCore)
	}

	w.updateStatus(r, current)
	return msgs
}

func (w *Watcher) updateStatus(r Reading, current map[string]string) {
	insts := make([]Instance, 0, len(current))
	for _, inst := range sortedInstances(current) {
		insts = append(insts, inst)
	}
	var heartbeat time.Time
	if r.HunterLogAge >= 0 {
		heartbeat = r.Now.Add(-r.HunterLogAge)
	}
	w.status = StatusSnapshot{
		CheckedAt:    r.Now,
		HostUp:       r.HostUp,
		DiskUsedPct:  r.DiskUsedPct,
		MemAvailPct:  r.MemAvailPct,
		LoadPerCore:  r.LoadPerCore,
		NumCPU:       r.NumCPU,
		HunterActive: r.HunterActive,
		HeartbeatAt:  heartbeat,
		Instances:    insts,
		OCIOK:        r.OCIOK,
		OCIError:     r.OCIError,
	}
}

// startedSummary is the one "bot started" message of a first run (design 5.3).
func (w *Watcher) startedSummary() string {
	var b strings.Builder
	b.WriteString("🤖 assistant-bot is up — baseline taken, alerts from here on.\n")
	if w.status.OCIOK {
		for _, inst := range w.status.Instances {
			fmt.Fprintf(&b, "• %s — %s\n", inst.Name, inst.State)
		}
		if _, ok := w.instances[MicroSpare]; !ok {
			fmt.Fprintf(&b, "• %s — not yet\n", MicroSpare)
		}
		if _, ok := w.instances[MicroMonitor]; !ok {
			fmt.Fprintf(&b, "• %s — not yet\n", MicroMonitor)
		}
	} else {
		b.WriteString("• first OCI call failed — retrying every 5 min\n")
	}
	fmt.Fprintf(&b, "Checks every %d min; alerts ping, recoveries don't.", int(CheckInterval/time.Minute))
	return b.String()
}

func sortedInstances(m map[string]string) []Instance {
	out := make([]Instance, 0, len(m))
	for name, state := range m {
		out = append(out, Instance{Name: name, State: state})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// truncate shortens s to at most max runes.
func truncate(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

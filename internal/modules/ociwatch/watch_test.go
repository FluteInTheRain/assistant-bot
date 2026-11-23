package ociwatch

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/FluteInTheRain/assistant-bot/internal/core"
)

var t0 = time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)

func tickTime(n int) time.Time { return t0.Add(time.Duration(n) * CheckInterval) }

// baseReading is a boringly healthy tick on the VM.
func baseReading(n int) Reading {
	return Reading{
		Now:          tickTime(n),
		OCIOK:        true,
		Instances:    []Instance{{Name: "side-projects", State: "RUNNING"}},
		DiskUsedPct:  30,
		MemAvailPct:  70,
		LoadPerCore:  0.1,
		NumCPU:       4,
		HostUp:       "1d 4h",
		HunterActive: true,
		HunterLogAge: 10 * time.Minute,
	}
}

func newTestWatcher() *Watcher {
	w := NewWatcher(func() time.Time { return t0 })
	w.Host = "side-projects"
	return w
}

func withInstances(r Reading, insts ...Instance) Reading {
	r.Instances = append([]Instance{{Name: "side-projects", State: "RUNNING"}}, insts...)
	return r
}

func bothMicros(r Reading) Reading {
	return withInstances(r,
		Instance{Name: MicroSpare, State: "RUNNING"},
		Instance{Name: MicroMonitor, State: "RUNNING"},
	)
}

func texts(msgs []OutMsg) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.Text
	}
	return out
}

func wantOne(t *testing.T, msgs []OutMsg, alert bool, substr string) {
	t.Helper()
	if len(msgs) != 1 {
		t.Fatalf("want exactly 1 message, got %d: %q", len(msgs), texts(msgs))
	}
	if msgs[0].Alert != alert {
		t.Fatalf("want alert=%v, got %v: %q", alert, msgs[0].Alert, msgs[0].Text)
	}
	if !strings.Contains(msgs[0].Text, substr) {
		t.Fatalf("message %q does not contain %q", msgs[0].Text, substr)
	}
}

func wantNone(t *testing.T, msgs []OutMsg) {
	t.Helper()
	if len(msgs) != 0 {
		t.Fatalf("want no messages, got %d: %q", len(msgs), texts(msgs))
	}
}

func wantContains(t *testing.T, msgs []OutMsg, alert bool, substr string) {
	t.Helper()
	for _, m := range msgs {
		if m.Alert == alert && strings.Contains(m.Text, substr) {
			return
		}
	}
	t.Fatalf("no %s message containing %q in %q", map[bool]string{true: "alert", false: "info"}[alert], substr, texts(msgs))
}

// --- first run ---------------------------------------------------------------

func TestFirstRunPostsOnlyOneSummary(t *testing.T) {
	w := newTestWatcher()
	msgs := w.Tick(baseReading(0))
	if len(msgs) != 1 {
		t.Fatalf("want 1 summary message, got %d: %q", len(msgs), texts(msgs))
	}
	if msgs[0].Alert {
		t.Fatalf("summary must not ping: %q", msgs[0].Text)
	}
	for _, want := range []string{
		"assistant-bot is up",
		"side-projects — RUNNING",
		"micro-spare — not yet",
		"micro-monitor — not yet",
	} {
		if !strings.Contains(msgs[0].Text, want) {
			t.Fatalf("summary %q missing %q", msgs[0].Text, want)
		}
	}
	if _, ok := w.instances["side-projects"]; !ok {
		t.Fatal("baseline did not record the instance")
	}
	// Second, identical tick: total silence — the baseline was silent and
	// every check stayed OK.
	wantNone(t, w.Tick(baseReading(1)))
}

func TestFirstRunWithOCIDownDefersBaseline(t *testing.T) {
	w := newTestWatcher()
	r0 := baseReading(0)
	r0.OCIOK, r0.OCIError, r0.Instances = false, "dial tcp: i'm out", nil
	msgs := w.Tick(r0)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "first OCI call failed") {
		t.Fatalf("want a started summary mentioning the failure, got %q", texts(msgs))
	}
	// First good call: the instance baseline is taken silently — the
	// pre-existing side-projects must NOT be announced as "appeared".
	msgs = w.Tick(baseReading(1))
	for _, m := range msgs {
		if strings.Contains(m.Text, "appeared") {
			t.Fatalf("baseline leaked an appeared event: %q", m.Text)
		}
	}
	if w.baselinePending {
		t.Fatal("baseline still pending after a good call")
	}
}

// --- instances ---------------------------------------------------------------

func TestInstanceEvents(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	// appeared
	msgs := w.Tick(withInstances(baseReading(1), Instance{Name: MicroSpare, State: "PROVISIONING"}))
	wantOne(t, msgs, true, "instance appeared: **micro-spare** (PROVISIONING)")

	// lifecycle change
	msgs = w.Tick(withInstances(baseReading(2), Instance{Name: MicroSpare, State: "RUNNING"}))
	wantOne(t, msgs, true, "**micro-spare**: PROVISIONING → RUNNING")

	// TERMINATED is filtered out of existence → "gone". Micros carry the
	// hand-restart note (the hunter may have exited after finishing).
	msgs = w.Tick(baseReading(3))
	wantOne(t, msgs, true, "instance gone: **micro-spare** (was RUNNING)")
	if !strings.Contains(msgs[0].Text, "restart `grab-servers` by hand") {
		t.Fatalf("a gone Micro must remind to restart the hunter: %q", msgs[0].Text)
	}
}

func TestNonMicroGoneHasNoRestartNote(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))
	r := baseReading(1)
	r.Instances = nil // even side-projects disappears
	msgs := w.Tick(r)
	wantOne(t, msgs, true, "instance gone: **side-projects**")
	if strings.Contains(msgs[0].Text, "restart") {
		t.Fatalf("non-micro gone must not carry a restart note: %q", msgs[0].Text)
	}
}

func TestMicroGoneAfterHunterFinishedRemindsRestart(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	msgs := w.Tick(bothMicros(baseReading(1)))
	wantContains(t, msgs, true, "instance appeared: **micro-spare**")
	wantContains(t, msgs, true, "instance appeared: **micro-monitor**")
	wantContains(t, msgs, true, "hunter finished")
	if !w.hunterDoneSaid {
		t.Fatal("hunterDoneSaid must be set when both Micros exist")
	}
	// No repeats while nothing changes.
	wantNone(t, w.Tick(bothMicros(baseReading(2))))

	// micro-spare disappears → gone + hand-restart note, flag cleared.
	msgs = w.Tick(withInstances(baseReading(3), Instance{Name: MicroMonitor, State: "RUNNING"}))
	wantOne(t, msgs, true, "instance gone: **micro-spare**")
	if !strings.Contains(msgs[0].Text, "restart `grab-servers` by hand") {
		t.Fatalf("gone message must remind to restart the hunter: %q", msgs[0].Text)
	}
	if w.hunterDoneSaid {
		t.Fatal("hunterDoneSaid must be cleared when a Micro disappears")
	}
	// And it can be said again once both Micros are back.
	msgs = w.Tick(bothMicros(baseReading(4)))
	wantContains(t, msgs, true, "instance appeared: **micro-spare**")
	wantContains(t, msgs, true, "hunter finished")
}

// --- hunter ------------------------------------------------------------------

func TestHunterDeadNeedsTwoStrikesAndRecovers(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	r := baseReading(1)
	r.HunterActive = false
	wantNone(t, w.Tick(r)) // 1 strike: the daily restart gap is seconds long

	msgs := w.Tick(r)
	wantOne(t, msgs, true, "grab-servers` not active for ~10 min")
	wantNone(t, w.Tick(r)) // no spam while the problem persists

	r.HunterActive = true
	wantOne(t, w.Tick(r), false, "grab-servers` is active again")
}

func TestBriefHunterGapNeverAlerts(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))
	r := baseReading(1)
	r.HunterActive = false
	wantNone(t, w.Tick(r))
	wantNone(t, w.Tick(baseReading(2))) // back before the 2nd strike
}

func TestInactiveHunterIsExpectedWhenBothMicrosExist(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))
	r := bothMicros(baseReading(1))
	r.HunterActive = false
	msgs := w.Tick(r)
	wantContains(t, msgs, true, "hunter finished")
	for _, m := range msgs {
		if strings.Contains(m.Text, "not active") {
			t.Fatalf("a finished hunter must not alert as dead: %q", m.Text)
		}
	}
	wantNone(t, w.Tick(r)) // steady state: total silence
}

func TestHunterStuck(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	r := baseReading(1)
	r.HunterLogAge = 45 * time.Minute
	wantNone(t, w.Tick(r)) // exactly the threshold is still fine

	r.HunterLogAge = 50 * time.Minute
	wantOne(t, w.Tick(r), true, "hunter looks stuck")
	wantNone(t, w.Tick(r)) // no spam while stuck

	r.HunterLogAge = 5 * time.Minute
	wantOne(t, w.Tick(r), false, "hunter log is moving again")

	// Missing log while the service runs counts as stuck.
	r.HunterLogAge = -1
	wantOne(t, w.Tick(r), true, "`grab.log` is missing")
}

func TestStuckResetsSilentlyWhenServiceInactive(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	r := baseReading(1)
	r.HunterLogAge = 50 * time.Minute
	wantOne(t, w.Tick(r), true, "hunter looks stuck")

	// Service goes inactive (finished or dead): the alive check owns that
	// story now, the stuck check resets without a recovery message.
	r.HunterActive = false
	msgs := w.Tick(r)
	for _, m := range msgs {
		if strings.Contains(m.Text, "moving again") {
			t.Fatalf("inactive service must not emit a stuck recovery: %q", m.Text)
		}
	}
}

// --- disk / memory / cpu ------------------------------------------------------

func TestDiskThreshold(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0)) // 30%: baseline

	r := baseReading(1)
	r.DiskUsedPct = diskWarnPct // exactly 85: still fine
	wantNone(t, w.Tick(r))

	r.DiskUsedPct = 86
	wantOne(t, w.Tick(r), true, "disk 86% used")
	wantNone(t, w.Tick(r)) // stays bad: one alert only

	r.DiskUsedPct = 84
	wantOne(t, w.Tick(r), false, "disk back to 84%")
}

func TestMemoryNeedsTwoStrikes(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	r := baseReading(1)
	r.MemAvailPct = 9
	wantNone(t, w.Tick(r)) // one dip is not a problem

	wantOne(t, w.Tick(r), true, "only 9% RAM available")
	wantNone(t, w.Tick(r)) // no spam while low

	r.MemAvailPct = 60
	wantOne(t, w.Tick(r), false, "RAM back to 60% available")
}

func TestMemoryIgnoresAntiIdleJob(t *testing.T) {
	// The hourly anti-idle burst takes 6 GB of 24 GB → 75% available.
	w := newTestWatcher()
	w.Tick(baseReading(0))
	for n := 1; n <= 3; n++ {
		r := baseReading(n)
		r.MemAvailPct = 75
		wantNone(t, w.Tick(r))
	}
}

func TestCPUNeedsThreeStrikes(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	r := baseReading(1)
	r.LoadPerCore = 0.95
	wantNone(t, w.Tick(r))
	wantNone(t, w.Tick(r))
	wantOne(t, w.Tick(r), true, "CPU load 0.95 per core (> 0.90) for ~15 min")
	wantNone(t, w.Tick(r))

	r.LoadPerCore = 0.5
	wantOne(t, w.Tick(r), false, "CPU load back to 0.50")
}

func TestCPUIgnoresAntiIdleJob(t *testing.T) {
	// The hourly anti-idle burst is ~70% for 8 min; 3 checks span 15 min,
	// and 0.7 stays under the 0.9 threshold anyway.
	w := newTestWatcher()
	w.Tick(baseReading(0))
	for n := 1; n <= 4; n++ {
		r := baseReading(n)
		r.LoadPerCore = 0.7
		wantNone(t, w.Tick(r))
	}
}

// --- OCI reachability ---------------------------------------------------------

func TestOCIFailuresNeedThreeStrikes(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	fail := func(n int) Reading {
		r := baseReading(n)
		r.OCIOK, r.OCIError, r.Instances = false, "503 service unavailable", nil
		return r
	}
	wantNone(t, w.Tick(fail(1)))
	wantNone(t, w.Tick(fail(2)))
	wantOne(t, w.Tick(fail(3)), true, "🚨 OCI API unreachable for ~15 min")
	wantNone(t, w.Tick(fail(4)))

	wantOne(t, w.Tick(baseReading(5)), false, "✅ OCI API reachable again")
}

func TestOCIOutagePreservesInstanceSnapshot(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	fail := func(n int) Reading {
		r := baseReading(n)
		r.OCIOK, r.OCIError, r.Instances = false, "timeout", nil
		return r
	}
	w.Tick(fail(1))
	w.Tick(fail(2))

	// API comes back with the same instances: total silence — no recovery
	// message (it never became a problem after only 2 strikes) and above all
	// no mass "appeared"/"gone" events fabricated by the outage.
	wantNone(t, w.Tick(baseReading(3)))
}

// --- persistence --------------------------------------------------------------

func TestRestartWithStateDiffsAgainstOldSnapshot(t *testing.T) {
	// A Micro created while the bot was down must still be reported.
	w1 := newTestWatcher()
	w1.Tick(baseReading(0))
	p := w1.Persisted()

	w2 := newTestWatcher()
	w2.Load(p)
	msgs := w2.Tick(withInstances(baseReading(1), Instance{Name: MicroSpare, State: "RUNNING"}))
	wantOne(t, msgs, true, "instance appeared: **micro-spare**")
}

func TestStateFileRoundTrip(t *testing.T) {
	w1 := newTestWatcher()
	w1.Tick(bothMicros(baseReading(0)))
	p1 := w1.Persisted()

	path := filepath.Join(t.TempDir(), "state.json")
	if err := core.SaveJSONState(path, &p1); err != nil {
		t.Fatal(err)
	}
	var p2 persistedState
	found, err := core.LoadJSONState(path, &p2)
	if err != nil || !found {
		t.Fatalf("load: found=%v err=%v", found, err)
	}

	w2 := newTestWatcher()
	w2.Load(p2)
	if len(w2.instances) != 3 {
		t.Fatalf("want 3 instances restored, got %v", w2.instances)
	}
	if !w2.hunterDoneSaid {
		t.Fatal("hunterDoneSaid must survive a restart")
	}
	if !w2.startedSaid || w2.baselinePending {
		t.Fatal("a restarted bot must not post another baseline/summary")
	}
	// Steady tick right after restart: silence, no repeats.
	wantNone(t, w2.Tick(bothMicros(baseReading(1))))
}

func TestRestartWhileProblemIsOpenDoesNotRepeatAlert(t *testing.T) {
	w1 := newTestWatcher()
	w1.Tick(baseReading(0))
	r := baseReading(1)
	r.DiskUsedPct = 95
	if msgs := w1.Tick(r); len(msgs) != 1 || !msgs[0].Alert {
		t.Fatalf("want one disk alert, got %q", texts(msgs))
	}
	p := w1.Persisted()

	w2 := newTestWatcher()
	w2.Load(p)
	r2 := baseReading(2)
	r2.DiskUsedPct = 95
	// Still broken after the restart: no repeat (the state was persisted).
	wantNone(t, w2.Tick(r2))
	// Fixing it still yields exactly one recovery.
	r2.DiskUsedPct = 40
	wantOne(t, w2.Tick(r2), false, "disk back to 40%")
}

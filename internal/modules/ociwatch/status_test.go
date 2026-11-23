package ociwatch

import (
	"strings"
	"testing"
	"time"
)

func TestStatusTextBeforeAnyCheck(t *testing.T) {
	w := newTestWatcher()
	if got := w.StatusText(); got != "No check has completed yet." {
		t.Fatalf("got %q", got)
	}
}

func TestStatusTextLayout(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))
	// w.now is frozen at t0 == the reading's Now → "checked just now".
	got := w.StatusText()
	wantLines := []string{
		"side-projects · up 1d 4h · checked just now",
		"disk 30% · mem avail 70% · load 0.10 (4 cores)",
		"hunter: active · heartbeat 10 min ago",
		"instances: micro-monitor not yet",
		"           micro-spare   not yet",
		"           side-projects RUNNING",
		"OCI API: ok",
	}
	for _, want := range wantLines {
		if !strings.Contains(got, want) {
			t.Fatalf("status text missing line %q:\n%s", want, got)
		}
	}
}

func TestStatusTextReflectsLatestReading(t *testing.T) {
	w := newTestWatcher()
	w.Tick(baseReading(0))

	// Good tick: both Micros show up, disk worsens, hunter goes inactive
	// (expected: it finished).
	r := bothMicros(baseReading(1))
	r.DiskUsedPct = 88
	r.HunterActive = false
	w.Tick(r)

	// Next tick: the OCI call fails → the snapshot keeps the last-known
	// instances and the status says the API is failing.
	r2 := baseReading(2)
	r2.OCIOK, r2.OCIError, r2.Instances = false, "boom", nil
	r2.HunterActive = false
	r2.HunterLogAge = 10 * time.Minute
	r2.DiskUsedPct = 88
	w.Tick(r2)

	// Move the frozen clock 2 minutes past that tick.
	w.now = func() time.Time { return t0.Add(12 * time.Minute) }
	got := w.StatusText()
	for _, want := range []string{
		"checked 2 min ago",
		"disk 88%",
		"hunter: inactive",
		"micro-spare   RUNNING",
		"micro-monitor RUNNING",
		"side-projects RUNNING",
		"OCI API: failing — boom",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("status text missing %q:\n%s", want, got)
		}
	}
}

func TestStatusTextAfterRestartUsesPersistedSnapshot(t *testing.T) {
	w1 := newTestWatcher()
	w1.Tick(baseReading(0))
	p := w1.Persisted()

	w2 := newTestWatcher()
	w2.Load(p)
	got := w2.StatusText()
	if !strings.Contains(got, "checked") || !strings.Contains(got, "side-projects RUNNING") {
		t.Fatalf("status after restart should render the saved snapshot, got:\n%s", got)
	}
}

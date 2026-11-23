package jobwatch

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testNow() time.Time { return time.Date(2026, 11, 23, 12, 0, 0, 0, ICT) }

func classifiedBackend(title, company, link string) Job {
	j := Job{Title: title, Company: company, Link: link, Source: "TopCV"}
	Classify(&j)
	return j
}

func TestPollBaselineIsSilent(t *testing.T) {
	e := NewEngine()
	now := testNow()
	jobs := []Job{
		classifiedBackend("Backend Developer", "FPT", "https://x/1"),
		classifiedBackend("Data Engineer", "Momo", "https://x/2"),
		{Title: "Marketing Manager", Company: "ACME", Link: "https://x/3"}, // not wanted
	}
	newJobs, msgs := e.Poll(now, jobs, nil)
	if len(newJobs) != 0 || len(msgs) != 0 {
		t.Fatalf("baseline must be silent: newJobs=%d msgs=%d", len(newJobs), len(msgs))
	}
	// Everything is marked seen, so the very same fetch adds nothing.
	newJobs, msgs = e.Poll(now.Add(time.Hour), jobs, nil)
	if len(newJobs) != 0 || len(msgs) != 0 {
		t.Fatalf("second poll of the same jobs must add nothing: newJobs=%d msgs=%d", len(newJobs), len(msgs))
	}
	if len(e.pending) != 0 {
		t.Fatalf("pending must stay empty, has %d", len(e.pending))
	}
}

func TestPollNewJobFlowsToPending(t *testing.T) {
	e := NewEngine()
	now := testNow()
	seed := []Job{classifiedBackend("Backend Developer", "FPT", "https://x/1")}
	e.Poll(now, seed, nil)

	next := []Job{
		seed[0],
		classifiedBackend("Senior Golang Engineer", "Tiki", "https://x/2"),
		{Title: "Sales", Company: "ACME", Link: "https://x/3"}, // filtered out
	}
	newJobs, msgs := e.Poll(now.Add(2*time.Hour), next, nil)
	if len(msgs) != 0 {
		t.Fatalf("steady state sends nothing, got %d msgs", len(msgs))
	}
	if len(newJobs) != 1 || newJobs[0].Title != "Senior Golang Engineer" {
		t.Fatalf("want exactly the new job, got %+v", newJobs)
	}
	if len(e.pending) != 1 || e.totalMatched != 1 {
		t.Fatalf("pending=%d totalMatched=%d, want 1/1", len(e.pending), e.totalMatched)
	}
}

func TestPollDedupAcrossBoardsAndFragments(t *testing.T) {
	e := NewEngine()
	now := testNow()
	// Cross-board repost: different links, same role (company+title).
	first := classifiedBackend("Senior Python Developer", "VNPT", "https://itviec.com/j/1")
	e.Poll(now, []Job{first}, nil)

	repost := classifiedBackend("Senior Python Developer", "VNPT", "https://topcv.vn/j/999")
	fragment := classifiedBackend("Senior Python Developer", "VNPT", first.Link+"#rc=job_list__item")
	newJobs, _ := e.Poll(now.Add(2*time.Hour), []Job{repost, fragment}, nil)
	if len(newJobs) != 0 {
		t.Fatalf("repost must dedup, got %+v", newJobs)
	}
	if len(e.pending) != 0 {
		t.Fatalf("pending must stay empty, has %d", len(e.pending))
	}
}

func TestPollAPIFailureStateMachine(t *testing.T) {
	e := NewEngine()
	now := testNow()

	// A failed first fetch must not baseline anything.
	_, msgs := e.Poll(now, nil, errors.New("boom"))
	if len(msgs) != 0 {
		t.Fatalf("first failure should stay quiet, got %+v", msgs)
	}
	if e.baselinePending != true {
		t.Fatal("baseline must still be pending after a failed first fetch")
	}

	// Two more failures → exactly one Alert (3 in a row ≈ 6 h).
	_, msgs = e.Poll(now.Add(2*time.Hour), nil, errors.New("boom"))
	if len(msgs) != 0 {
		t.Fatalf("second failure should stay quiet, got %+v", msgs)
	}
	_, msgs = e.Poll(now.Add(4*time.Hour), nil, errors.New("boom"))
	if len(msgs) != 1 || !msgs[0].Alert {
		t.Fatalf("third failure must alert once, got %+v", msgs)
	}
	// Still failing: no repeat alert.
	_, msgs = e.Poll(now.Add(6*time.Hour), nil, errors.New("boom"))
	if len(msgs) != 0 {
		t.Fatalf("steady PROBLEM sends nothing, got %+v", msgs)
	}

	// Recovery: one Info, and the baseline finally happens silently.
	jobs := []Job{classifiedBackend("Backend Developer", "FPT", "https://x/1")}
	newJobs, msgs := e.Poll(now.Add(8*time.Hour), jobs, nil)
	if len(msgs) != 1 || msgs[0].Alert || msgs[0].Text == "" {
		t.Fatalf("recovery must send one Info, got %+v", msgs)
	}
	if len(newJobs) != 0 {
		t.Fatalf("recovery fetch is the baseline, must be silent, got %+v", newJobs)
	}
}

func TestDigestClearsPendingAndAlwaysSends(t *testing.T) {
	e := NewEngine()
	now := testNow()
	seed := []Job{classifiedBackend("Backend Developer", "FPT", "https://x/1")}
	e.Poll(now, seed, nil) // baseline
	next := []Job{seed[0], classifiedBackend("Golang Dev", "VNG", "https://x/2")}
	e.Poll(now.Add(2*time.Hour), next, nil)
	if len(e.pending) != 1 {
		t.Fatalf("pending=%d, want 1", len(e.pending))
	}

	msgs := e.Digest(now.Add(20 * time.Hour))
	if len(msgs) == 0 {
		t.Fatal("digest must always send")
	}
	if !e.lastDigest.Equal(now.Add(20 * time.Hour)) {
		t.Fatal("digest must anchor lastDigest")
	}
	if len(e.pending) != 0 {
		t.Fatalf("pending must be cleared, has %d", len(e.pending))
	}
	// The digest never repeats: another Digest sends the empty one.
	msgs2 := e.Digest(now.Add(44 * time.Hour))
	if len(msgs2) != 1 || !strings.Contains(msgs2[0].Text, "0 new matched") {
		t.Fatalf("second digest should report 0 new, got %+v", msgs2)
	}
}

func TestEngineRoundTripState(t *testing.T) {
	e := NewEngine()
	now := testNow()
	seed := []Job{classifiedBackend("Backend Developer", "FPT", "https://x/1")}
	e.Poll(now, seed, nil)
	next := []Job{seed[0], classifiedBackend("AI Researcher", "VinAI", "https://x/2")}
	e.Poll(now.Add(2*time.Hour), next, nil)

	restored := NewEngine()
	restored.Load(e.Persisted())
	if restored.baselinePending || len(restored.seen) != len(e.seen) ||
		len(restored.pending) != 1 || restored.totalMatched != 1 {
		t.Fatalf("state round trip lost data: %+v", restored)
	}
	// A restart replays nothing (design 5.5): refetching the same jobs
	// adds zero.
	newJobs, _ := restored.Poll(now.Add(4*time.Hour), next, nil)
	if len(newJobs) != 0 {
		t.Fatalf("restart must not replay, got %+v", newJobs)
	}
}

func TestAppendArchive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.jsonl")
	j := classifiedBackend("Backend Developer", "FPT", "https://x/1")
	now := testNow()
	if err := appendArchive(path, j, now); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := appendArchive(path, j, now); err != nil {
		t.Fatalf("append twice: %v", err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
		var raw map[string]any
		if err := json.Unmarshal(sc.Bytes(), &raw); err != nil {
			t.Fatalf("line %d not JSON: %v", lines, err)
		}
		if _, ok := raw["description"]; ok {
			t.Fatal("description must never be archived")
		}
		if raw["title"] != "Backend Developer" {
			t.Fatalf("bad record: %v", raw)
		}
		if _, ok := raw["first_seen"]; !ok {
			t.Fatal("archive record missing first_seen")
		}
	}
	if lines != 2 {
		t.Fatalf("want 2 lines, got %d", lines)
	}
}

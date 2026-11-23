package jobwatch

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func ictTime(day int, h, m int) time.Time {
	return time.Date(2026, 11, day, h, m, 0, 0, ICT)
}

func TestNextDigestTime(t *testing.T) {
	cases := []struct {
		now  time.Time
		want time.Time
	}{
		// 01:00 UTC == 08:00 ICT: a digest due right now is not re-armed
		// for the same minute — it goes to tomorrow (strictly after).
		{time.Date(2026, 11, 24, 1, 0, 0, 0, time.UTC), ictTime(25, 8, 0)},
		{ictTime(24, 7, 59), ictTime(24, 8, 0)},
		{ictTime(24, 8, 0), ictTime(25, 8, 0)},
		{ictTime(24, 23, 30), ictTime(25, 8, 0)},
	}
	for _, c := range cases {
		if got := nextDigestTime(c.now); !got.Equal(c.want) {
			t.Errorf("nextDigestTime(%v) = %v, want %v", c.now, got, c.want)
		}
	}
}

func mkJob(title, company, link string, backend, aidata, finance bool) Job {
	return Job{Title: title, Company: company, Link: link, Source: "ITViec", Backend: backend, AIData: aidata, Finance: finance}
}

func TestBuildDigestSectionsAndOrder(t *testing.T) {
	jobs := []Job{
		mkJob("B1", "FPT", "https://x/b1", true, false, false),
		mkJob("F1", "MB Bank", "https://x/f1", true, false, true),
		mkJob("A1", "VinAI", "https://x/a1", false, true, false),
		mkJob("F2", "TCBS", "https://x/f2", false, true, true),
		// Hits both sets → displayed as AI · Data, not Backend.
		mkJob("X1", "Tiki", "https://x/x1", true, true, false),
	}
	chunks := buildDigest(jobs, digestInfo{
		Now:          ictTime(24, 8, 0),
		WindowStart:  ictTime(23, 8, 0),
		APIOK:        true,
		LastPollAge:  time.Hour,
		TotalMatched: 5,
	})
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	c := chunks[0]
	// 5 new, 24 h window, finance section first with both finance jobs
	// (each posting in exactly one section).
	for _, want := range []string{
		"5 new matched",
		"24 h window",
		"🔥 Finance · Fintech (2)",
		"💼 Backend (1)",
		"🤖 AI · Data (2)",
		"F1 — MB Bank",
		"API ok · last poll 1h ago · 5 matched since deploy",
	} {
		if !strings.Contains(c, want) {
			t.Errorf("digest missing %q:\n%s", want, c)
		}
	}
	f1 := strings.Index(c, "F1")
	b1 := strings.Index(c, "B1")
	if f1 > b1 {
		t.Error("finance section must come before backend")
	}
	x1 := strings.Index(c, "X1")
	if x1 < b1 {
		t.Error("AI·Data section must come after backend in the digest")
	}
}

func TestBuildDigestEmptyStillSends(t *testing.T) {
	// The digest is the module's alive-signal: 0 jobs still sends one
	// message (design 5.4).
	chunks := buildDigest(nil, digestInfo{Now: ictTime(24, 8, 0), APIOK: false})
	if len(chunks) != 1 {
		t.Fatalf("want 1 chunk, got %d", len(chunks))
	}
	if !strings.Contains(chunks[0], "0 new matched") {
		t.Errorf("empty digest missing count: %s", chunks[0])
	}
	if !strings.Contains(chunks[0], "API down") {
		t.Errorf("empty digest missing API state: %s", chunks[0])
	}
	if !strings.Contains(chunks[0], "first window") {
		t.Errorf("first digest must say so: %s", chunks[0])
	}
}

func TestBuildDigestCapAndMore(t *testing.T) {
	var jobs []Job
	for i := 0; i < maxDigestJobs+7; i++ {
		jobs = append(jobs, mkJob(fmt.Sprintf("Job %02d", i), "FPT", fmt.Sprintf("https://x/j%d", i), true, false, false))
	}
	chunks := buildDigest(jobs, digestInfo{Now: ictTime(24, 8, 0), APIOK: true})
	total := strings.Join(chunks, "\n")
	if !strings.Contains(total, "+7 more — all in jobs.jsonl") {
		t.Errorf("missing +7 footnote in:\n%s", total)
	}
	shown := strings.Count(total, "](https://x/")
	if shown != maxDigestJobs {
		t.Errorf("showed %d jobs, want cap %d", shown, maxDigestJobs)
	}
}

func TestChunkDigestLimit(t *testing.T) {
	var lines []string
	for i := 0; i < 80; i++ {
		lines = append(lines, fmt.Sprintf("- [%s](https://itviec.com/it-jobs/some-posting-slug-with-a-fairly-long-url-%d)", strings.Repeat("long title ", 10), i))
	}
	chunks := chunkDigest("📰 header", lines, "\nfooter")
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, c := range chunks {
		if len(c) > maxDiscordChunk {
			t.Errorf("chunk %d too long: %d", i, len(c))
		}
	}
	if !strings.HasPrefix(chunks[0], "📰") {
		t.Error("header must ride the first chunk")
	}
	if !strings.Contains(chunks[len(chunks)-1], "footer") {
		t.Error("footer must ride the last chunk")
	}
}

func TestFmtAge(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Minute: "45m",
		3 * time.Hour:    "3h",
		72 * time.Hour:   "3d",
	}
	for in, want := range cases {
		if got := fmtAge(in); got != want {
			t.Errorf("fmtAge(%v) = %q, want %q", in, got, want)
		}
	}
}

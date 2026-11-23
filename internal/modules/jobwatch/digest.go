package jobwatch

import (
	"fmt"
	"strings"
	"time"
)

// digestHour is the ICT hour the digest fires: 08:00, chosen by Khang
// (design 4).
const digestHour = 8

// maxDigestJobs caps a digest so one noisy day cannot flood the channel;
// everything is in the archive, the digest says how much was cut
// (design 5.4).
const maxDigestJobs = 30

// maxDiscordChunk keeps each message under Discord's 2000-char limit with
// headroom (design 5.4).
const maxDiscordChunk = 1900

// maxLineRunes keeps one posting's visible text to one visual line
// (design 7).
const maxLineRunes = 110

// nextDigestTime returns the next 08:00 ICT strictly after now
// (design 5.4).
func nextDigestTime(now time.Time) time.Time {
	ict := now.In(ICT)
	y, m, d := ict.Date()
	t := time.Date(y, m, d, digestHour, 0, 0, 0, ICT)
	if !ict.Before(t) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

// digestInfo carries the engine snapshot the footer reports (design 5.4).
type digestInfo struct {
	Now          time.Time
	WindowStart  time.Time // zero → first digest after deploy
	APIOK        bool
	LastPollAge  time.Duration // zero when no poll ever succeeded
	TotalMatched int
}

// buildDigest renders the digest as ready-to-send chunks: header, sections
// (🔥 finance first, then 💼 backend, 🤖 AI·Data — a posting appears in
// exactly one section), "+N more" footnote, API footer. Always returns at
// least one chunk — the digest doubles as the module's alive-signal
// (design 5.4).
func buildDigest(jobs []Job, d digestInfo) []string {
	ict := d.Now.In(ICT)

	var finance, backend, aidata []Job
	for _, j := range jobs {
		switch {
		case j.Finance:
			finance = append(finance, j)
		case j.AIData:
			// A job that hits both sets (an AI engineer with Python in
			// its skills) is shown as AI · Data — that is the role it
			// reads as (design 5.4).
			aidata = append(aidata, j)
		case j.Backend:
			backend = append(backend, j)
		default:
			// Wanted() guarantees Backend or AIData; defensive.
			aidata = append(aidata, j)
		}
	}

	header := fmt.Sprintf("📰 VN IT jobs — %s · %d new matched · %s",
		ict.Format("Mon 02/01"), len(jobs), windowLabel(d))
	if len(jobs) == 0 {
		header += " — quiet day"
	}

	shown, hidden := 0, 0
	var lines []string
	for _, sec := range []struct {
		title string
		list  []Job
	}{
		{"🔥 Finance · Fintech", finance},
		{"💼 Backend", backend},
		{"🤖 AI · Data", aidata},
	} {
		if len(sec.list) == 0 {
			continue
		}
		lines = append(lines, fmt.Sprintf("\n%s (%d)", sec.title, len(sec.list)))
		for _, j := range sec.list {
			if shown >= maxDigestJobs {
				hidden++
				continue
			}
			lines = append(lines, jobLine(j))
			shown++
		}
	}
	if hidden > 0 {
		lines = append(lines, fmt.Sprintf("\n+%d more — all in jobs.jsonl", hidden))
	}
	footer := "\n" + footerLine(d)
	return chunkDigest(header, lines, footer)
}

// footerLine reports the module's own health so Khang can tell "quiet
// market" from "broken bot" at a glance (design 5.4).
func footerLine(d digestInfo) string {
	api := "API ok"
	if !d.APIOK {
		api = "API down"
	}
	poll := "no successful poll yet"
	if d.LastPollAge > 0 {
		poll = "last poll " + fmtAge(d.LastPollAge) + " ago"
	}
	return fmt.Sprintf("%s · %s · %d matched since deploy", api, poll, d.TotalMatched)
}

// windowLabel describes the digest window in the header (design 5.4).
func windowLabel(d digestInfo) string {
	if d.WindowStart.IsZero() {
		return "first window"
	}
	h := d.Now.Sub(d.WindowStart) / time.Hour
	if h < 1 {
		return "<1 h window"
	}
	return fmt.Sprintf("%d h window", h)
}

// jobLine renders one posting: title — company — location — salary —
// source, linked to the original posting (design 5.4). Empty fields are
// skipped; the goodjobs cache links stay clickable as-is.
func jobLine(j Job) string {
	parts := make([]string, 0, 5)
	for _, s := range []string{j.Title, j.Company, j.Location, j.Salary, j.Source} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	text := strings.Join(parts, " — ")
	if r := []rune(text); len(r) > maxLineRunes {
		text = string(r[:maxLineRunes-1]) + "…"
	}
	return "- [" + text + "](" + j.Link + ")"
}

// chunkDigest packs header + lines + footer into ≤maxDiscordChar chunks,
// split on line boundaries. Header rides the first chunk, footer the
// last (design 5.4).
func chunkDigest(header string, lines []string, footer string) []string {
	var chunks []string
	cur := header
	flush := func() {
		chunks = append(chunks, cur)
	}
	for _, l := range lines {
		if len(cur)+len(l)+1 > maxDiscordChunk {
			flush()
			cur = "…"
		}
		if cur != "" {
			cur += "\n"
		}
		cur += l
	}
	cur += footer
	flush()
	return chunks
}

// fmtAge renders a duration for the footer: minutes under an hour, hours
// under two days, days beyond (design 7).
func fmtAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// ageSince returns 0 when t is zero, else now−t (design 5.4).
func ageSince(now, t time.Time) time.Duration {
	if t.IsZero() {
		return 0
	}
	return now.Sub(t)
}

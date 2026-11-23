// Package jobwatch is the bot's second feature module (design
// docs/superpowers/specs/2026-11-23-jobwatch-design.md): it polls the VN IT
// job market via goodjobs.io.vn every 2 h, keeps a dedup archive of matched
// Backend/AI/Data postings, and posts one digest at 08:00 ICT.
package jobwatch

import (
	"context"
	"time"
)

// Job is one posting as the digest and archive see it. Fields mirror the
// goodjobs response (design 3); the description is deliberately not carried
// — it is fetch-only, never stored (design 5.2).
type Job struct {
	Title    string   `json:"title"`
	Company  string   `json:"company"`
	Location string   `json:"location"`
	Link     string   `json:"link"`
	Source   string   `json:"source"`
	Salary   string   `json:"salary"`
	Skills   []string `json:"skills"`

	// Match results, set by Classify (design 5.3).
	Backend bool `json:"backend"`
	AIData  bool `json:"ai_data"`
	Finance bool `json:"finance"`
}

// Wanted reports whether the posting belongs in the digest: it must hit the
// Backend or AI·Data set; the finance flag only decides the section
// (design 5.3).
func (j Job) Wanted() bool { return j.Backend || j.AIData }

// JobSource is one place postings come from. v1 ships the goodjobs client
// (goodjobs.go); the interface is the "plug more sources in later" hook
// chosen by Khang (design 4) — a scraper or a replacement service is a new
// implementation, not a digest change.
type JobSource interface {
	// Fetch returns the current postings. Implementations must be
	// read-only and must not retry inside one call — the poll loop's next
	// tick is the retry (design 5.2).
	Fetch(ctx context.Context) ([]Job, error)
}

// ICT is Vietnam's fixed UTC+7 zone: no DST, so a constant replaces a
// tzdata dependency (design 5.7).
var ICT = time.FixedZone("ICT", 7*60*60)

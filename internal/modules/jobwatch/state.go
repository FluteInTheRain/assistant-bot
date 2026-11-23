package jobwatch

import (
	"fmt"
	"time"
)

// OutMsg is a message one step wants delivered (mirrors ociwatch.OutMsg;
// design 5.5).
type OutMsg struct {
	Alert bool // true → mention the owner
	Text  string
}

const stateVersion = 1

// apiFailAlertAfter: 3 consecutive failed polls (~6 h at one poll per 2 h)
// → one Alert (design 5.5).
const apiFailAlertAfter = 3

// seenRetention prunes the seen-set to 45 days: goodjobs keeps postings
// 14 days, the slack covers a future source that keeps them longer
// (design 5.2).
const seenRetention = 45 * 24 * time.Hour

// Engine turns fetch results into digest content, holding all state that
// survives restarts. Like ociwatch.Watcher it is pure: no clock, no disk,
// no network — everything arrives as arguments (design 7).
type Engine struct {
	seen    map[string]time.Time // dedup keys → first-seen
	pending []Job                // matched, waiting for the next digest
	// lastDigest anchors the digest window (design 5.4); zero until the
	// first digest.
	lastDigest time.Time
	lastOK     time.Time // last successful poll
	apiFails   int
	// apiProblemSaid is the "goodjobs unreachable" state: set once the
	// alert fired, cleared by a recovery (design 5.5).
	apiProblemSaid bool
	totalMatched   int
	// baselinePending is true until the first successful fetch: the
	// baseline must be taken silently, and a failed first fetch must not
	// turn pre-existing postings into a flood (design 5.2).
	baselinePending bool
}

func NewEngine() *Engine {
	return &Engine{seen: map[string]time.Time{}, baselinePending: true}
}

// persistedState is the JSON saved between runs (design 5.6).
type persistedState struct {
	Version        int                  `json:"version"`
	Seen           map[string]time.Time `json:"seen"`
	Pending        []Job                `json:"pending"`
	LastDigest     time.Time            `json:"last_digest"`
	LastOK         time.Time            `json:"last_ok"`
	APIFails       int                  `json:"api_fails"`
	APIProblemSaid bool                 `json:"api_problem_said"`
	TotalMatched   int                  `json:"total_matched"`
}

func (e *Engine) Load(p persistedState) {
	e.seen = map[string]time.Time{}
	for k, v := range p.Seen {
		e.seen[k] = v
	}
	e.pending = p.Pending
	e.lastDigest = p.LastDigest
	e.lastOK = p.LastOK
	e.apiFails = p.APIFails
	e.apiProblemSaid = p.APIProblemSaid
	e.totalMatched = p.TotalMatched
	e.baselinePending = false
}

func (e *Engine) Persisted() persistedState {
	return persistedState{
		Version:        stateVersion,
		Seen:           e.seen,
		Pending:        e.pending,
		LastDigest:     e.lastDigest,
		LastOK:         e.lastOK,
		APIFails:       e.apiFails,
		APIProblemSaid: e.apiProblemSaid,
		TotalMatched:   e.totalMatched,
	}
}

// Poll ingests one fetch result and returns (a) matched new postings the
// caller must archive and (b) messages to send. First-seen semantics: a
// posting is new the first time the bot sees it — posted_ts is not
// trusted (design 5.2). Only matched postings enter the seen-set, so the
// state file stays small.
func (e *Engine) Poll(now time.Time, jobs []Job, fetchErr error) (newJobs []Job, msgs []OutMsg) {
	if fetchErr != nil {
		e.apiFails++
		if e.apiFails >= apiFailAlertAfter && !e.apiProblemSaid {
			e.apiProblemSaid = true
			msgs = append(msgs, OutMsg{
				Alert: true,
				Text: fmt.Sprintf("🚨 goodjobs API unreachable for ~%d h (%d failed polls). Digest may be incomplete until it recovers.",
					int(pollInterval.Seconds())*e.apiFails/3600, e.apiFails),
			})
		}
		return nil, msgs
	}

	e.apiFails = 0
	if e.apiProblemSaid {
		e.apiProblemSaid = false
		msgs = append(msgs, OutMsg{Text: "✅ goodjobs API reachable again."})
	}
	e.lastOK = now

	// First successful fetch: silent baseline, no digest content
	// (design 5.2).
	if e.baselinePending {
		for _, j := range jobs {
			e.markSeen(j, now)
		}
		e.baselinePending = false
		return nil, msgs
	}

	for _, j := range jobs {
		if !j.Wanted() {
			continue
		}
		k1, k2 := linkKey(j.Link), roleKey(j.Company, j.Title)
		if _, dup := e.seen[k1]; dup {
			continue
		}
		if _, dup := e.seen[k2]; dup {
			continue
		}
		e.markSeen(j, now)
		e.pending = append(e.pending, j)
		e.totalMatched++
		newJobs = append(newJobs, j)
	}
	e.prune(now)
	return newJobs, msgs
}

func (e *Engine) markSeen(j Job, now time.Time) {
	e.seen[linkKey(j.Link)] = now
	e.seen[roleKey(j.Company, j.Title)] = now
}

func (e *Engine) prune(now time.Time) {
	for k, t := range e.seen {
		if now.Sub(t) > seenRetention {
			delete(e.seen, k)
		}
	}
}

// Digest returns the digest message chunks for now. Pending is cleared and
// lastDigest anchored here; the caller persists before sending, so a crash
// between the two at worst folds one window into the next digest — the
// archive already has every posting (design 5.5).
func (e *Engine) Digest(now time.Time) []OutMsg {
	apiOK := !e.lastOK.IsZero() && e.apiFails < apiFailAlertAfter
	chunks := buildDigest(e.pending, digestInfo{
		Now:          now,
		WindowStart:  e.lastDigest,
		APIOK:        apiOK,
		LastPollAge:  ageSince(now, e.lastOK),
		TotalMatched: e.totalMatched,
	})
	e.pending = nil
	e.lastDigest = now
	out := make([]OutMsg, len(chunks))
	for i, c := range chunks {
		out[i] = OutMsg{Text: c}
	}
	return out
}

// StatusText is the module's snapshot line. Nothing surfaces it in v1.1 —
// a /status that aggregates modules ships together with the deferred /jobs
// command (design 5.6).
func (e *Engine) StatusText(now time.Time) string {
	api := "down"
	if !e.lastOK.IsZero() && e.apiFails < apiFailAlertAfter {
		api = "ok"
	}
	return fmt.Sprintf("jobwatch: %d pending, API %s, last poll %s, %d matched total",
		len(e.pending), api, ageSince(now, e.lastOK), e.totalMatched)
}

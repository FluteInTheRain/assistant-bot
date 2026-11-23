package jobwatch

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/FluteInTheRain/assistant-bot/internal/core"
)

// Paths pinned by the design (5.6): a separate state file next to
// ociwatch's, and the append-only archive.
const (
	defaultStatePath   = "/home/ubuntu/.local/state/assistant-bot/jobwatch-state.json"
	defaultArchivePath = "/home/ubuntu/.local/state/assistant-bot/jobs.jsonl"
)

// pollInterval: one GET every 2 h, aligned with goodjobs' own cache
// refresh (design 5.2).
const pollInterval = 2 * time.Hour

// Deps carries everything the module needs from the core. Source,
// StatePath, ArchivePath and Now exist so tests can fake them; production
// leaves them nil/empty (same convention as ociwatch.Deps).
type Deps struct {
	Log         *slog.Logger
	Notifier    core.Notifier
	Source      JobSource        // nil → the real goodjobs client
	StatePath   string           // "" → defaultStatePath
	ArchivePath string           // "" → defaultArchivePath
	Now         func() time.Time // nil → time.Now
}

// Module implements core.Module (design 5.6).
type Module struct {
	deps        Deps
	e           *Engine
	statePath   string
	archivePath string
}

// New builds the module and restores the persisted state, if any
// (mirrors ociwatch.New).
func New(deps Deps) *Module {
	if deps.StatePath == "" {
		deps.StatePath = defaultStatePath
	}
	if deps.ArchivePath == "" {
		deps.ArchivePath = defaultArchivePath
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.Source == nil {
		deps.Source = &Goodjobs{}
	}
	m := &Module{
		deps:        deps,
		e:           NewEngine(),
		statePath:   deps.StatePath,
		archivePath: deps.ArchivePath,
	}

	var p persistedState
	found, err := core.LoadJSONState(m.statePath, &p)
	switch {
	case err != nil:
		deps.Log.Warn("jobwatch state unreadable; starting with a fresh baseline", "err", err)
	case found:
		m.e.Load(p)
		deps.Log.Info("jobwatch state restored", "seen", len(p.Seen), "pending", len(p.Pending), "total_matched", p.TotalMatched)
	default:
		deps.Log.Info("jobwatch: no state file yet; first fetch takes a silent baseline")
	}
	return m
}

func (m *Module) Name() string { return "jobwatch" }

// Commands is empty in v1.1: /jobs is deferred with Khang, together with a
// /status that aggregates modules (design 2, 5.6).
func (m *Module) Commands() []*discordgo.ApplicationCommand {
	return nil
}

// HandleCommand never fires in v1.1 (no commands); kept defensive so a
// future registration mistake logs instead of panicking.
func (m *Module) HandleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	m.deps.Log.Warn("jobwatch got a command it does not define", "id", i.ID)
}

// StatusText is the module's /status section. Nothing surfaces it in v1.1
// (design 5.6) — implemented now so the Module interface stays satisfied.
func (m *Module) StatusText() string { return m.e.StatusText(m.deps.Now()) }

// Run polls immediately (so a restart catches up with whatever goodjobs
// has), then every 2 h; the digest fires at each 08:00 ICT (design 5.2,
// 5.4).
func (m *Module) Run(ctx context.Context) {
	m.poll(ctx)
	pollT := time.NewTicker(pollInterval)
	defer pollT.Stop()
	for {
		digestT := time.NewTimer(nextDigestTime(m.deps.Now()).Sub(m.deps.Now()))
		select {
		case <-ctx.Done():
			digestT.Stop()
			return
		case <-pollT.C:
			m.poll(ctx)
		case <-digestT.C:
			m.digest()
		}
	}
}

// poll: fetch → engine → persist → archive → notify. Persisting before
// archiving/sending follows ociwatch's order: a crash right after Poll
// never re-processes a posting (design 5.2).
func (m *Module) poll(ctx context.Context) {
	now := m.deps.Now()
	jobs, err := m.deps.Source.Fetch(ctx)
	if err != nil {
		m.deps.Log.Error("jobwatch fetch failed", "err", err)
	}
	newJobs, msgs := m.e.Poll(now, jobs, err)
	m.save()
	for _, j := range newJobs {
		if err := appendArchive(m.archivePath, j, now); err != nil {
			m.deps.Log.Error("archive append failed", "err", err)
		}
	}
	m.send(msgs)
}

// digest: engine clears pending → persist → send. The state file is saved
// before the messages go out, so a crash between the two at worst folds
// one window into the next digest (design 5.5).
func (m *Module) digest() {
	now := m.deps.Now()
	msgs := m.e.Digest(now)
	m.save()
	m.send(msgs)
	m.deps.Log.Info("digest sent", "chunks", len(msgs))
}

func (m *Module) save() {
	if err := core.SaveJSONState(m.statePath, m.e.Persisted()); err != nil {
		m.deps.Log.Error("jobwatch state save failed", "err", err)
	}
}

func (m *Module) send(msgs []OutMsg) {
	for _, msg := range msgs {
		if msg.Alert {
			m.deps.Notifier.Alert(msg.Text)
		} else {
			m.deps.Notifier.Info(msg.Text)
		}
	}
}

// archiveRecord is one jobs.jsonl line: the Job fields plus first_seen.
// The description is never stored (design 5.2); the embedded Job flattens
// its fields inline.
type archiveRecord struct {
	FirstSeen time.Time `json:"first_seen"`
	Job
}

// appendArchive writes one matched posting to the append-only archive —
// the "collect data to process later" store chosen by Khang (design 1).
// Pruning never touches this file (design 5.2).
func appendArchive(path string, j Job, now time.Time) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	line, err := json.Marshal(archiveRecord{FirstSeen: now, Job: j})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

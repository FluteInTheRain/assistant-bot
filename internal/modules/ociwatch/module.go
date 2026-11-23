// Package ociwatch is the bot's first feature module (design 5.1): every
// 5 minutes it checks the OCI instances, the host's disk/memory/CPU, the
// grab-servers hunter, and answers /status from its latest snapshot.
package ociwatch

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/bwmarrin/discordgo"

	"github.com/FluteInTheRain/assistant-bot/internal/core"
)

// stateFilePath is pinned by the design (5.6).
const stateFilePath = "/home/ubuntu/.local/state/assistant-bot/state.json"

// Deps carries everything the module needs from the core. OCI, StatePath and
// Now exist so tests can fake them; production leaves them nil/empty.
type Deps struct {
	Log       *slog.Logger
	Notifier  core.Notifier
	Pinger    *core.Pinger
	OCI       InstanceLister  // nil → the real instance-principal client
	StatePath string         // "" → stateFilePath
	Now       func() time.Time // nil → time.Now
}

// Module implements core.Module.
type Module struct {
	deps      Deps
	w         *Watcher
	oci       InstanceLister
	statePath string
}

// New builds the module and restores the persisted state, if any.
func New(deps Deps) *Module {
	if deps.StatePath == "" {
		deps.StatePath = stateFilePath
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.OCI != nil {
		deps.Log.Warn("custom OCI lister injected (test/degraded mode)")
	}
	m := &Module{
		deps:      deps,
		w:         NewWatcher(deps.Now),
		oci:       deps.OCI,
		statePath: deps.StatePath,
	}
	if host, err := os.Hostname(); err == nil {
		m.w.Host = host
	}

	var p persistedState
	found, err := core.LoadJSONState(m.statePath, &p)
	switch {
	case err != nil:
		deps.Log.Warn("state file unreadable; starting with a fresh baseline", "err", err)
	case found:
		m.w.Load(p)
		deps.Log.Info("state restored", "instances", len(p.Instances), "hunter_done_said", p.HunterDoneSaid)
	default:
		deps.Log.Info("no state file yet; first tick posts a started summary")
	}
	return m
}

func (m *Module) Name() string { return "ociwatch" }

func (m *Module) Commands() []*discordgo.ApplicationCommand {
	return []*discordgo.ApplicationCommand{{
		Name:        "status",
		Description: "Latest host + OCI snapshot (owner only)",
	}}
}

// HandleCommand answers /status. The core has already verified the user is
// the owner; the reply is ephemeral so only Khang sees it (design 5.4).
func (m *Module) HandleCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: "```\n" + m.StatusText() + "\n```",
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		m.deps.Log.Error("/status respond failed", "err", err)
	}
}

// StatusText exposes the module's /status section.
func (m *Module) StatusText() string { return m.w.StatusText() }

// Run ticks once immediately (catching anything that happened while the bot
// was down), then every CheckInterval until ctx is cancelled.
func (m *Module) Run(ctx context.Context) {
	m.tick(ctx)
	t := time.NewTicker(CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.tick(ctx)
		}
	}
}

// tick: collect → decide → persist → notify → ping. Persisting before
// notifying means a crash right after a transition never repeats the alert.
func (m *Module) tick(ctx context.Context) {
	r := m.collect(ctx)
	msgs := m.w.Tick(r)
	if err := core.SaveJSONState(m.statePath, m.w.Persisted()); err != nil {
		m.deps.Log.Error("state save failed", "err", err)
	}
	for _, msg := range msgs {
		if msg.Alert {
			m.deps.Notifier.Alert(msg.Text)
		} else {
			m.deps.Notifier.Info(msg.Text)
		}
	}
	// Dead-man's switch: proves the check loop actually ran (design 5.8).
	m.deps.Pinger.Ping()
}

// collect gathers one Reading: local host facts first (never blocked by the
// network), then the single OCI list call, bounded by its own timeout.
func (m *Module) collect(ctx context.Context) Reading {
	now := m.deps.Now()
	r := Reading{Now: now, HostUp: formatUp(hostUptime())}

	// Host — best effort: /proc always answers on Linux; on failure the
	// zero values cannot trip a threshold and the error is logged.
	disk, err := readDiskUsedPct()
	if err != nil {
		m.deps.Log.Error("disk read failed", "err", err)
	}
	r.DiskUsedPct = disk

	mem, err := readMemAvailPct()
	if err != nil {
		m.deps.Log.Error("meminfo read failed", "err", err)
	}
	r.MemAvailPct = mem

	load, numCPU, err := readLoadPerCore()
	if err != nil {
		m.deps.Log.Error("loadavg read failed", "err", err)
	}
	r.LoadPerCore, r.NumCPU = load, numCPU

	// Hunter — read-only probes only (anti-goal: never interfere with it).
	r.HunterActive = hunterServiceActive()
	age, err := hunterLogAge(now)
	if err != nil {
		if !os.IsNotExist(err) {
			m.deps.Log.Error("grab.log stat failed", "err", err)
		}
		r.HunterLogAge = -1
	} else {
		r.HunterLogAge = age
	}

	// OCI — the only network call in a tick.
	if m.oci == nil {
		m.oci = NewOCICollector(m.deps.Log)
	}
	listCtx, cancel := context.WithTimeout(ctx, ociCallTimeout)
	defer cancel()
	insts, err := m.oci.List(listCtx)
	if err != nil {
		r.OCIOK, r.OCIError = false, err.Error()
	} else {
		r.OCIOK, r.Instances = true, insts
	}
	return r
}

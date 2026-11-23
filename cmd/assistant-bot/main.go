// assistant-bot: Khang's personal Discord assistant. Two modules run in
// one process on the OCI VM `side-projects`: ociwatch (every 5 min, OCI
// instances / host / hunter, /status) and jobwatch (every 2 h, VN IT job
// market via goodjobs.io.vn, digest at 08:00 ICT). Design docs:
// docs/superpowers/specs/2026-11-23-oci-watch-design.md,
// docs/superpowers/specs/2026-11-23-jobwatch-design.md.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/FluteInTheRain/assistant-bot/internal/core"
	"github.com/FluteInTheRain/assistant-bot/internal/modules/jobwatch"
	"github.com/FluteInTheRain/assistant-bot/internal/modules/ociwatch"
)

func main() {
	// journald collects stdout/stderr (design 5.7).
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))

	cfg, err := core.LoadConfig()
	if err != nil {
		log.Error("refusing to start", "err", err) // never log config values
		os.Exit(1)
	}

	session, err := core.NewSession(cfg.DiscordToken)
	if err != nil {
		log.Error("discord connect failed", "err", err)
		os.Exit(1)
	}
	defer session.Close()

	notifier, stopNotifier := core.NewNotifier(session, cfg.DiscordChannelID, cfg.OwnerUserID, log)
	defer stopNotifier()
	// One channel per module (jobwatch design 5.1, 5.6): the digest and
	// jobwatch's own health land in #jobs, not in #server.
	jobsNotifier, stopJobsNotifier := core.NewNotifier(session, cfg.JobwatchChannelID, cfg.OwnerUserID, log)
	defer stopJobsNotifier()
	pinger := core.NewPinger(cfg.HealthcheckURL, log)

	ociModule := ociwatch.New(ociwatch.Deps{
		Log:      log,
		Notifier: notifier,
		Pinger:   pinger,
	})
	jobModule := jobwatch.New(jobwatch.Deps{
		Log:      log,
		Notifier: jobsNotifier,
	})
	modules := []core.Module{ociModule, jobModule}

	// A panicking module reports into its own channel (jobwatch design
	// 5.5); unknown future modules fall back to the alert channel.
	moduleNotifiers := map[string]core.Notifier{
		"ociwatch": notifier,
		"jobwatch": jobsNotifier,
	}

	// Slash commands, guild-scoped so they are available instantly (5.4);
	// the dispatcher answers only the owner and logs denied attempts.
	session.AddHandler(core.NewCommandDispatcher(cfg.OwnerUserID, modules, log))
	if err := core.RegisterCommands(session, cfg.DiscordGuildID, modules, log); err != nil {
		log.Error("command registration failed", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Each module runs in its own goroutine behind a recover: one module
	// panicking must not kill the bot, and the panic is reported to that
	// module's channel (design 5.5). A dead ociwatch also stops the
	// healthcheck pings, so the dead-man's switch flags the silence within
	// ~15 min.
	for _, m := range modules {
		panicNotifier := moduleNotifiers[m.Name()]
		if panicNotifier == nil {
			panicNotifier = notifier
		}
		go runModule(ctx, m, panicNotifier, log)
	}

	<-ctx.Done()
	log.Info("shutting down")
}

func runModule(ctx context.Context, m core.Module, notifier core.Notifier, log *slog.Logger) {
	defer func() {
		if r := recover(); r != nil {
			log.Error("module panicked", "module", m.Name(), "panic", r)
			notifier.Alert(fmt.Sprintf("🚨 module `%s` panicked and stopped: %v — check `journalctl -u assistant-bot`; healthchecks.io will flag the silence soon.", m.Name(), r))
		}
	}()
	m.Run(ctx)
}

package core

import (
	"context"

	"github.com/bwmarrin/discordgo"
)

// Module is one feature of the bot (design 5.5). Modules are independent:
// each runs in its own goroutine behind a recover, so one module panicking
// does not kill the bot, and later modules can be added without touching the
// OCI module.
type Module interface {
	// Name identifies the module in logs and panic reports.
	Name() string
	// Run blocks until ctx is cancelled, doing the module's periodic work.
	Run(ctx context.Context)
	// Commands are the module's slash commands, registered guild-scoped so
	// they are available instantly in the owner's private server (design 5.4).
	Commands() []*discordgo.ApplicationCommand
	// HandleCommand serves one of the module's slash commands. The core has
	// already verified the invoking user is the owner.
	HandleCommand(s *discordgo.Session, i *discordgo.InteractionCreate)
	// StatusText is the module's section of the /status reply, built from the
	// latest snapshot — never a live call (design 5.4).
	StatusText() string
}

package core

import (
	"fmt"
	"log/slog"

	"github.com/bwmarrin/discordgo"
)

// NewSession opens the Discord gateway connection. No privileged intents are
// needed (design 6.2): the bot only reads slash-command interactions and sends
// REST messages.
func NewSession(token string) (*discordgo.Session, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("discord session: %w", err)
	}
	if err := session.Open(); err != nil {
		session.Close()
		return nil, fmt.Errorf("discord gateway connect: %w", err)
	}
	return session, nil
}

// RegisterCommands registers every module command in the owner's guild.
// Guild-scoped commands take effect instantly, no global propagation wait
// (design 5.4). Re-registering on restart is idempotent.
func RegisterCommands(session *discordgo.Session, guildID string, modules []Module, log *slog.Logger) error {
	appID := session.State.User.ID
	for _, m := range modules {
		for _, cmd := range m.Commands() {
			if _, err := session.ApplicationCommandCreate(appID, guildID, cmd); err != nil {
				return fmt.Errorf("register /%s: %w", cmd.Name, err)
			}
			log.Info("slash command ready", "command", "/"+cmd.Name, "guild", guildID)
		}
	}
	return nil
}

// NewCommandDispatcher returns the InteractionCreate handler. It answers only
// the owner: anyone else gets an ephemeral "not allowed" and the attempt is
// logged (design 5.4).
func NewCommandDispatcher(ownerID string, modules []Module, log *slog.Logger) func(*discordgo.Session, *discordgo.InteractionCreate) {
	handlers := make(map[string]Module)
	for _, m := range modules {
		for _, cmd := range m.Commands() {
			handlers[cmd.Name] = m
		}
	}
	return func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		if i.Type != discordgo.InteractionApplicationCommand {
			return
		}
		name := i.ApplicationCommandData().Name
		userID := interactionUserID(i)
		if userID != ownerID {
			log.Warn("slash command denied", "command", "/"+name, "user", userID)
			respondEphemeral(s, i, log, "Not allowed.")
			return
		}
		m, ok := handlers[name]
		if !ok {
			log.Warn("unknown slash command", "command", "/"+name)
			respondEphemeral(s, i, log, "Unknown command.")
			return
		}
		m.HandleCommand(s, i)
	}
}

func interactionUserID(i *discordgo.InteractionCreate) string {
	if i.User != nil {
		return i.User.ID
	}
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	return ""
}

func respondEphemeral(s *discordgo.Session, i *discordgo.InteractionCreate, log *slog.Logger, text string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: text,
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})
	if err != nil {
		log.Error("interaction respond failed", "err", err)
	}
}

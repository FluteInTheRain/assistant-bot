// Package core holds the bot-wide plumbing: config, the Discord session,
// owner check, the alert notifier, the healthcheck pinger and the state file.
// Feature modules (internal/modules/*) plug into this core.
package core

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Config holds every key loaded by systemd from EnvironmentFile=.
// See deploy/env.example. There are no defaults: the bot refuses to start
// if any key is missing (design 5.6).
type Config struct {
	// DiscordToken is the bot token. Secret: never logged, never committed.
	DiscordToken string
	// DiscordGuildID is Khang's private server; slash commands register here.
	DiscordGuildID string
	// DiscordChannelID is the #server channel that receives alerts.
	DiscordChannelID string
	// OwnerUserID is the only Discord user the bot answers.
	OwnerUserID string
	// HealthcheckURL is the healthchecks.io ping URL. Secret: never logged.
	HealthcheckURL string
}

// LoadConfig reads the required keys from the environment.
func LoadConfig() (Config, error) {
	cfg := Config{
		DiscordToken:     os.Getenv("DISCORD_TOKEN"),
		DiscordGuildID:   os.Getenv("DISCORD_GUILD_ID"),
		DiscordChannelID: os.Getenv("DISCORD_CHANNEL_ID"),
		OwnerUserID:      os.Getenv("OWNER_USER_ID"),
		HealthcheckURL:   os.Getenv("HEALTHCHECK_URL"),
	}
	var missing []string
	for k, v := range map[string]string{
		"DISCORD_TOKEN":     cfg.DiscordToken,
		"DISCORD_GUILD_ID":  cfg.DiscordGuildID,
		"DISCORD_CHANNEL_ID": cfg.DiscordChannelID,
		"OWNER_USER_ID":     cfg.OwnerUserID,
		"HEALTHCHECK_URL":   cfg.HealthcheckURL,
	} {
		if v == "" {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return Config{}, fmt.Errorf("missing required config keys: %s (see deploy/env.example)", strings.Join(missing, ", "))
	}
	return cfg, nil
}

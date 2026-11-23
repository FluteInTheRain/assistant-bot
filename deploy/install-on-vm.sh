#!/usr/bin/env bash
# Run ON the VM (side-projects) as ubuntu, from the directory holding the
# uploaded files (assistant-bot, assistant-bot.service). Installs the binary
# and unit, then enables + starts the service.
#
# Before this: create /home/ubuntu/.config/assistant-bot/env (mode 600) —
# see deploy/env.example. The bot refuses to start without it.
set -euo pipefail

test -f ./assistant-bot || { echo "missing ./assistant-bot (binary)"; exit 1; }
test -f ./assistant-bot.service || { echo "missing ./assistant-bot.service"; exit 1; }
test "$(uname -m)" = "aarch64" || { echo "this VM is $(uname -m), expected aarch64"; exit 1; }

ENV_FILE="$HOME/.config/assistant-bot/env"
test -f "$ENV_FILE" || { echo "missing $ENV_FILE — create it from env.example first"; exit 1; }
chmod 600 "$ENV_FILE"

mkdir -p "$HOME/.local/state/assistant-bot" # ReadWritePaths must exist

sudo install -m 0755 ./assistant-bot /usr/local/bin/assistant-bot
sudo install -m 0644 ./assistant-bot.service /etc/systemd/system/assistant-bot.service
sudo systemctl daemon-reload
sudo systemctl enable --now assistant-bot
sleep 2
systemctl --no-pager --lines=20 status assistant-bot || true

echo
echo "watch it live:  journalctl -u assistant-bot -f"
echo "verify:        /status in Discord, and healthchecks.io showing pings"

# Deploying assistant-bot v1

> VI: Các bước triển khai bot lên VM `side-projects`.

Follow the approved design (§6): Khang does steps 1–4 by hand, Claude prepared
everything else (binary, unit file, scripts, commands).

## 1. Discord app (once)

1. <https://discord.com/developers/applications> → **New Application** → name it.
2. **Bot** tab → **Reset Token** → copy the token (secret).
3. Invite the bot to your private server with this URL (replace `APP_ID`,
   shown on the General Information tab):

   ```
   https://discord.com/oauth2/authorize?client_id=APP_ID&scope=bot+applications.commands&permissions=1280
   ```

   `1280` = View Channel + Send Messages. No privileged intents needed.
4. In Discord, enable **Developer Mode** (Settings → Advanced), then copy:
   - the server ID (right-click server → Copy Server ID)
   - the `#server` channel ID (right-click channel → Copy Channel ID)
   - your own user ID (right-click your name → Copy User ID)

## 2. healthchecks.io (once)

1. Free account at <https://healthchecks.io> → **Add Check**.
2. Set **Period 5 min**, **Grace 10 min** (alert after ~15 min of silence).
3. Integrations: connect **Discord** and **Email**.
4. Copy the ping URL (`https://hc-ping.com/<uuid>`) — treat it as a secret.

## 3. Build (on the Mac)

```sh
./deploy/build.sh          # → dist/assistant-bot (linux/arm64)
```

## 4. Copy to the VM

Port 22 is blocked on the company network — do this **from home**, or via OCI
Cloud Shell (Console → Cloud Shell, upload the SSH key with the ⋮ menu, then):

```sh
scp dist/assistant-bot deploy/assistant-bot.service deploy/install-on-vm.sh \
    ubuntu@<VM_IP>:
```

## 5. On the VM

```sh
ssh ubuntu@<VM_IP>

# Create the env file (secrets — never commit it):
mkdir -p ~/.config/assistant-bot && chmod 700 ~/.config/assistant-bot
cat > ~/.config/assistant-bot/env <<'EOF'
DISCORD_TOKEN=...
DISCORD_GUILD_ID=...
DISCORD_CHANNEL_ID=...
OWNER_USER_ID=...
HEALTHCHECK_URL=https://hc-ping.com/...
EOF
chmod 600 ~/.config/assistant-bot/env

# Install binary + systemd unit, enable + start:
./install-on-vm.sh
```

## 6. Verify

- `journalctl -u assistant-bot -f` — expect a ready log, then a tick every
  5 min ending with the healthcheck ping.
- In Discord `#server`: one 🤖 "assistant-bot is up" summary on first start.
- Type `/status` — an ephemeral snapshot only you can see.
- healthchecks.io: the check shows pings every 5 min.
- Try `/status` from another account → ephemeral "Not allowed." and a
  `slash command denied` line in journald.

## 7. Post-deploy review (from the design's checklist)

- **No OCI write calls**: `sudo journalctl -u assistant-bot | grep -i oci`
  only ever shows list calls; the code has none — this is a code guarantee.
- **Token never logged**: grep journald for the token → nothing.
- **Anti-idle false alerts**: leave the bot running across at least one
  hourly stress burst (~1 h). Expected: **no** disk/mem/CPU alerts; mem
  dips to ~75% and CPU load to ~0.7, both under the thresholds
  (10% avail / 0.9 per core).

## 8. Rollback / stop

```sh
sudo systemctl disable --now assistant-bot
sudo rm /usr/local/bin/assistant-bot /etc/systemd/system/assistant-bot.service
```

The state file stays at `~/.local/state/assistant-bot/state.json`; removing it
makes the next start post a fresh "bot started" baseline summary.

## Notes

- The Mac-side watcher (`local.oci-micro-watch`) keeps running until the bot
  has proven itself (design non-goal: keep both for now).
- Restarting the bot is silent: no repeated alerts, no second summary — the
  state file carries the last snapshot and check states.
- When the hunter succeeds: expect 🎉 for each Micro, then one 🎯
  "hunter finished". `grab-servers` has then exited on purpose; the bot
  reminds you to restart it by hand if a Micro ever disappears.

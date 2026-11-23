# assistant-bot

A self-hosted personal assistant that lives on Discord: one small Go process
running 24/7 on an Oracle Cloud ARM VM, structured as isolated modules that
each do one job — and never spam.

## What it does

| Module | Cadence | Job |
|---|---|---|
| **ociwatch** | every 5 min | Watches an Oracle Cloud free-tier tenancy: instance lifecycle changes, host health (disk / RAM / CPU), a systemd "capacity hunter" service, and answers a `/status` slash command from its latest snapshot |
| **jobwatch** | every 2 h + 08:00 digest | Tracks the Vietnamese IT job market through the [goodjobs.io.vn](https://goodjobs.io.vn) aggregator API: dedups postings across 13+ boards, classifies Backend / AI / Data with finance-sector jobs surfaced first, archives every match to JSONL, and posts one daily digest |

One contract governs both: **alert exactly once on a state change, stay
quiet otherwise.** Every check is an OK→PROBLEM→OK state machine; events
are one-shot; a restart replays nothing thanks to atomic state files.

## Why it exists

1. **OCI free-tier hunting.** A "hunter" script on the VM tries to grab
   scarce Micro instances whenever capacity appears. The previous
   watchdog was a macOS popup that only worked while the Mac was awake;
   the bot watches the tenancy, the hunter and the host itself, and
   reports the moment anything changes.
2. **Job-market signal.** A daily, deduplicated pulse of the VN IT job
   market — backend / AI / data roles, finance and fintech companies
   first — with every matched posting archived locally as JSONL, ready
   for later analysis. A digest that always arrives (even with "0 new")
   doubles as the module's alive-signal.

## Architecture

```
                Oracle Cloud VM (ARM · systemd · ubuntu)
┌───────────────────────────────────────────────────┐
│ assistant-bot                                     │
│                                                   │
│  core: config · Discord gateway · owner check ·   │
│        per-module notifiers · atomic state file · │
│        healthcheck pinger                         │
│                                                   │
│  ociwatch (5 min)          jobwatch (2 h)         │
│   ├ OCI ListInstances        ├ goodjobs.io.vn API │
│   ├ /proc + statfs           ├ match: Backend /   │
│   ├ systemd + log age        │  AI / Data / finance│
│   └ /status                  ├ dedup: link + role │
│                              ├ jobs.jsonl archive │
│                              └ 08:00 ICT digest  │
└──────────────┬────────────────────┬──────────────┘
               │ outbound HTTPS only│
               ▼                    ▼
       Discord #server         Discord #jobs
       + healthchecks.io
       dead-man's switch
```

Design properties:

- **Module isolation** — each module is a goroutine behind a `recover`; a
  panic in one module cannot kill the process, and it is reported to that
  module's channel. New features are new modules; old ones are untouched.
- **No spam, no replay** — state machines fire exactly one message per
  transition; snapshots persist across restarts (temp file + rename), so
  the bot never repeats an old alert or digests the same posting twice.
- **Silence is a signal** — the bot pings healthchecks.io every 5 min; if
  the VM, the process or the network dies, the *absence* of pings alerts
  the owner within ~15 minutes. A bot that cannot report its own death
  has something else report it.
- **Least privilege** — OCI auth uses an instance principal with a
  read-only policy: no keys on the VM, no write calls in the code. The
  bot opens outbound connections only.
- **Polite to third parties** — one GET to a free aggregator API every
  2 h, an honest User-Agent, no in-tick retries; and behind a
  `JobSource` interface, so the day that service disappears it is a
  one-file swap, not a rewrite.

## Engineering notes

- Decision logic — checks, thresholds, dedup, digest rendering — is
  **pure and unit-tested** (52 test functions, with fakes for OCI,
  Discord, the host and the job API); the real network appears only in a
  manual smoke test.
- A posting counts as new by **first sight**, not by its timestamp: some
  boards truncate `posted_ts` to the day. Dedup keys are the link
  (fragment stripped) plus a normalized `(company, title)` role key that
  collapses the same role reposted across boards.
- The hourly anti-idle stress job on the host VM (CPU ~70 % / 8 min, RAM
  6 GB / 10 min) sits under every alert threshold by design — covered by
  tests — so the monitor never cries wolf on its own machine.
- Cross-compiled on macOS (`GOOS=linux GOARCH=arm64`) and deployed as a
  systemd service; `deploy/` carries the unit file, an env template and a
  step-by-step guide.

## Design-first workflow

Both modules were specified before they were written; the approved specs
live in the repo, bilingual (EN/VI), each with goals, anti-goals, failure
modes and the rejected alternatives:

- [`oci-watch design`](docs/superpowers/specs/2026-11-23-oci-watch-design.md)
- [`jobwatch design`](docs/superpowers/specs/2026-11-23-jobwatch-design.md)

## Layout

```
cmd/assistant-bot/           wiring: config, Discord session, module goroutines
internal/core/               config, owner check, per-module notifiers, pinger,
                             atomic state file
internal/modules/ociwatch/    checks, state machines, /status, collectors
internal/modules/jobwatch/   JobSource (goodjobs client), match / dedup,
                             digest rendering, JSONL archive
deploy/                      systemd unit, env template, build + deploy scripts
docs/superpowers/specs/      approved design documents
```

## Running your own

Prerequisites: a Discord app invited to a server you own, any always-on
Linux box (nothing here is cloud-specific), and a free
[healthchecks.io](https://healthchecks.io) check for the dead-man's
switch.

```sh
git clone https://github.com/FluteInTheRain/assistant-bot
cd assistant-bot
go test ./...        # pure logic, no network needed
./deploy/build.sh    # cross-compile linux/arm64
```

Then copy `deploy/env.example` and follow
[`deploy/DEPLOY.md`](deploy/DEPLOY.md). Slash commands answer only the
configured owner; anyone else gets an ephemeral "Not allowed." and a log
line.

## Stack

Go 1.27 · discordgo · oci-go-sdk (instance principal) · systemd ·
healthchecks.io · goodjobs.io.vn public API · Oracle Cloud A1 (ARM)

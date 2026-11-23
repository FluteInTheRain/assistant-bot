# assistant-bot

Khang's personal Discord assistant. Two modules run in one process:

- **ociwatch** — on the OCI VM `side-projects`: it alerts when instances
  appear/disappear/change, when the VM is unhealthy (disk / RAM / CPU),
  when the `grab-servers` hunter dies or hangs, when the OCI API is
  unreachable, and answers `/status`.
- **jobwatch** — the VN IT job market via goodjobs.io.vn: it polls every
  2 h, dedups postings, archives matched Backend/AI/Data postings to
  `jobs.jsonl`, and posts one digest at 08:00 ICT into `#jobs` with
  finance/fintech companies first.

Approved designs:
[`oci-watch`](docs/superpowers/specs/2026-11-23-oci-watch-design.md) ·
[`jobwatch`](docs/superpowers/specs/2026-11-23-jobwatch-design.md)

## Layout

```
cmd/assistant-bot/              wiring: config, Discord, module goroutines
internal/core/                 config, Discord session, owner check, notifier,
                               healthcheck pinger, atomic state file
internal/modules/ociwatch/     checks, state machines, /status, collectors
internal/modules/jobwatch/     JobSource (goodjobs), match/dedup, digest,
                               jobs.jsonl archive
deploy/                        systemd unit, env.example, build + install scripts,
                               step-by-step DEPLOY.md
```

Each module is a goroutine behind a `recover`; a panic is reported to that
module's own channel, and the dead-man's switch (healthchecks.io) flags
process-wide silence.

## Guarantees (from the designs' anti-goals)

- Read-only on OCI (a single `ListInstances` per 5 min, instance principal —
  no keys on the VM, no writes ever).
- Polite to goodjobs.io.vn: one `GET /recent-jobs` per 2 h, no in-tick
  retries, an honest User-Agent; a third-party service is treated as
  replaceable behind the `JobSource` interface.
- No spam: every check is an OK→PROBLEM→OK state machine; each transition
  sends exactly one message; the job digest is deduped by link and by
  (company, title); a restart replays nothing (state files).
- The hourly anti-idle stress job (CPU ~70% / 8 min, RAM 6 GB / 10 min)
  stays under every threshold — covered by unit tests.
- The Discord token and ping URL are never logged or committed.

## Development

```sh
go test ./...              # pure logic, fakes for OCI/Discord/host/goodjobs
./deploy/build.sh          # cross-compile linux/arm64 for the VM
```

Deploy: see [`deploy/DEPLOY.md`](deploy/DEPLOY.md).

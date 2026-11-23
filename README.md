# assistant-bot

Khang's personal Discord assistant. v1 runs one module — **ociwatch** — on
the OCI VM `side-projects`: it alerts when instances appear/disappear/change,
when the VM is unhealthy (disk / RAM / CPU), when the `grab-servers` hunter
dies or hangs, or when the OCI API is unreachable, and answers `/status`.

Approved design: [`docs/superpowers/specs/2026-11-23-oci-watch-design.md`](docs/superpowers/specs/2026-11-23-oci-watch-design.md)

## Layout

```
cmd/assistant-bot/          wiring: config, Discord, module goroutines
internal/core/              config, Discord session, owner check, notifier,
                            healthcheck pinger, atomic state file
internal/modules/ociwatch/  checks, state machines, /status, collectors
deploy/                     systemd unit, env.example, build + install scripts,
                            step-by-step DEPLOY.md
```

Each module is a goroutine behind a `recover`; a panic is reported to Discord
and the dead-man's switch (healthchecks.io) flags the silence.

## Guarantees (from the design's anti-goals)

- Read-only on OCI (a single `ListInstances` per 5 min, instance principal —
  no keys on the VM, no writes ever).
- No spam: every check is an OK→PROBLEM→OK state machine; each transition
  sends exactly one message; instance changes are one-shot events; a restart
  replays nothing (state file).
- The hourly anti-idle stress job (CPU ~70% / 8 min, RAM 6 GB / 10 min)
  stays under every threshold — covered by unit tests.
- The Discord token and ping URL are never logged or committed.

## Development

```sh
go test ./...              # pure logic, fakes for OCI/Discord/host
./deploy/build.sh          # cross-compile linux/arm64 for the VM
```

Deploy: see [`deploy/DEPLOY.md`](deploy/DEPLOY.md).

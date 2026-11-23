#!/usr/bin/env bash
# Build the linux/arm64 binary for the side-projects VM (design §3).
# Run this on the Mac: ./deploy/build.sh
set -euo pipefail
cd "$(dirname "$0")/.."

mkdir -p dist
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 \
  go build -trimpath -ldflags="-s -w" -o dist/assistant-bot ./cmd/assistant-bot

echo "built dist/assistant-bot (linux/arm64) — deploy with deploy/DEPLOY.md"

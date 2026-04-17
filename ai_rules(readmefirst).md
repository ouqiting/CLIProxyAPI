# AGENTS.md

This file defines the required workflow for AI when working in this repository.

## Required build

When Codex changes code in this repository, it must complete the following steps before finishing:

 Run `$env:CGO_ENABLED="0"; $env:GOOS="linux"; $env:GOARCH="amd64"; go build -o cli-proxy-api ./cmd/server/main.go` from the repository root.

## Post-build reminder (do not execute automatically)

After task completion and successful compilation, only remind the user to run:
1. `pkill -9 cli-proxy-api` — stop old service
2. `chmod +x cli-proxy-api` — grant execute permission
3. `systemctl restart cliproxyapi.service && journalctl -u cliproxyapi.service -f` — restart and tail logs

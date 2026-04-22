# Agent Instructions

## Commands

```bash
go test ./...                 # run all tests
golangci-lint run             # lint (CI uses golangci-lint-action)
mockery                       # regenerate mocks (committed as mock_UserliService.go)
```

CI runs: lint → test (with coverage) → goreleaser snapshot build. No required local order.

## Architecture

Single-package `main` app — all `.go` files live in the repo root, no `cmd/` or `internal/`.

Key abstraction: `ConnectionHandler` interface in `tcpserver.go` — both `LookupServer` and `PolicyServer` implement it; `StartTCPServer()` provides shared connection pooling, graceful shutdown, and metrics.

## Design Constraints

- **Fail-open**: API errors must return `DUNNO`/allow and never block mail delivery
- **No PII in metrics**: aggregate counters only, no email addresses in Prometheus labels
- Never store `context.Context` in structs — pass through function parameters

## Protocols

Socketmap (lookup): netstring-encoded `<len>:<mapname> <key>,` → `<len>:<status> <data>,`
Responses: `OK <data>`, `NOTFOUND`, `TEMP <msg>`, `PERM <msg>`

Policy delegation: `name=value\n` pairs, empty line terminates → `action=ACTION\n\n`
Only process at `protocol_state=END-OF-MESSAGE` for accurate counting.

## Pitfalls

- `USERLI_TOKEN` is required — app exits immediately if missing
- Rate limiter cleanup runs every 5 minutes in a background goroutine
- Socketmap names must match exactly: `alias`, `domain`, `mailbox`, `senders`

## Docker Development

```bash
cp .env.example .env
docker compose up
docker compose exec userli bin/console doctrine:schema:create
docker compose exec userli bin/console doctrine:fixtures:load --no-debug
```

Fixtures must be loaded manually after first start.

## Commits

- **Gitmoji** style: e.g., `:sparkles: Add feature`, `:bug: Fix rate limiter`, `:recycle: Refactor server`
- Add `Co-Authored-By: OpenCode <noreply@opencode.ai>` to the commit message

## Pull Requests

- Open as **draft**
- Write description in English
- Label PRs for release-drafter:
  - `feature` (major), `enhancement` (minor)
  - `fix` / `bugfix` / `bug` (patch)
  - `chore` / `dependencies` (patch, maintenance)

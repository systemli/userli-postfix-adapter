# Copilot Instructions for userli-postfix-adapter

## Project Overview

Postfix adapter for [Userli](https://github.com/systemli/userli) email management. Provides two TCP servers:

- **Lookup Server** (`:10001`): Lookups for aliases, domains, mailboxes, senders
- **Policy Server** (`:10003`): Rate limiting via Postfix SMTP Access Policy Delegation

## Architecture

```
┌─────────┐     ┌──────────────────┐     ┌────────────┐
│ Postfix │────▶│ tcpserver.go     │────▶│ Userli API │
└─────────┘     │ (shared infra)   │     └────────────┘
                ├──────────────────┤
                │ lookup.go        │  ← ConnectionHandler interface
                │ policy.go        │  ← ConnectionHandler interface
                └──────────────────┘
```

### Key Pattern: ConnectionHandler Interface

Both servers implement `ConnectionHandler` from `tcpserver.go`:

```go
type ConnectionHandler interface {
    HandleConnection(ctx context.Context, conn net.Conn)
}
```

`StartTCPServer()` provides shared infrastructure: connection pooling (semaphore), graceful shutdown, TCP keep-alive, metrics hooks.

### File Structure

| File            | Purpose                                                              |
| --------------- | -------------------------------------------------------------------- |
| `tcpserver.go`  | Shared TCP server with connection pooling, graceful shutdown         |
| `lookup.go`     | Socketmap protocol + `LookupServer` (implements `ConnectionHandler`) |
| `policy.go`     | Policy protocol + `PolicyServer` + rate limit logic                  |
| `ratelimit.go`  | Sliding window rate limiter (in-memory, per-sender)                  |
| `userli.go`     | HTTP client for Userli API with Bearer auth                          |
| `prometheus.go` | Metrics server + all metric definitions                              |
| `config.go`     | Environment variable configuration                                   |

## Development

```bash
cp .env.dist .env  # Set USERLI_TOKEN
docker-compose up  # Full stack: adapter + postfix + userli + mariadb + mailcatcher

# Test lookup (via socketmap protocol)
docker-compose exec postfix postmap -q "example.org" socketmap:inet:adapter:10001:domain

# Test policy (sends raw policy request)
echo -e "request=smtpd_access_policy\nprotocol_state=END-OF-MESSAGE\nsender=test@example.org\n\n" | nc localhost 10003
```

## Code Conventions

### Context Propagation

- Never store `context.Context` in structs - pass through function parameters
- Use parent context for timeouts: `ctx, cancel := context.WithTimeout(parentCtx, 5*time.Second)`

### Error Handling

- **Socketmap responses**: `OK <data>`, `NOTFOUND`, `TEMP <msg>`, `PERM <msg>`
- **Policy responses**: `action=DUNNO\n\n` (allow) or `action=REJECT <msg>\n\n`
- **Fail-open**: API errors return DUNNO/allow, never block mail on failures

### Metrics (prometheus.go)

- No PII in labels - aggregate counters only, no email addresses
- Metrics defined as package-level vars, registered in `StartMetricsServer()`

### Testing

- Mocks generated via mockery (`.mockery.yml`) - regenerate with `mockery`
- Use `context.Background()` in tests for handlers

## Protocols

### Socketmap (RFC-like netstring)

```
Request:  <len>:<mapname> <key>,    e.g., "18:domain example.org,"
Response: <len>:<status> <data>,    e.g., "4:OK 1,"
```

### Policy Delegation (Postfix SMTPD)

```
Request:  name=value\n pairs, empty line terminates
Response: action=ACTION\n\n
```

Only process at `protocol_state=END-OF-MESSAGE` for accurate counting.

## Common Pitfalls

- `USERLI_TOKEN` is required - app exits immediately if missing
- Rate limiter cleanup runs every 5 minutes in background goroutine
- Map names must match exactly: `alias`, `domain`, `mailbox`, `senders`

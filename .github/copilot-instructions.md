# Copilot Instructions for userli-postfix-adapter

## Project Overview

This is a Postfix socketmap adapter for the [Userli](https://github.com/systemli/userli) email user management system. It provides TCP-based lookup services for Postfix to query virtual aliases, domains, mailboxes, and sender login maps from Userli's REST API.

## Architecture

### Core Components

- **Socketmap Server** (`server.go`): TCP server implementing Postfix's socketmap protocol on port 10001
- **Policy Server** (`policy.go`): TCP server implementing Postfix SMTP Access Policy Delegation for rate limiting on port 10003
- **Rate Limiter** (`ratelimit.go`): In-memory rate limiting with sliding window for per-hour and per-day quotas
- **Userli Client** (`userli.go`): REST API client for querying Userli backend
- **Metrics Server** (`prometheus.go`): Prometheus metrics endpoint on port 10002
- **Adapter Logic** (`adapter.go`): Request routing and response formatting for four map types (alias, domain, mailbox, senders)

### Request Flow

1. Postfix sends socketmap query via TCP (format: `<netstring>MAP_NAME <SP> KEY`)
2. Socketmap server parses request and routes to appropriate handler in adapter
3. Adapter queries Userli REST API (`/api/postfix/{map_type}?query={key}`)
4. Response converted back to socketmap netstring format
5. Metrics updated for observability

## Development Workflow

### Local Setup

```bash
# Copy environment template
cp .env.dist .env

# Edit .env and set USERLI_TOKEN (required)
# Token can be created in Userli: Settings -> Api Tokens

# Start full stack (adapter + postfix + userli + mariadb + mailcatcher)
docker-compose up

# Adapter runs on :10001 (socketmap), :10002 (metrics), and :10003 (policy)
```

### Testing Postfix Integration

```bash
# Test socketmap queries directly
echo -e "10:alias test" | nc localhost 10001

# Test via Postfix container
docker-compose exec postfix postmap -q "user@example.org" socketmap:inet:adapter:10001:alias
docker-compose exec postfix postmap -q "example.org" socketmap:inet:adapter:10001:domain
docker-compose exec postfix postmap -q "user@example.org" socketmap:inet:adapter:10001:mailbox
docker-compose exec postfix postmap -q "user@example.org" socketmap:inet:adapter:10001:senders

# View caught test emails
open http://localhost:1080  # Mailcatcher web UI
```

### Building & Testing

```bash
# Run tests with coverage
go test ./...

# Build binary
go build -o userli-postfix-adapter

# Build Docker image
docker build -t systemli/userli-postfix-adapter .
```

## Code Conventions

### Configuration Pattern

- Use environment variables exclusively (no config files)
- **Required**: `USERLI_TOKEN` - application will fatal if missing
- Defaults defined in `config.go:NewConfig()`:
  - `USERLI_BASE_URL`: `http://localhost:8000`
  - `SOCKETMAP_LISTEN_ADDR`: `:10001`
  - `POLICY_LISTEN_ADDR`: `:10003`
  - `METRICS_LISTEN_ADDR`: `:10002`
  - `LOG_LEVEL`: `info`
  - `LOG_FORMAT`: `text` (or `json`)

### Error Handling

- Use `logrus` for structured logging: `log.WithField("key", value).Error()`
- Socketmap protocol requires specific error responses:
  - `"TEMP "` - temporary failure (HTTP errors, network issues)
  - `"PERM "` - permanent failure (404 not found)
  - `"NOTFOUND "` - valid query but no result
  - `"OK <value>"` - successful lookup
- Fatal errors only for startup issues (missing token, port bind failures)
- Network/API errors are logged but return TEMP to Postfix for retry

### Adapter Response Pattern

The adapter in `adapter.go` follows this flow:

```go
// 1. Parse socketmap request (map name and key)
// 2. Query Userli API: GET /api/postfix/{mapName}?query={key}
// 3. Parse JSON response structure: {"exists": bool, "result": string}
// 4. Return formatted response: "OK result" or "NOTFOUND "
```

### Socketmap Protocol Implementation

- Request format: `<length>:<data>,` (netstring format)
- Data format: `<mapName> <key>`
- Responses must end with space and newline per Postfix spec
- See `server.go:handleConnection()` for full protocol details

### Testing with Mocks

- Mock interfaces generated with `mockery` (see `mock_UserliService.go`)
- Test files follow `*_test.go` naming convention
- Use table-driven tests for multiple scenarios

## Key Files

- `main.go` - Entry point, initializes config and starts servers
- `server.go` - TCP server and socketmap protocol implementation
- `policy.go` - Policy server for rate limiting (Postfix SMTP Access Policy Delegation)
- `ratelimit.go` - In-memory rate limiter with sliding window
- `adapter.go` - Request routing and Userli API interaction
- `userli.go` - HTTP client with Bearer token authentication
- `config.go` - Environment-based configuration
- `prometheus.go` - Metrics instrumentation
- `docker-compose.yml` - Full test environment with Postfix, Userli, MariaDB, and Mailcatcher

## External Dependencies

- **Userli API**: REST endpoints at `/api/postfix/{alias,domain,mailbox,senders,quota}/{key}`
  - Socketmap endpoints return JSON arrays or booleans
  - Quota endpoint returns: `{"per_hour": int, "per_day": int}` (0 = unlimited)
  - Requires Bearer token authentication
- **Postfix Configuration**: Uses `socketmap:inet:adapter:10001:{mapName}` in virtual\_\*\_maps directives
- **Policy Server**: Uses `check_policy_service inet:adapter:10003` in `smtpd_end_of_data_restrictions`
- **Prometheus**: Scrapes metrics from `:10002/metrics`

## Common Pitfalls

- Forgetting to set `USERLI_TOKEN` in `.env` causes immediate startup failure
- Map names in Postfix config must exactly match: `alias`, `domain`, `mailbox`, `senders`
- Netstring format is strict: must include length prefix and comma suffix
- Empty API responses (exists=false) should return "NOTFOUND ", not an error
- All socketmap responses must end with space + newline for Postfix compatibility

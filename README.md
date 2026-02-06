# userli-postfix-adapter

[![Integration](https://github.com/systemli/userli-postfix-adapter/actions/workflows/integration.yml/badge.svg)](https://github.com/systemli/userli-postfix-adapter/actions/workflows/integration.yml) [![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter) [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter) [![Coverage](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=coverage)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter)

This is a postfix socketmap adapter for the [userli](https://github.com/systemli/userli) project.
It implements the [socketmap protocol](https://www.postfix.org/socketmap_table.5.html) to provide dynamic lookups for aliases, domains, mailboxes, and senders.

## Configuration

The adapter is configured via environment variables:

- `USERLI_TOKEN`: The token to authenticate against the userli API.
- `USERLI_BASE_URL`: The base URL of the userli API.
- `POSTFIX_RECIPIENT_DELIMITER`: The recipient delimiter used in Postfix (e.g., `+`). Default: empty.
- `SOCKETMAP_LISTEN_ADDR`: The address to listen on for socketmap requests. Default: `:10001`.
- `POLICY_LISTEN_ADDR`: The address to listen on for policy requests (rate limiting). Default: `:10003`.
- `METRICS_LISTEN_ADDR`: The address to listen on for metrics. Default: `:10002`.

In Postfix, you can configure the adapter using the socketmap protocol like this:

```text
virtual_alias_maps = socketmap:inet:localhost:10001:alias
virtual_mailbox_domains = socketmap:inet:localhost:10001:domain
virtual_mailbox_maps = socketmap:inet:localhost:10001:mailbox
smtpd_sender_login_maps = socketmap:inet:localhost:10001:senders
```

### Rate Limiting (Policy Server)

The adapter also provides a Postfix SMTP Access Policy Delegation server for rate limiting outgoing mail.
It queries the Userli API for per-user quotas and enforces sending limits.

Configure in Postfix `main.cf`:

```text
smtpd_end_of_data_restrictions = check_policy_service inet:localhost:10003
```

The Userli API endpoint `/api/postfix/quota/{email}` returns:

```json
{
    "per_hour": 100,
    "per_day": 1000
}
```

Where `0` means unlimited. If the API is unreachable, messages are allowed (fail-open).

## Docker

You can run the adapter using Docker.
A `docker-compose.yml` file is provided for convenience.

```bash
docker compose up -d

# Create the database and load fixtures
docker compose exec userli bin/console doctrine:schema:create
docker compose exec userli bin/console doctrine:fixtures:load --no-debug

docker compose exec postfix postmap -q "example.org" socketmap:inet:adapter:10001:domain
```

The socketmap names supported are:

- `alias` - For virtual alias lookups
- `domain` - For virtual domain lookups
- `mailbox` - For virtual mailbox lookups
- `senders` - For sender login map lookups

## Usage Example

You can test the socketmap adapter using `postmap`:

```bash
# Test alias lookup
postmap -q "test@example.com" socketmap:inet:localhost:10001:alias

# Test domain lookup
postmap -q "example.com" socketmap:inet:localhost:10001:domain

# Test mailbox lookup
postmap -q "user@example.com" socketmap:inet:localhost:10001:mailbox

# Test sender lookup
postmap -q "sender@example.com" socketmap:inet:localhost:10001:senders
```

### Docker Usage

```bash
# Build and run with docker-compose
docker-compose up --build

# Or build manually
docker build -t userli-postfix-adapter .
docker run -e USERLI_TOKEN=your_token -e USERLI_BASE_URL=http://your-userli-instance -p 10001:10001 -p 10002:10002 userli-postfix-adapter
```

## Protocol Details

The adapter implements the Postfix socketmap protocol using netstrings for encoding. Each request and response is formatted as:

```text
[length]:[data],
```

Where:

- `length` is the decimal length of the data
- `:` is a delimiter
- `data` is the actual request or response content
- `,` is the terminating comma

### Request Format

```text
[length]:[mapname key],
```

Examples:

- `22:alias test@example.com,` - Look up alias for <test@example.com>
- `18:domain example.com,` - Check if domain example.com exists
- `23:mailbox user@example.com,` - Check if mailbox <user@example.com> exists
- `24:senders user@example.com,` - Get senders for <user@example.com>

### Response Format

The adapter returns one of these response types:

- `OK [data]` - Successful lookup with data
- `NOTFOUND` - No data found for the key
- `TEMP [reason]` - Temporary error (retry later)
- `PERM [reason]` - Permanent error (don't retry)

Examples:

- `19:OK dest@example.com,` - Alias found, destination is <dest@example.com>
- `4:OK 1,` - Domain/mailbox exists
- `8:NOTFOUND,` - No data found
- `20:TEMP Service error,` - Temporary service error

## Observability

The adapter exposes Prometheus metrics on `/metrics` (port 10002) and provides health check endpoints.

### Metrics

**Socketmap Metrics:**

- `userli_postfix_adapter_request_duration_seconds` - Request duration histogram
- `userli_postfix_adapter_requests_total` - Total request counter
- `userli_postfix_adapter_active_connections` - Active connections gauge
- `userli_postfix_adapter_connection_pool_usage` - Connection pool usage (0-500)

**HTTP Client Metrics:**

- `userli_postfix_adapter_http_client_duration_seconds` - Userli API request duration
- `userli_postfix_adapter_http_client_requests_total` - Userli API request counter

**Health:**

- `userli_postfix_adapter_health_check_status` - Health check status (1=healthy, 0=unhealthy)

**Policy/Rate Limiting Metrics:**

- `userli_postfix_adapter_policy_active_connections` - Active policy connections gauge
- `userli_postfix_adapter_policy_requests_total` - Total policy request counter
- `userli_postfix_adapter_policy_request_duration_seconds` - Policy request duration histogram
- `userli_postfix_adapter_quota_exceeded_total` - Total messages rejected due to quota
- `userli_postfix_adapter_quota_checks_total` - Total quota checks performed
- `userli_postfix_adapter_tracked_senders` - Number of senders tracked by rate limiter

All metrics include relevant labels (handler, status, endpoint, etc.).

### Health Endpoints

- **`/health`** - Liveness probe (always returns 200 OK)
- **`/ready`** - Readiness probe (checks Userli API connectivity)

Example Kubernetes configuration:

```yaml
livenessProbe:
  httpGet:
    path: /health
    port: 10002
readinessProbe:
  httpGet:
    path: /ready
    port: 10002
```

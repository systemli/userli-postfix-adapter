# userli-postfix-adapter

[![Integration](https://github.com/systemli/userli-postfix-adapter/actions/workflows/integration.yml/badge.svg)](https://github.com/systemli/userli-postfix-adapter/actions/workflows/integration.yml) [![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter) [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter) [![Coverage](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=coverage)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter)

This is a postfix socketmap adapter for the [userli](https://github.com/systemli/userli) project.
It implements the [socketmap protocol](https://www.postfix.org/socketmap_table.5.html) to provide dynamic lookups for aliases, domains, mailboxes, and senders.

## Configuration

The adapter is configured via environment variables:

- `USERLI_TOKEN`: The token to authenticate against the userli API.
- `USERLI_BASE_URL`: The base URL of the userli API.
- `SOCKETMAP_LISTEN_ADDR`: The address to listen on for socketmap requests. Default: `:10001`.
- `METRICS_LISTEN_ADDR`: The address to listen on for metrics. Default: `:10002`.

In Postfix, you can configure the adapter using the socketmap protocol like this:

```text
virtual_alias_maps = socketmap:inet:localhost:10001:alias
virtual_mailbox_domains = socketmap:inet:localhost:10001:domain
virtual_mailbox_maps = socketmap:inet:localhost:10001:mailbox
smtpd_sender_login_maps = socketmap:inet:localhost:10001:senders
```

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

```
[length]:[data],
```

Where:

- `length` is the decimal length of the data
- `:` is a delimiter
- `data` is the actual request or response content
- `,` is the terminating comma

### Request Format

```
[length]:[mapname key],
```

Examples:

- `22:alias test@example.com,` - Look up alias for test@example.com
- `18:domain example.com,` - Check if domain example.com exists
- `23:mailbox user@example.com,` - Check if mailbox user@example.com exists
- `24:senders user@example.com,` - Get senders for user@example.com

### Response Format

The adapter returns one of these response types:

- `OK [data]` - Successful lookup with data
- `NOTFOUND` - No data found for the key
- `TEMP [reason]` - Temporary error (retry later)
- `PERM [reason]` - Permanent error (don't retry)

Examples:

- `19:OK dest@example.com,` - Alias found, destination is dest@example.com
- `4:OK 1,` - Domain/mailbox exists
- `8:NOTFOUND,` - No data found
- `20:TEMP Service error,` - Temporary service error

## Metrics

The adapter exposes metrics in the Prometheus format. You can access them on the `/metrics` endpoint.

```text
# HELP userli_postfix_adapter_request_duration_seconds Duration of requests to userli
# TYPE userli_postfix_adapter_request_duration_seconds histogram
userli_postfix_adapter_request_duration_seconds_bucket{handler="alias",status="success",le="0.1"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="alias",status="success",le="0.15000000000000002"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="alias",status="success",le="0.22500000000000003"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="alias",status="success",le="0.3375"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="alias",status="success",le="0.5062500000000001"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="alias",status="success",le="+Inf"} 1
userli_postfix_adapter_request_duration_seconds_sum{handler="alias",status="success"} 0.074540625
userli_postfix_adapter_request_duration_seconds_count{handler="alias",status="success"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="domain",status="success",le="0.1"} 3
userli_postfix_adapter_request_duration_seconds_bucket{handler="domain",status="success",le="0.15000000000000002"} 3
userli_postfix_adapter_request_duration_seconds_bucket{handler="domain",status="success",le="0.22500000000000003"} 3
userli_postfix_adapter_request_duration_seconds_bucket{handler="domain",status="success",le="0.3375"} 3
userli_postfix_adapter_request_duration_seconds_bucket{handler="domain",status="success",le="0.5062500000000001"} 3
userli_postfix_adapter_request_duration_seconds_bucket{handler="domain",status="success",le="+Inf"} 3
userli_postfix_adapter_request_duration_seconds_sum{handler="domain",status="success"} 0.246158083
userli_postfix_adapter_request_duration_seconds_count{handler="domain",status="success"} 3
userli_postfix_adapter_request_duration_seconds_bucket{handler="mailbox",status="success",le="0.1"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="mailbox",status="success",le="0.15000000000000002"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="mailbox",status="success",le="0.22500000000000003"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="mailbox",status="success",le="0.3375"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="mailbox",status="success",le="0.5062500000000001"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="mailbox",status="success",le="+Inf"} 1
userli_postfix_adapter_request_duration_seconds_sum{handler="mailbox",status="success"} 0.097836333
userli_postfix_adapter_request_duration_seconds_count{handler="mailbox",status="success"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="senders",status="success",le="0.1"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="senders",status="success",le="0.15000000000000002"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="senders",status="success",le="0.22500000000000003"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="senders",status="success",le="0.3375"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="senders",status="success",le="0.5062500000000001"} 1
userli_postfix_adapter_request_duration_seconds_bucket{handler="senders",status="success",le="+Inf"} 1
userli_postfix_adapter_request_duration_seconds_sum{handler="senders",status="success"} 0.097870375
userli_postfix_adapter_request_duration_seconds_count{handler="senders",status="success"} 1
```

# userli-postfix-adapter

[![Integration](https://github.com/systemli/userli-postfix-adapter/actions/workflows/integration.yml/badge.svg)](https://github.com/systemli/userli-postfix-adapter/actions/workflows/integration.yml) [![Maintainability Rating](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=sqale_rating)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter) [![Quality Gate Status](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=alert_status)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter) [![Coverage](https://sonarcloud.io/api/project_badges/measure?project=systemli_userli-postfix-adapter&metric=coverage)](https://sonarcloud.io/summary/new_code?id=systemli_userli-postfix-adapter)

This is a postfix adapter for the [userli](https://github.com/systemli/userli) project.

## Configuration

The adapter is configured via environment variables:

- `USERLI_TOKEN`: The token to authenticate against the userli API.
- `USERLI_BASE_URL`: The base URL of the userli API.
- `ALIAS_LISTEN_ADDR`: The address to listen on for incoming requests. Default: `10001`.
- `DOMAIN_LISTEN_ADDR`: The address to listen on for incoming requests. Default: `10002`.
- `MAILBOX_LISTEN_ADDR`: The address to listen on for incoming requests. Default: `10003`.
- `SENDERS_LISTEN_ADDR`: The address to listen on for incoming requests. Default: `10004`.
- `METRICS_LISTEN_ADDR`: The address to listen on for metrics. Default: `10005`.

In Postfix, you can configure the adapter as a transport like this:

```text
virtual_alias_maps = tcp:localhost:10001
virtual_mailbox_domains = tcp:localhost:10002
virtual_mailbox_maps = tcp:localhost:10003
smtpd_sender_login_maps = tcp:localhost:10004
```

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

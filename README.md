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

In Postfix, you can configure the adapter as a transport like this:

```text
virtual_alias_maps = tcp:localhost:10001
virtual_mailbox_domains = tcp:localhost:10002
virtual_mailbox_maps = tcp:localhost:10003
smtpd_sender_login_maps = tcp:localhost:10004
```

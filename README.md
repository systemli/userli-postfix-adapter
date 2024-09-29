# userli-postfix-adapter

This is a postfix adapter for the [userli](https://github.com/systemli/userli) project.

## Configuration

The adapter is configured via environment variables:

- `USERLI_TOKEN`: The token to authenticate against the userli API.
- `USERLI_BASE_URL`: The base URL of the userli API.
- `ALIAS_LISTEN_ADDR`: The address to listen on for incoming requests. Default: `10001`.

In Postfix, you can configure the adapter as a transport like this:

```text
virtual_alias_maps = tcp:localhost:10001
```
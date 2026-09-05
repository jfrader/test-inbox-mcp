# Inbox Mail MCP

Inbox Mail MCP gives coding agents disposable email addresses and a small set of tools for testing signup confirmations, magic links, password resets, and one-time codes. It is a zero-dependency Go stdio MCP server backed by a self-hosted [Inbucket](https://www.inbucket.org/) instance.

The MCP process runs on the developer's machine. It creates addresses locally and uses Inbucket's REST API to list, retrieve, wait for, and delete test messages. Inbucket accepts SMTP mail, stores it for inspection, and never relays it onward.

## Why

Automated browser and integration tests often stop when a flow sends email. This server lets an agent create a unique address, trigger the flow, wait for the message, extract links or numeric codes, and clean up without access to a real inbox.

## Requirements

- Go 1.23 or newer to build the MCP server
- Docker with Compose to self-host Inbucket
- A host that can receive inbound TCP port 25 for internet-delivered email
- A domain or subdomain whose MX record points to that host

Many residential networks and cloud providers block port 25. Confirm inbound SMTP availability before changing DNS.

## Quickstart

### 1. Start Inbucket

```sh
docker compose up -d
```

The compose file publishes SMTP on host port 25 and binds the web UI and REST API only to `127.0.0.1:9000`. It uses memory storage with a 24-hour retention period and a 100-message cap per mailbox. Messages are lost when the container restarts.

Check the local API:

```sh
curl http://127.0.0.1:9000/status
```

### 2. Configure DNS

Create a host record for the mail receiver, then point the disposable-address domain's MX record at it. The MX target must be a hostname, not an IP address.

```text
mail.inbox.example.com.  A   203.0.113.10
inbox.example.com.       MX  10 mail.inbox.example.com.
```

This configuration receives addresses ending in `@inbox.example.com`. Allow inbound TCP port 25 to the Docker host. Inbucket is receive-only: it stores accepted messages and does not relay mail.

### 3. Build and configure the MCP server

```sh
make build
export INBOX_BASE_URL=http://127.0.0.1:9000
export INBOX_DOMAIN=inbox.example.com
./bin/agent-test-inbox-mcp
```

The process waits for newline-delimited MCP JSON-RPC on standard input. MCP clients normally start it for you.

Configuration:

| Variable | Default | Purpose |
| --- | --- | --- |
| `INBOX_BASE_URL` | `http://127.0.0.1:9000` | Inbucket base URL, directly or through a reverse proxy |
| `INBOX_DOMAIN` | `inbox.example.com` | Domain appended to generated mailbox local parts |
| `INBOX_BASIC_USER` | empty | Optional HTTP basic-auth username |
| `INBOX_BASIC_PASS` | empty | Optional HTTP basic-auth password |

Set both basic-auth variables or neither. Keep credentials in environment variables or a secret manager, not in a committed config file.

## MCP client configuration

Replace `/absolute/path/to/agent-test-inbox-mcp` with the built binary path. Add basic-auth variables only when the API is behind basic auth.

### OpenCode

Add this to `opencode.json` or `opencode.jsonc`:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "inbox-mail": {
      "type": "local",
      "command": ["/absolute/path/to/agent-test-inbox-mcp"],
      "enabled": true,
      "environment": {
        "INBOX_BASE_URL": "https://inbox.example.com",
        "INBOX_DOMAIN": "inbox.example.com",
        "INBOX_BASIC_USER": "replace-me",
        "INBOX_BASIC_PASS": "replace-me"
      }
    }
  }
}
```

### Claude Desktop

Add this entry under `mcpServers` in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "inbox-mail": {
      "command": "/absolute/path/to/agent-test-inbox-mcp",
      "args": [],
      "env": {
        "INBOX_BASE_URL": "https://inbox.example.com",
        "INBOX_DOMAIN": "inbox.example.com",
        "INBOX_BASIC_USER": "replace-me",
        "INBOX_BASIC_PASS": "replace-me"
      }
    }
  }
}
```

Restart Claude Desktop after changing its configuration.

### Cursor

Create `.cursor/mcp.json` in a project or edit the global Cursor MCP configuration:

```json
{
  "mcpServers": {
    "inbox-mail": {
      "command": "/absolute/path/to/agent-test-inbox-mcp",
      "args": [],
      "env": {
        "INBOX_BASE_URL": "https://inbox.example.com",
        "INBOX_DOMAIN": "inbox.example.com",
        "INBOX_BASIC_USER": "replace-me",
        "INBOX_BASIC_PASS": "replace-me"
      }
    }
  }
}
```

## Tools

| Tool | Purpose | Required input |
| --- | --- | --- |
| `new_address` | Generate a unique address or use an explicit local part | None |
| `list_messages` | List newest messages with optional subject and sender filters | `mailbox` |
| `get_message` | Return headers, text, HTML, links, and 4-8 digit codes | `mailbox`, `id` |
| `wait_for_email` | Poll until a matching message is available | `mailbox` |
| `clear_inbox` | Delete all messages in a mailbox | `mailbox` |
| `delete_message` | Delete one message | `mailbox`, `id` |

A typical agent flow is `new_address`, trigger the application action, `wait_for_email`, follow the returned link or enter the code, then `clear_inbox`.

## Reverse proxy and basic auth

The compose API is loopback-only. For MCP clients on other machines, expose it through an HTTPS reverse proxy and require authentication. A minimal nginx location looks like:

```nginx
location / {
    auth_basic "Test inbox";
    auth_basic_user_file /etc/nginx/htpasswd/inbox;
    proxy_pass http://127.0.0.1:9000;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Generate the nginx password file with an Apache MD5 `apr1` hash:

```sh
htpasswd -c -m ./inbox.htpasswd inbox-user
```

Verify that the stored hash starts with `$apr1$`. Do not use bcrypt `$2y$` hashes for this deployment: nginx rejects them and every authenticated API request returns HTTP 403.

## Security

- Treat every mailbox as discoverable unless the API is protected. Mailbox names are identifiers, not passwords.
- Use HTTPS and basic auth when the API leaves localhost. Store the password outside source control.
- Expose only SMTP publicly. Keep the Inbucket web/API port on loopback and put a reverse proxy in front of it if remote access is needed.
- Inbucket accepts and stores mail but never relays it. It is not an outbound SMTP server or open relay.
- Test messages can contain login links, personal data, and one-time codes. Never use this service for production user mail.
- Keep retention short. The provided compose file uses volatile memory storage, removes messages after 24 hours, and caps each mailbox at 100 messages.
- Restrict accepted domains with Inbucket's `INBUCKET_SMTP_DEFAULTACCEPT=false` and `INBUCKET_SMTP_ACCEPTDOMAINS=your-domain.example` when operating a dedicated public receiver.
- Apply firewall rules and rate limiting appropriate for an internet-facing SMTP service.

## Verify

Run unit tests:

```sh
make test
```

Build a stripped static binary and run the offline stdio handshake smoke test:

```sh
make smoke
```

The smoke test sends `initialize`, `notifications/initialized`, `tools/list`, and `new_address` messages to the binary. It requires no Inbucket instance or network access.

For an end-to-end deployment check:

1. Run `docker compose up -d` and confirm `curl http://127.0.0.1:9000/status` succeeds.
2. Send a message through the local SMTP listener: `swaks --server 127.0.0.1 --port 25 --to check@inbox.example.com --from sender@example.net --header "Subject: Inbox check" --body "Code 123456"`.
3. Configure an MCP client and call `list_messages` with mailbox `check`.
4. Call `get_message` and confirm it returns the subject, body, and code `123456`.
5. After publishing DNS, repeat the test through an external mail provider to verify MX routing and inbound port 25.

## Development

```sh
make test
make smoke
```

The server intentionally uses only the Go standard library. `CONTRIBUTING.md` and `SECURITY.md` contain contribution and vulnerability-reporting guidance.

## License

MIT — see [LICENSE](LICENSE).

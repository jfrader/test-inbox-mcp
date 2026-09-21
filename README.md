# Test Inbox MCP

Test Inbox MCP gives coding agents disposable email addresses and tools for testing
signup confirmations, magic links, password resets, and one-time codes. It is a
zero-dependency Go stdio MCP server backed by a self-hosted
[Inbucket](https://www.inbucket.org/) instance.

The MCP process runs on the developer's machine: it creates addresses locally and uses
Inbucket's REST API to list, retrieve, wait for, and delete messages. Inbucket accepts
SMTP mail, stores it for inspection, and never relays it onward.

## Why

Browser and integration tests often stop where a flow sends email. This server lets an
agent create a unique address, trigger the flow, wait for the message, extract the link or
code, and clean up — without access to a real inbox.

## Requirements

- Go 1.23 or newer
- Docker with Compose, to self-host Inbucket
- A host that can receive inbound TCP port 25, and a domain whose MX record points to it

Many residential networks and cloud providers block port 25. Confirm inbound SMTP works
before changing DNS.

## Quickstart

### 1. Start Inbucket

```sh
docker compose up -d
curl http://127.0.0.1:9000/status
```

Compose publishes SMTP on host port 25 and binds the web UI and REST API to
`127.0.0.1:9000` only. Storage is in memory: 24-hour retention, a 100-message cap per
mailbox, and messages are lost when the container restarts.

### 2. Point DNS at the receiver

The MX target must be a hostname, not an IP address.

```text
mail.inbox.example.com.  A   203.0.113.10
inbox.example.com.       MX  10 mail.inbox.example.com.
```

Addresses then end in `@inbox.example.com`. Inbucket is receive-only: it stores accepted
messages and does not relay mail.

### 3. Build and run the server

```sh
make build
export INBOX_BASE_URL=http://127.0.0.1:9000
export INBOX_DOMAIN=inbox.example.com
./bin/test-inbox-mcp
```

The process reads newline-delimited MCP JSON-RPC on stdin. MCP clients normally start it
for you.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `INBOX_BASE_URL` | `http://127.0.0.1:9000` | Inbucket base URL, directly or behind a reverse proxy |
| `INBOX_DOMAIN` | `inbox.example.com` | Domain appended to generated mailbox local parts |
| `INBOX_BASIC_USER` | empty | Optional HTTP basic-auth username |
| `INBOX_BASIC_PASS` | empty | Optional HTTP basic-auth password |

Set both basic-auth variables or neither, and keep them out of source control.

## MCP client configuration

Every client takes the same command, arguments, and environment; only the surrounding key
differs.

```json
{
  "command": "/absolute/path/to/test-inbox-mcp",
  "args": [],
  "env": {
    "INBOX_BASE_URL": "https://inbox.example.com",
    "INBOX_DOMAIN": "inbox.example.com",
    "INBOX_BASIC_USER": "replace-me",
    "INBOX_BASIC_PASS": "replace-me"
  }
}
```

- **OpenCode** — an `mcp` entry in `opencode.jsonc` with `"type": "local"`, using
  `command` and `environment`.
- **Claude Desktop** — an `mcpServers` entry in `claude_desktop_config.json`; restart the
  app afterwards.
- **Cursor** — the same `mcpServers` shape in `.cursor/mcp.json` (per project) or the
  global Cursor MCP configuration.

## Tools

| Tool | Purpose | Required input |
| --- | --- | --- |
| `new_address` | Generate a unique address, or use an explicit local part | none |
| `list_messages` | List newest messages, optionally filtered by subject or sender | `mailbox` |
| `get_message` | Return headers, text, HTML, links, and 4–8 digit codes | `mailbox`, `id` |
| `wait_for_email` | Poll until a matching message arrives | `mailbox` |
| `clear_inbox` | Delete every message in a mailbox | `mailbox` |
| `delete_message` | Delete one message | `mailbox`, `id` |

A typical flow is `new_address`, trigger the application action, `wait_for_email`, follow
the returned link or enter the code, then `clear_inbox`.

## Security

Treat this as an internet-facing SMTP receiver holding other people's login links.

- Mailboxes are discoverable identifiers, not passwords. Anyone who can reach port 25 can
  send to any address on the domain, and message bodies are attacker-controlled text that
  ends up in agent context. Only follow links and codes that match the flow under test.
- Never use it for production user mail. Keep retention short; the provided compose file
  is volatile, removes messages after 24 hours, and caps each mailbox at 100 messages.
- Keep the Inbucket web and API port on loopback. To reach it from another machine, put an
  HTTPS reverse proxy with basic auth in front of it:

  ```nginx
  location / {
      auth_basic "Test inbox";
      auth_basic_user_file /etc/nginx/htpasswd/inbox;
      proxy_pass http://127.0.0.1:9000;
      proxy_set_header Host $host;
      proxy_set_header X-Forwarded-Proto $scheme;
  }
  ```

  Generate the password file with an Apache MD5 `apr1` hash, for example
  `htpasswd -c -m ./inbox.htpasswd inbox-user`. nginx rejects bcrypt `$2y$` hashes, and
  every authenticated request then returns HTTP 403.
- Basic-auth credentials travel in cleartext unless `INBOX_BASE_URL` is `https://`. Never
  point the server at a plain `http://` endpoint on a remote host.
- For a dedicated public receiver, restrict accepted domains with
  `INBUCKET_SMTP_DEFAULTACCEPT=false` and `INBUCKET_SMTP_ACCEPTDOMAINS=your-domain.example`,
  and add firewall rules and rate limiting. The open listener can be mail-bombed; with
  volatile storage that costs you test mail, not data.
- Inbucket accepts and stores mail but never relays it. It is not an outbound SMTP server
  or an open relay.

## Verify

```sh
make test     # unit tests
make smoke    # offline stdio handshake; no Inbucket or network access needed
```

For an end-to-end check:

1. Run `docker compose up -d` and confirm `curl http://127.0.0.1:9000/status` succeeds.
2. Deliver a message through the local listener:
   `swaks --server 127.0.0.1 --port 25 --to check@inbox.example.com --from sender@example.net --header "Subject: Inbox check" --body "Code 123456"`.
3. Call `list_messages` with mailbox `check`, then `get_message`, and confirm the subject,
   body, and code `123456`.
4. After DNS is live, repeat through an external mail provider to confirm MX routing and
   inbound port 25.

The server intentionally uses only the Go standard library. See `CONTRIBUTING.md` and
`SECURITY.md` for contribution and vulnerability-reporting guidance.

## License

MIT — see [LICENSE](LICENSE).

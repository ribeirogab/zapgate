# zapgate

WhatsApp for AI agents, with a human in the loop.

![An AI agent asks zapgate to send a WhatsApp message. The owner approves it, and the message arrives on the phone.](docs/assets/demo.svg)

zapgate is a self-hosted [MCP](https://modelcontextprotocol.io) server that connects your AI agents to your WhatsApp accounts. Agents read chats, search messages and download media. With your approval, they also send, react, edit and delete.

zapgate connects to WhatsApp as a linked device of each number, like WhatsApp Web, through the [whatsmeow](https://github.com/tulir/whatsmeow) library.

> [!WARNING]
> zapgate is not affiliated with WhatsApp or Meta. It is an unofficial client, and WhatsApp can ban a number that uses an unofficial client. Send at a human pace. Do not use zapgate for bulk messages or for messages that no person approves.

## Why zapgate

- **Many accounts, one server.** Pair a personal number, a work number, or as many as you need. Every tool call names its account, and no tool has a default account.
- **Your approval on every write.** In clients with MCP elicitation (Claude Code, Codex, Cursor), every send, reaction, edit and delete opens a question with the exact account, chat and content. Nothing goes out until you accept.
- **One access per client.** Each client gets its own access, with only the accounts you choose. Bots get a read-only access.
- **Remote clients work too.** OAuth with dynamic client registration lets claude.ai, ChatGPT and Claude on mobile connect as a custom connector. Static bearer tokens serve bots and CLIs.
- **Built against prompt injection.** Message text arrives in a `third_party` field, and the server tells the agent that this field is data, not instructions.
- **Your history, on your disk.** Full-text search, media downloaded on arrival, retention per account, and a JSON audit log.
- **One static binary.** Pure Go, no cgo, and a hardened container image for amd64 and arm64.

## Quick start

You need Docker and a phone with WhatsApp.

```bash
git clone https://github.com/ribeirogab/zapgate.git
```

```bash
cd zapgate/deploy && ./init-env.sh && docker compose up -d --build
```

The `init-env.sh` script writes `deploy/.env` with a random owner password and signing key. The server listens on `http://localhost:8080`, on this computer only.

Pair a number. The account name is yours to choose:

```bash
docker compose exec zapgate zapgate accounts add personal --phone +5511988887777
```

The command shows an 8-character code. On the phone, open WhatsApp > Settings > Linked devices > Link a device > Link with phone number instead, and type the code.

Connect Claude Code:

```bash
claude mcp add --transport http --scope user whatsapp http://localhost:8080/mcp
```

In Claude Code, run `/mcp`, select `whatsapp` and authenticate. The browser opens the zapgate login page. Type the owner password, give the access a name, select the accounts and click **Authorize**. To read the owner password:

```bash
docker compose exec zapgate zapgate password
```

To use zapgate from claude.ai, ChatGPT or your phone, the server needs a public HTTPS address. See [Deploy](docs/deploy.md).

## Tools

| Tool | Kind | What it does |
| --- | --- | --- |
| `list_accounts` | read | Lists the accounts of the access, with their state |
| `list_chats` | read | Lists the chats of an account, with search and an unread filter |
| `get_messages` | read | Reads a part of a chat |
| `search_messages` | read | Searches text in an account |
| `get_chat` | read | Shows a chat and the members of a group |
| `search_contacts` | read | Searches contacts by name or number |
| `download_media` | read | Returns an image inline and a signed link for any media |
| `request_older_history` | read | Asks the phone for older messages of a chat |
| `send_message` | write | Sends text, with an optional quoted reply |
| `send_media` | write | Sends an image, video, audio, voice note or document |
| `react` | write | Adds or removes a reaction |
| `edit_message` | write | Edits a text that the account sent |
| `delete_message` | write | Deletes for everyone a message that the account sent |
| `mark_read` | mark_read | Marks received messages as read |

Every write needs your approval. See [Tools](docs/tools.md) for the inputs, the file uploads and the limits.

## Documentation

- [Deploy](docs/deploy.md): local mode, always-on mode, configuration, updates and backups
- [Accounts](docs/accounts.md): pairing, states, a logged-out account and retention
- [Access and approval](docs/access.md): OAuth, static tokens, read-only access and approval modes
- [Clients](docs/clients.md): Claude Code, Claude Desktop, claude.ai, ChatGPT, Codex, Cursor and bots
- [Tools](docs/tools.md): tools, file uploads and limits
- [Security](docs/security.md): the risks and how zapgate handles them
- [Glossary](GLOSSARY.md) and [decisions](docs/adr/)

## License

[MIT](LICENSE). WhatsApp is a trademark of WhatsApp LLC. zapgate is not affiliated with, endorsed by or sponsored by WhatsApp LLC or Meta Platforms, Inc.

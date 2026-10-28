# Clients

The MCP endpoint is `<ZAPGATE_PUBLIC_URL>/mcp`, with the Streamable HTTP transport. In local mode, it is `http://localhost:8080/mcp`. The examples below use `https://wa.example.com/mcp`: put your address in its place.

Each client logs in with OAuth or sends a static token. See [Access and approval](access.md).

## Claude Code

With OAuth:

```bash
claude mcp add --transport http --scope user whatsapp https://wa.example.com/mcp
```

In Claude Code, run `/mcp`, select `whatsapp` and authenticate. The browser opens the zapgate login page. Claude Code has elicitation, so each write opens the approval question in the terminal.

With a static token, for example on a machine without a browser:

```bash
claude mcp add --transport http --scope user whatsapp https://wa.example.com/mcp --header "Authorization: Bearer <token>"
```

## Claude Desktop

Custom connectors in Claude Desktop connect from Anthropic's cloud, like claude.ai. They need the always-on mode: add the connector as in [claude.ai](#claudeai), and it shows in Claude Desktop too.

For a local server, use a stdio bridge such as [mcp-remote](https://www.npmjs.com/package/mcp-remote) in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "whatsapp": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "http://localhost:8080/mcp"]
    }
  }
}
```

## claude.ai

1. In Settings > Connectors, add a custom connector.
2. Give it a name, for example `WhatsApp`, and paste the URL `https://wa.example.com/mcp`.
3. Click **Connect** and log in on the zapgate page.

claude.ai has no confirmed elicitation. The approval is claude.ai's own tool permission. Keep the write tools on "ask every time".

## ChatGPT

1. In the ChatGPT settings, under Apps and connectors, turn on developer mode.
2. Create a connector with the URL `https://wa.example.com/mcp` and OAuth authentication.
3. Log in on the zapgate page.

ChatGPT has no confirmed elicitation. The approval is the confirmation that ChatGPT asks before a write tool.

## Codex and Cursor

Codex and Cursor have elicitation and connect like Claude Code: with OAuth or with a static token in the `Authorization` header.

Cursor as a cloud agent says that it has elicitation, but it has no screen. It declines every approval question by itself, so you cannot write through it. Give the access of Cursor the approval mode `app`:

```bash
zapgate access set cursor --approval app
```

Then the approval is Cursor's own tool permission.

## Bots

Any MCP client with Streamable HTTP works, for example the official MCP SDK of each language. Create a static token, read-only when the bot acts alone, and send the header `Authorization: Bearer <token>` in every request. To check the token:

```bash
curl -sS https://wa.example.com/mcp -H "Authorization: Bearer $ZAPGATE_TOKEN" -H 'Content-Type: application/json' -H 'Accept: application/json, text/event-stream' -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"check","version":"1"}}}'
```

With a valid token, the response has the `serverInfo`. Without a token, or with an invalid token, zapgate answers 401.

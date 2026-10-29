# Access and approval

An access is the permission of one client: an OAuth login or a static token. Each access has a unique name, the accounts it reaches, a mode (read-write or read-only) and an approval mode. The name has lowercase letters, digits and hyphens.

A client reaches only the accounts of its access. Every tool needs the account name, and a call to an account outside the access fails.

The commands below are [owner commands](deploy.md#owner-commands).

## OAuth login

claude.ai, ChatGPT, Claude Code and other clients with a browser log in with OAuth. The client opens the zapgate authorization page. On the page:

1. Type the owner password in **Owner password**.
2. Give the access a name in **Access name**.
3. Select the accounts of the access in **Accounts**.
4. Click **Authorize**.

To read the owner password:

```bash
zapgate password
```

zapgate accepts 5 wrong passwords per IP address every 15 minutes. The client gets an access token for 1 hour and a refresh token for 90 days, and it renews both by itself. Each renewal replaces the refresh token. A client that does not use zapgate for 90 days logs in again. zapgate deletes the registration of a client that does not finish a login in 24 hours. When the page says `This app is not registered on this server.`, remove the connection in the client and connect again.

When the page says `This address is local`, the client that asked runs on the same computer, like Claude Code. Authorize only when you started the connection yourself.

An OAuth login always gives a read-write access with the approval mode `elicitation`. For a read-only access, use a static token. For the approval mode `app`, use `access set` after the login.

## Static token

A bot or a CLI without a browser uses a static token:

```bash
zapgate tokens create my-bot --accounts work --read-only
```

```text
created the access my-bot (static token, accounts: work, read-only)
token: <token>
this is the only time the token is shown; store it now
```

The token shows only this time. Keep it in a password manager. zapgate keeps only its hash, so a lost token does not come back: revoke the access and create another. A static token does not expire.

`--accounts` takes a comma-separated list or `all`. `all` gives the accounts that exist now. An account that you pair later does not join by itself: use `access set`.

`--approval` sets the approval mode: `elicitation` (the default) or `app`.

## Read-only access

A read-only access does not see the write tools or `mark_read`. Give this mode to every bot that acts without a person: a client without elicitation runs a write without asking you (see [Approval](#approval)). You choose the mode in `tokens create`, and it does not change later. To change it, revoke the access and create another.

## Change and revoke

| Command | What it does |
| --- | --- |
| `zapgate access list` | Shows each OAuth login and static token, with the kind (`oauth` or `static`), the accounts, the mode, the approval mode and the creation date |
| `zapgate access set <name> [--accounts <a,b\|all>] [--approval elicitation\|app]` | Changes the accounts, the approval mode or both. Give at least one. The change applies on the next call, without a new login |
| `zapgate access revoke <name>` | Ends the access now. Its next call gets 401 |

After `access set` with `--accounts`, zapgate tells the open sessions that the tool list changed. The audit log records each `access set` with the approval mode that the access keeps.

## Approval

Every write passes through your approval: `send_message`, `send_media`, `react`, `edit_message` and `delete_message`.

- **Client with elicitation** (Claude Code, Codex, Cursor): zapgate opens a question in the client, with the exact account, chat and content. Only "accept" runs the action. A decline or a cancel sends nothing, and the agent gets the error `the owner did not approve`. The answer is valid only for the action that you saw: when an argument changes, zapgate asks again. Each answer is valid once, also after a restart.
- **Client without elicitation** (claude.ai, ChatGPT, most bots): zapgate runs the action. The write tools have `destructiveHint: true`, and the approval is the client's own tool permission. A client set to allow everything writes without asking. For this reason, a bot without a person gets a read-only access.
- **Access with the approval mode `app`**: zapgate does not open the question, also in a client with elicitation. The approval is the client's own tool permission.

The result of each write has the field `approval`, which tells which case happened: `owner_approved`, `client_without_elicitation` or `access_trusts_app`.

The question waits up to 5 minutes. While it waits, zapgate sends a ping every 25 seconds, so that a proxy does not close the idle response.

### Approval mode

| Mode | What happens on a write |
| --- | --- |
| `elicitation` (default) | In a client with elicitation, zapgate opens the approval question. In a client without elicitation, zapgate runs the action |
| `app` | zapgate runs the action without the question. The approval is the client's own tool permission |

Use `app` only for a client that has elicitation but cannot show the question to you, such as Cursor as a cloud agent. In that client, keep the write tools on "ask every time".

```bash
zapgate access set cursor --approval app
```

A read-only access has no write tools, so its approval mode changes nothing.

`mark_read` does not pass through the approval. The agent marks a chat as read only when you ask. No read tool marks messages as read.

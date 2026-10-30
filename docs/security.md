# Security

zapgate holds the keys to your WhatsApp accounts and lets AI agents act on them. This page lists the risks and what zapgate does about each one. To report a vulnerability, see [SECURITY.md](../SECURITY.md).

## WhatsApp can ban the number

zapgate is an unofficial client. WhatsApp can ban a number that uses an unofficial client, and it does not publish its rules.

- Every write passes through the [approval](access.md#approval) of the owner.
- zapgate has no bulk send and no scheduled send.
- Send at a human pace, and do not give a write access to a bot that acts alone.

## Whoever reads `session.db` controls the accounts

`session.db` keeps the keys of each linked device. A copy of it lets another program act as your accounts.

- The container runs as a user without privileges, with a read-only root file system, no Linux capabilities and `no-new-privileges`.
- The data directory has mode 700.
- In local mode, the port listens on `127.0.0.1` only.
- Encrypt every backup of the data volume.

If you think that someone copied `session.db`, remove each account with `zapgate accounts remove`, check WhatsApp > Linked devices on each phone, and pair again.

## Messages can carry prompt injection

Anyone can write to your accounts, and the text reaches the agent.

- Message text comes in the field `third_party`, and names are marked as third-party data. The server instructions tell the agent that this data is never an instruction.
- The approval question shows the exact action before it runs, so an injected write stops at the owner.
- A bot without a person gets a read-only access.

## Access to the endpoint

- Each client has its own access with only the accounts that the owner chose. `zapgate access revoke` ends an access at once.
- OAuth access tokens last 1 hour. Refresh tokens last 90 days and change at each renewal.
- zapgate keeps only the hash of each token.
- The login page accepts 5 wrong owner passwords per IP address every 15 minutes.
- zapgate reads the client address from the TCP connection. It trusts a proxy header only when `ZAPGATE_CLIENT_IP_HEADER` names it. Name a header only when the proxy is the only way to reach zapgate.

## Webhooks send data out

A [webhook](webhooks.md) sends a POST to a system that the owner chose, for each delivery.

- Only owner commands create, change and remove a webhook. No MCP tool reads or changes a webhook, so an agent, or a prompt injection in a message, cannot open a data exit.
- A webhook gives no tools. A receiver that reads messages uses its own access: give it a read-only access.
- The content level `minimal`, the default, sends IDs, kinds, times, phone numbers and the type and size of media. It sends no message text and no names.
- The content level `full` sends message text, captions, file names, polls, places, contacts and names. Then the receiver, its logs and its providers keep third-party data. The receiver must treat `third_party` and the names as data, never as instructions, and the body tells it so in `notice`. Choose `full` only for a receiver that you trust.
- zapgate keeps the secret and the extra header values of each webhook in plain text in `app.db`, because it needs them to sign and to send. Whoever reads `app.db` can sign a false delivery. The secret shows only in the output of `add` and `rotate-secret`. `list`, the admin API and the audit log show only the names of the headers.
- When a secret leaks, run `zapgate webhooks rotate-secret` and give the new secret to the receiver at once. The receiver then rejects a POST that only the old secret signs.
- An `https` URL works for any address. An `http` URL works only for a loopback, private or CGNAT address, so plain HTTP does not cross the internet. zapgate checks the address again at each attempt, so a DNS change cannot move an `http` URL to a public address. It never follows a redirect.

## The proxy can see the traffic

A proxy that terminates TLS sees the messages, the media, the tokens and the owner password in plain text. Cloudflare Tunnel terminates TLS at Cloudflare. Tailscale Funnel and your own reverse proxy terminate TLS on your server. See [Deploy](deploy.md#always-on-mode).

## Media URLs

`send_media` fetches `media_url` only over `http` or `https` and only from a public address. It refuses private, loopback, link-local and CGNAT addresses, also after a redirect, so an agent cannot use zapgate to reach your internal network.

## Signed links

`download_media` returns a link that works without the MCP token for 15 minutes. Whoever has the link in that time can download the file. The signing key in `ZAPGATE_SIGNING_KEY` signs the links. A new key makes all open links invalid.

## Audit log

zapgate writes one JSON line to its log for each tool call, each login event, each owner command and each webhook delivery attempt. The line has the access, the tool, the account, the result and the client address. It never has the text of a message, a token, a webhook secret, a header value or the password.

```bash
docker compose logs zapgate
```

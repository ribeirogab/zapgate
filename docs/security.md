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

## The proxy can see the traffic

A proxy that terminates TLS sees the messages, the media, the tokens and the owner password in plain text. Cloudflare Tunnel terminates TLS at Cloudflare. Tailscale Funnel and your own reverse proxy terminate TLS on your server. See [Deploy](deploy.md#always-on-mode).

## Media URLs

`send_media` fetches `media_url` only over `http` or `https` and only from a public address. It refuses private, loopback, link-local and CGNAT addresses, also after a redirect, so an agent cannot use zapgate to reach your internal network.

## Signed links

`download_media` returns a link that works without the MCP token for 15 minutes. Whoever has the link in that time can download the file. The signing key in `ZAPGATE_SIGNING_KEY` signs the links. A new key makes all open links invalid.

## Audit log

zapgate writes one JSON line to its log for each tool call, each login event and each owner command. The line has the access, the tool, the account, the result and the client address. It never has the text of a message, a token or the password.

```bash
docker compose logs zapgate
```

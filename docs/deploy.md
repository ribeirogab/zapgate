# Deploy

zapgate runs in two modes:

| Mode | Where | Clients |
| --- | --- | --- |
| Local | Your computer, on `http://localhost:8080` | Clients on the same computer: Claude Code, Codex, Cursor |
| Always-on | A server that stays on, with a public HTTPS address | All clients, also claude.ai, ChatGPT and the mobile apps |

claude.ai, ChatGPT and the Claude and ChatGPT mobile apps call MCP servers from their own cloud. They cannot reach `localhost`, so they need the always-on mode.

zapgate must stay on to receive messages. When it stops, WhatsApp keeps the messages for the linked device and sends them when zapgate connects again. While it is off, no client can call it.

## Local mode

You need Docker.

```bash
git clone https://github.com/ribeirogab/zapgate.git
```

```bash
cd zapgate/deploy && ./init-env.sh && docker compose up -d --build
```

The `init-env.sh` script writes `deploy/.env` with mode 600, a random owner password and a random signing key. It never changes a `.env` that exists. The container publishes port 8080 on `127.0.0.1` only.

Then [pair an account](accounts.md) and [connect a client](clients.md).

## Owner commands

The owner commands run inside the container and talk to the server through a Unix socket. From `deploy/`:

```bash
docker compose exec zapgate zapgate help
```

A shell alias makes them shorter. Put the path of your clone in it:

```bash
alias zapgate='docker compose -f ~/zapgate/deploy/compose.yaml exec zapgate zapgate'
```

| Command | What it does |
| --- | --- |
| `zapgate status` | Shows each account with its state, messages, media size and retention |
| `zapgate accounts ...` | Pairs, describes, lists and removes accounts, and changes their retention ([Accounts](accounts.md)) |
| `zapgate tokens create ...` | Creates a static token ([Access](access.md)) |
| `zapgate access ...` | Lists, changes and revokes accesses ([Access](access.md)) |
| `zapgate password` | Prints the owner password of the OAuth login page |

## Always-on mode

Run zapgate on a server that stays on: a VPS, a home server or a Raspberry Pi. Give it a public HTTPS address in one of the ways below, and set `ZAPGATE_PUBLIC_URL` in `deploy/.env` to that address, without `/mcp`.

All three ways put a proxy in front of zapgate. The proxy must not buffer responses and must keep a response open for at least 6 minutes: an approval waits up to 5 minutes, and zapgate sends a ping every 25 seconds while it waits.

### Cloudflare Tunnel

The tunnel needs no open port on the server.

1. In the Cloudflare dashboard, open Zero Trust > Networks > Tunnels and create a tunnel of type `cloudflared`.
2. Add a public hostname, for example `wa.example.com`, with the service `http://zapgate:8080`.
3. Copy the tunnel token.
4. In `deploy/.env`, set `ZAPGATE_PUBLIC_URL=https://wa.example.com` and `CLOUDFLARE_TUNNEL_TOKEN=<token>`.
5. Start zapgate with the tunnel overlay:

```bash
docker compose -f compose.yaml -f compose.tunnel.yaml up -d --build
```

The overlay adds the `cloudflared` container, removes the port on `127.0.0.1`, and sets `ZAPGATE_CLIENT_IP_HEADER=CF-Connecting-IP`.

Add a rate limit rule at the Cloudflare edge for `/oauth/authorize`, `/oauth/register` and `/oauth/token`, for example 10 requests per 10 seconds per IP.

> [!IMPORTANT]
> Cloudflare terminates TLS. It sees in plain text the messages, the media, the tokens and the owner password that go through the tunnel.

### Tailscale Funnel

Funnel gives the server a public HTTPS address on your tailnet domain. TLS ends on your server, so Tailscale does not see the content.

```bash
tailscale funnel --bg 8080
```

Set `ZAPGATE_PUBLIC_URL=https://<machine>.<tailnet>.ts.net`. Keep `ZAPGATE_CLIENT_IP_HEADER` empty. zapgate then sees every client with the address of the local proxy, so all clients share one limit of wrong passwords.

### Reverse proxy

With your own proxy (Caddy, nginx, Traefik) and a TLS certificate, point the proxy to `127.0.0.1:8080`. Make the proxy write the client address in a header that it always replaces, and name that header in `ZAPGATE_CLIENT_IP_HEADER`. For example, in nginx:

```nginx
proxy_set_header X-Real-IP $remote_addr;
proxy_buffering off;
proxy_read_timeout 360s;
```

```bash
ZAPGATE_CLIENT_IP_HEADER=X-Real-IP
```

## Configuration

zapgate reads its configuration from environment variables. The compose file reads them from `deploy/.env`.

| Variable | Default | What it does |
| --- | --- | --- |
| `ZAPGATE_PUBLIC_URL` | (required) | The address that clients use, without `/mcp`. It is also the OAuth issuer. In local mode: `http://localhost:8080` |
| `ZAPGATE_OWNER_PASSWORD` | (required) | The password of the OAuth login page |
| `ZAPGATE_SIGNING_KEY` | (required) | A hex key of at least 32 bytes. It signs the media links and the approval answers |
| `ZAPGATE_RETENTION_MONTHS` | `6` | The default retention, from 1 to 1200 months |
| `ZAPGATE_CLIENT_IP_HEADER` | (empty) | The header where your proxy writes the client address, for example `CF-Connecting-IP`. Empty: zapgate uses the address of the TCP connection |
| `ZAPGATE_LISTEN` | `0.0.0.0:8080` | The address of the HTTP listener |
| `ZAPGATE_DATA_DIR` | `/data` | The directory of the databases and the media |
| `ZAPGATE_ADMIN_SOCKET` | `/run/zapgate/admin.sock` | The Unix socket of the owner commands |

> [!CAUTION]
> Set `ZAPGATE_CLIENT_IP_HEADER` only when the proxy is the only way to reach zapgate. Otherwise a client can write any address in that header and get around the limit of wrong passwords.

Keep the signing key the same. A new key makes the open media links and the approval answers invalid.

## Without Docker

zapgate is one static binary. Download it from the [releases](https://github.com/ribeirogab/zapgate/releases), or build it with Go 1.26:

```bash
go install github.com/ribeirogab/zapgate/cmd/zapgate@latest
```

Outside the container, set `ZAPGATE_DATA_DIR` and `ZAPGATE_ADMIN_SOCKET` to paths that your user can write, and export the same variables for the owner commands. On macOS, the socket path can have at most 104 characters, for example `~/.zapgate/admin.sock`. Voice notes need `ffmpeg` and `ffprobe` on the `PATH`.

```bash
zapgate serve
```

## Updates

The compose file uses the image `ghcr.io/ribeirogab/zapgate:latest`. To get the latest release:

```bash
docker compose pull && docker compose up -d
```

To stay on one version, set it in `deploy/.env`, without the `v`:

```bash
ZAPGATE_VERSION=0.1.0
```

To run the code of `main` instead of a release:

```bash
git pull && docker compose up -d --build
```

Update at least once a month. WhatsApp shows an "outdated client" warning to an old linked device and can disconnect it.

The database changes of a new version run when zapgate starts, and they only go forward. Make a backup before an update, because an older version cannot go back to the old database.

## Data and backups

The data lives in the Docker volume `zapgate_data`, mounted at `/data`:

| File | What it keeps |
| --- | --- |
| `session.db` | The whatsmeow session: devices, keys, contacts and the map between LID and phone number. **Whoever reads this file controls the accounts** |
| `app.db` | The accounts, chats, messages, the search index, the OAuth clients, the accesses, the token hashes and the used approvals |
| `media/` | The media that arrived and the media that `send_media` sent, one directory per account, with random file names |
| `uploads/` | The files sent to `/uploads`, until the `upload_id` expires |
| `tmp/` | Temporary files of `send_media`. zapgate empties it at start |

zapgate makes no backup. Without the volume, you pair each account again and lose the local history: the history comes back only with the part that the phone sends at pairing. To make a backup, stop zapgate and copy the volume to a safe place. Encrypt the copy, because it contains `session.db`.

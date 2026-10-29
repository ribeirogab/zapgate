# Our own MCP server on whatsmeow

The owner has more than one WhatsApp account and wants agents to read, search and write in them. The agents run on a computer, on claude.ai, on ChatGPT and as bots. Each client reaches only the accounts of its access, and every write passes through the owner's approval.

WhatsApp has no official API for a personal account. The options are libraries that speak the WhatsApp Web protocol as a linked device, or ready REST servers on top of those libraries.

zapgate is our own MCP server, in Go, on the whatsmeow library.

## Decisions

- **whatsmeow directly, without a REST server in the middle.** zapgate already does the accounts, the login, the approval, the retention and the search. A REST server in the middle would only translate calls, and it would bring a database and an API that someone else owns.
- **Go 1.26.** whatsmeow requires Go 1.26 in its `go.mod`.
- **The official Go SDK of MCP.** It has the Streamable HTTP transport, elicitation and the OAuth parts that the approval and the access use.
- **Two SQLite databases with the `modernc.org/sqlite` driver.** The driver is pure Go, so the binary needs no cgo, and FTS5 is included. `session.db` belongs to whatsmeow: devices, keys, contacts and the LID map. `app.db` belongs to zapgate and is the source of truth for the history. zapgate does not write to `session.db`.
- **One process owns everything.** It has one whatsmeow client per account, all on one `session.db`, plus the ingest, the retention, the HTTP interface and the owner interface. A new account needs no restart.
- **Two seams with test doubles: the WhatsApp port and the transcoder.** An internal interface separates zapgate from whatsmeow: send, react, edit, delete, download, pair, ask for history and the event stream. The real implementation uses whatsmeow, and the tests use a fake one. The second seam is the audio transcoder, which calls `ffmpeg` and `ffprobe` for voice notes. The tests use a fake transcoder by default, because the test machine can lack `ffmpeg`. One test uses the real `ffmpeg` when it is on the `PATH`. All other parts run for real in the tests.

## Considered options

- **GOWA (go-whatsapp-web-multidevice):** it would only translate between zapgate and whatsmeow. The retention would depend on a database that someone else owns, and its REST API changed in incompatible ways in v6, v8 and v9.
- **WAHA:** it has many sessions only in the paid version, and the owner wants as many accounts as needed.
- **Baileys:** it is Node, and it gains nothing over whatsmeow for a new server. The Go MCP SDK and the static binary counted for Go.
- **The official WhatsApp Business API:** it does not reach a personal number and does not read the app history.

## Consequences

- WhatsApp can ban a number that uses an unofficial client. zapgate sends at a human pace, every write passes through the approval, and nothing is a bulk send. The README says this first.
- whatsmeow has no tags. The version is fixed in `go.sum` by a pseudo-version, and an update is a normal change, checked by the ingest tests. A version that is months old gets the "outdated client" warning from WhatsApp.
- The phone of each number must open WhatsApp at least every 14 days. Without that, the account drops, and the owner pairs again. `zapgate status` and `list_accounts` show the state.
- A number accepts up to 4 linked devices, and zapgate uses one.
- Whoever reads `session.db` controls the accounts. The data directory has mode 700 and stays out of git.
- The old history is only what the phone sends at pairing. The tool `request_older_history` asks for more, within the WhatsApp limit.

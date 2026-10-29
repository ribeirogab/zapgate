# Signed webhooks from a durable journal

zapgate tells nobody when something happens in an account. A client knows about a new message only when it calls `list_chats` again. A bot that must reply at once asks every few seconds and spends calls and paid agent turns, or it asks every few minutes and the person waits. Automations, queues and agent platforms that only react to HTTP calls cannot use zapgate.

The owner needs a notice to an external system without polling, with control over the accounts, the chats and the content that leave zapgate. The receiver needs proof that the notice came from zapgate. A notice must survive a receiver or zapgate that stops for some minutes, and a burst of messages must not start a burst of runs in the receiver.

zapgate pushes signed webhooks from an event journal in `app.db`.

## Decisions

- **Push by HTTP webhook.** zapgate sends an HTTP POST to the URL of a webhook. A POST wakes bots, automations and Claude Code routines. An MCP notification reaches only an open session.
- **Only owner commands create, change and remove a webhook.** No MCP tool reads or changes a webhook. An agent, or a prompt injection in a message, cannot open a data exit. A webhook is its own entity, with its own name and accounts. It gives no tools: a receiver that must read uses its own access.
- **The Standard Webhooks signature.** Each webhook has a `whsec_` secret. The HMAC-SHA256 covers the `webhook-id`, the `webhook-timestamp` and the body, so a receiver can reject a changed or a replayed POST with a Standard Webhooks library. zapgate keeps the secret in `app.db`, because it needs the value to sign.
- **A durable event journal.** The ingest writes a live message and its event in the same SQLite transaction. A crash after the commit does not lose the event. Messages from the history sync and messages that the account sends write no event.
- **Deliveries in `app.db`.** A delivery and its retries stay in `app.db` until the receiver accepts it, also across a restart. Every attempt uses the same `webhook-id`, so the receiver can drop a duplicate. The delivery is at least once.
- **One delivery per chat, after a quiet window, with a max wait.** A burst of messages in one chat gives one delivery and one run in the receiver. The deliveries of one chat leave in order.
- **Minimal content by default.** A delivery carries IDs, kinds, times and phone numbers. Message text and names leave zapgate only when the owner sets the content level `full` on a webhook.

## Considered options

- **MCP notifications:** they reach only an open session, and the 2026-07-28 revision of the specification replaced `resources/subscribe` with `subscriptions/listen`.
- **Retries in memory, as Evolution API, WAHA, Wuzapi and GOWA do:** a restart loses the pending notices.
- **A signature of the body only, as these gateways do:** it gives no protection against a replayed POST.
- **A pull API with long poll or SSE:** it is out of scope now. The event journal keeps it possible later.

## Consequences

- This decision keeps [ADR 0001](0001-own-mcp-server-on-whatsmeow.md). The same process owns the webhooks, the journal and the deliveries. `app.db` stays the source of truth, and `session.db` does not change.
- A webhook with the content level `full` sends third-party data to a system that the owner does not control. The receiver must treat it as data, never as instructions.
- A receiver gets a delivery more than once now and then, and it must drop a `webhook-id` that it already saw.
- A receiver whose clock is wrong rejects valid signatures. The Standard Webhooks libraries accept 5 minutes of difference.
- The journal and the settled deliveries grow with the traffic. The daily retention pass deletes them after 7 days.

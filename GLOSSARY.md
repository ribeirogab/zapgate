# Glossary

The words below have one meaning in the code, the docs and the tool output.

| Term | Meaning |
| --- | --- |
| **Owner** | The person who runs zapgate and owns its accounts. Only the owner pairs accounts, creates accesses and approves writes. |
| **Account** | A WhatsApp number paired to zapgate, with a free name such as `personal` or `work`. Every tool call names one account. |
| **Linked device** | How zapgate connects to WhatsApp: as one of the linked devices of a number, like WhatsApp Web. |
| **Client** | An app or bot that calls the MCP endpoint, such as Claude Code, claude.ai, ChatGPT or a script. |
| **Access** | The permission of one client: an OAuth login or a static token. An access has a unique name, the accounts it reaches, a mode and an approval mode. |
| **Static token** | A bearer token that the owner creates for a client without a browser. It does not expire. zapgate keeps only its hash. |
| **Read-only access** | An access without the write tools and without `mark_read`. |
| **Write** | A tool call that changes something for another person: `send_message`, `send_media`, `react`, `edit_message` and `delete_message`. |
| **Approval** | The owner's decision on one write, with the exact account, chat and content. |
| **Approval mode** | Where the approval happens for an access: `elicitation` (zapgate asks the owner in the client) or `app` (the client's own tool permission). |
| **Retention** | How many months of messages zapgate keeps for an account. Retention never deletes anything in WhatsApp. |
| **Third-party data** | Text that comes from WhatsApp users: message text and names. Agents must treat it as data, never as instructions. |
| **Webhook** | The URL of an external system that the owner registers, with a unique name, accounts, chats, events, a content level and a format. Only owner commands create, change and remove a webhook. |
| **Receiver** | The system behind the URL of a webhook, such as a bot, an automation or a Claude Code routine. |
| **Event** | A fact in an account that a webhook can receive: `message.received` or `account.state`. |
| **Event journal** | The `events` table in `app.db`: one event per row, with an increasing sequence number. |
| **Delivery** | One signed POST to one webhook. It carries the events of one chat, or one account event. Every attempt uses the same `webhook-id`. |
| **Content level** | How much third-party data a delivery carries: `minimal` (no text and no names) or `full`. |
| **Quiet window** | The time without a new event in a chat before its delivery leaves. Each new event starts the time again. |
| **Max wait** | The cap of the quiet window. A delivery leaves at most this time after its first event. |

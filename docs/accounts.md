# Accounts

An account is a WhatsApp number paired to zapgate, with a free name. The name has lowercase letters, digits and hyphens, with up to 32 characters. There is no limit of accounts, and a new account needs no restart.

The commands below are [owner commands](deploy.md#owner-commands).

## Pair an account

```bash
zapgate accounts add personal --phone +5511988887777 --description "personal number"
```

The phone number has the country code. The command shows an 8-character code:

```text
on the phone of the account personal, open WhatsApp > Settings > Linked devices > Link a device > Link with phone number instead, and type this code:

    ABCD-1234

waiting for the phone...
```

On the phone of that number, type the code. The command waits until the phone confirms and says when the account is connected. The phone has 3 minutes. After that, run the command again.

To pair with a QR code in the terminal, add `--qr`. The `--phone` stays required:

```bash
zapgate accounts add work --phone +5511977776666 --qr
```

At pairing, the phone sends a part of the history, and zapgate keeps it. After that, zapgate records every message that arrives or goes out, also the messages that you send from the phone.

## Describe, list and remove

| Command | What it does |
| --- | --- |
| `zapgate accounts describe <name> <text>` | Changes the description. Agents read it in `list_accounts` to know what each account is for |
| `zapgate accounts list` | Shows the name, number, profile name, state and description of each account |
| `zapgate accounts remove <name> [--yes]` | Asks first, unlinks the device in WhatsApp, deletes the local data of the account and removes it from every access. `--yes` skips the question |
| `zapgate accounts retention <name> <months\|default>` | Changes the retention of the account, from 1 to 1200 months. `default` goes back to `ZAPGATE_RETENTION_MONTHS` |

When the phone still shows the device after `remove`, the command tells you. Then remove the device in WhatsApp > Linked devices.

## States

| State | When |
| --- | --- |
| `pairing` | The pairing started, and the phone did not confirm yet |
| `connected` | The account is connected to WhatsApp |
| `disconnected` | The connection dropped. zapgate connects again by itself, also after a reboot |
| `logged_out` | WhatsApp unlinked the device. The account needs a new pairing |

A [webhook](webhooks.md) with the event `account.state` gets each change to `connected`, `disconnected` or `logged_out`.

## A logged-out account

WhatsApp unlinks the device when the phone does not open WhatsApp for 14 days, or when someone removes the device on the phone. `zapgate status` and `list_accounts` show `logged_out`. To connect again, remove the account, pair it again with the same name and give it back to the accesses:

```bash
zapgate accounts remove personal
```

```bash
zapgate accounts add personal --phone +5511988887777
```

```bash
zapgate access set claude-code --accounts personal,work
```

`remove` deletes the local history and media of the account. The history comes back only with the part that the phone sends at pairing.

## Retention

A daily task deletes from zapgate the messages older than the retention. The default is 6 months, in `ZAPGATE_RETENTION_MONTHS`. An account can have its own value with `accounts retention`. Retention never deletes anything in WhatsApp.

The task runs when the server starts and then every 24 hours. It compares the send date of each message with the retention of its account. With the message, it deletes the reactions, the poll, the location or contacts, the search entry and the media file. The chat stays in `list_chats`, also without messages.

The task deletes in batches of 1,000 messages, one transaction per batch, with a short pause between batches. New messages continue to arrive during a large pass. `zapgate status` shows the last pass and what it deleted.

## WhatsApp limits

| Limit | Value |
| --- | --- |
| Linked devices per number | 4. zapgate uses one |
| Phone without opening WhatsApp | 14 days at most. After that, the account goes to `logged_out` |
| Old history | Only what the phone sends. `request_older_history` asks for up to 50 messages at a time |

# Tools

Every tool needs the account name. Every response names its account and the phone number of the account. A large response comes in pages of about 10,000 tokens at most: follow `next_cursor`, `older_before` or `newer_after`.

| Tool | Kind | What it does |
| --- | --- | --- |
| `list_accounts` | read | Lists the accounts of the access, with the name, number, profile name, description and state |
| `list_chats` | read | Lists the chats of an account, with a search by name or number and an unread filter |
| `get_messages` | read | Reads a part of a chat, with `before`, `after` and `limit` |
| `search_messages` | read | Searches text in an account, with an optional chat and period |
| `get_chat` | read | Shows a chat and the members of a group |
| `search_contacts` | read | Searches contacts by name or number |
| `download_media` | read | Returns an image up to 5 MB inline, and a signed link for 15 minutes for any media. The link does not need the MCP token. When the download at arrival failed, the tool tries again now |
| `request_older_history` | read | Asks the phone for older messages of a chat |
| `send_message` | write | Sends text to a phone number with the country code or to a chat, with an optional quoted reply |
| `send_media` | write | Sends an image, video, audio, voice note or document from a URL, an upload or base64, with an optional caption, file name and quoted reply |
| `react` | write | Adds or removes a reaction |
| `edit_message` | write | Edits a text that the account sent |
| `delete_message` | write | Deletes for everyone a message that the account sent |
| `mark_read` | mark_read | Marks received messages as read, without approval |

Writes need [approval](access.md#approval). A read-only access does not see the write tools or `mark_read`.

The text of messages comes in the field `third_party`. Names come from WhatsApp users. Both are third-party data, and the server instructions tell the agent never to follow them as instructions.

## Send files

`send_media` takes exactly one of three sources:

| Source | For | Limit |
| --- | --- | --- |
| `upload_id` | An agent with a shell and a local file | 100 MB |
| `media_url` | A file at a public URL | 100 MB and 1 minute |
| `base64` | A small file, also as a data URL such as `data:image/png;base64,...` | 5 MB after decoding |

### Uploads

An agent with a shell sends the local file to `/uploads` and passes the `upload_id` to `send_media`. This way the file does not travel inside the tool argument. `/uploads` needs a bearer token, so use a static token:

```bash
curl -sS https://wa.example.com/uploads -H "Authorization: Bearer $ZAPGATE_TOKEN" -H 'Content-Type: application/pdf' --data-binary @report.pdf
```

The request body is the raw file. zapgate refuses a multipart form (`curl -F`). The response is `201` with the `upload_id` and `expires_at`. The `upload_id` is valid for 1 hour, and only for the access of the token that sent the file. A read-only access gets `403`.

### Media URL

zapgate fetches `media_url` only over `http` or `https` and only from a public address. It refuses a private, loopback, link-local or CGNAT address, also after a redirect.

### File types

The file type decides how WhatsApp shows the file: image (JPEG, PNG, WebP), video (MP4, 3GP), audio (Ogg, MP3, M4A, AAC, AMR) or document (all other types). zapgate reads the type from the `Content-Type`, then from the extension of `file_name`, then from the content. With `application/octet-stream`, pass `file_name`. Audio has no caption.

`as_voice_note` converts an audio or video file with ffmpeg to mono Ogg/Opus and sends it as a voice note, with its duration. In the approval question, you see the account, the chat, the kind, the file name, the size, the start of the SHA-256 and the caption.

## Limits

| Limit | Value | Source |
| --- | --- | --- |
| Edit a message | Up to 15 minutes after sending, only text of the account | WhatsApp |
| Delete for everyone | Up to about 2 days after sending. zapgate refuses after 48 hours | WhatsApp |
| File in `/uploads` | 100 MB, valid for 1 hour | zapgate |
| `base64` media in `send_media` | 5 MB | zapgate |
| `media_url` media in `send_media` | 100 MB and 1 minute, public address only, up to 5 redirects | zapgate |
| Request body on `/mcp` | 8 MB | zapgate |
| Image in the `download_media` response | 5 MB. The signed link is valid for 15 minutes | zapgate |
| Media downloads at the same time | 4 | zapgate |
| Download attempts for one media | 12, with a wait of 1 minute that doubles after each failure, up to 6 hours. Time with the account disconnected does not count | zapgate |
| Approval question | 5 minutes | zapgate |
| Wrong owner passwords | 5 per IP address every 15 minutes | zapgate |
| Old history | `request_older_history` asks for up to 50 messages at a time | WhatsApp |

A proxy in front of zapgate can have lower limits. For example, the Cloudflare Free plan accepts request bodies up to 100 MB.

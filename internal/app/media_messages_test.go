package app_test

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const (
	mariaSender  = `{"jid": "5531944443333@s.whatsapp.net", "phone": "+5531944443333", "name": "Maria"}`
	mariaChatRef = `{"jid": "5531944443333@s.whatsapp.net", "kind": "direct", "name": "Maria", "phone": "+5531944443333"}`
	cdnPath      = "/v/t62.7118-24/123_456?ccb=11-4&oh=x&oe=y"
)

func image(caption string) *waE2E.Message {
	return &waE2E.Message{ImageMessage: &waE2E.ImageMessage{
		Caption: proto.String(caption), Mimetype: proto.String("image/jpeg"), FileLength: proto.Uint64(48213),
		MediaKey: make([]byte, 32), DirectPath: proto.String(cdnPath), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
		URL:   proto.String("https://mmg.whatsapp.net/v/t62.7118-24/123_456?ccb=11-4"),
		Width: proto.Uint32(1080), Height: proto.Uint32(1920),
	}}
}

func video(caption string) *waE2E.Message {
	return &waE2E.Message{VideoMessage: &waE2E.VideoMessage{
		Caption: proto.String(caption), Mimetype: proto.String("video/mp4"), FileLength: proto.Uint64(1048576), Seconds: proto.Uint32(12),
		MediaKey: make([]byte, 32), DirectPath: proto.String(cdnPath), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
	}}
}

func voiceNote() *waE2E.Message {
	return &waE2E.Message{AudioMessage: &waE2E.AudioMessage{
		Mimetype: proto.String("audio/ogg; codecs=opus"), PTT: proto.Bool(true), Seconds: proto.Uint32(7), FileLength: proto.Uint64(9120),
		MediaKey: make([]byte, 32), DirectPath: proto.String(cdnPath), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
	}}
}

func document(fileName string) *waE2E.DocumentMessage {
	return &waE2E.DocumentMessage{
		FileName: proto.String(fileName), Mimetype: proto.String("application/pdf"), FileLength: proto.Uint64(20480), PageCount: proto.Uint32(2),
		MediaKey: make([]byte, 32), DirectPath: proto.String(cdnPath), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
	}
}

func sticker() *waE2E.Message {
	return &waE2E.Message{StickerMessage: &waE2E.StickerMessage{
		Mimetype: proto.String("image/webp"), FileLength: proto.Uint64(30000), IsAnimated: proto.Bool(false),
		MediaKey: make([]byte, 32), DirectPath: proto.String(cdnPath), FileSHA256: make([]byte, 32), FileEncSHA256: make([]byte, 32),
	}}
}

func mariaMessages(messages string) string {
	return `{` + envelope + `, "chat": ` + mariaChatRef + `, "messages": [` + messages + `]}`
}

func fromMaria(id, sentAt, rest string) string {
	return `{"id": "` + id + `", "from_me": false, "sender": ` + mariaSender + `, "sent_at": "` + sentAt + `", ` + rest + `}`
}

func TestMediaMessagesKeepTheKindTheCaptionAndTheMediaDetails(t *testing.T) {
	cases := []struct {
		name string
		raw  *waE2E.Message
		want string
	}{
		{
			name: "image with caption",
			raw:  image("foto da nota"),
			want: `"kind": "image", "media": {"mime": "image/jpeg", "size": 48213}, "third_party": {"text": "foto da nota"}`,
		},
		{
			name: "video",
			raw:  video("olha isso"),
			want: `"kind": "video", "media": {"mime": "video/mp4", "size": 1048576, "seconds": 12}, "third_party": {"text": "olha isso"}`,
		},
		{
			name: "voice note",
			raw:  voiceNote(),
			want: `"kind": "audio", "media": {"mime": "audio/ogg; codecs=opus", "size": 9120, "seconds": 7, "voice_note": true}, "third_party": {}`,
		},
		{
			name: "document",
			raw:  &waE2E.Message{DocumentMessage: document("contrato.pdf")},
			want: `"kind": "document", "media": {"mime": "application/pdf", "size": 20480}, "third_party": {"file_name": "contrato.pdf"}`,
		},
		{
			name: "sticker",
			raw:  sticker(),
			want: `"kind": "sticker", "media": {"mime": "image/webp", "size": 30000}, "third_party": {}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := apptest.Start(t)
			session := workSession(t, h)

			h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "3EB0M1", "Maria", morning), tc.raw))

			result := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
			assertJSON(t, result.StructuredContent, mariaMessages(fromMaria("3EB0M1", "2026-10-13T09:00:00Z", tc.want)))
		})
	}
}

func TestWrappedMessagesAreUnwrapped(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	ephemeral := &waE2E.Message{EphemeralMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String("some em 24h"), ContextInfo: &waE2E.ContextInfo{Expiration: proto.Uint32(86400)}},
	}}}
	viewOnce := &waE2E.Message{ViewOnceMessageV2: &waE2E.FutureProofMessage{Message: image("só uma vez")}}
	captioned := document("boleto.pdf")
	captioned.Caption = proto.String("segue o boleto")
	withCaption := &waE2E.Message{DocumentWithCaptionMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{DocumentMessage: captioned}}}

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W1", "Maria", morning), ephemeral))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W2", "Maria", morning.Add(time.Minute)), viewOnce))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "W3", "Maria", morning.Add(2*time.Minute)), withCaption))

	result := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	assertJSON(t, result.StructuredContent, mariaMessages(
		fromMaria("W1", "2026-10-13T09:00:00Z", `"kind": "text", "third_party": {"text": "some em 24h"}`)+`, `+
			fromMaria("W2", "2026-10-13T09:01:00Z", `"kind": "image", "media": {"mime": "image/jpeg", "size": 48213}, "third_party": {"text": "só uma vez"}`)+`, `+
			fromMaria("W3", "2026-10-13T09:02:00Z", `"kind": "document", "media": {"mime": "application/pdf", "size": 20480}, "third_party": {"text": "segue o boleto", "file_name": "boleto.pdf"}`)))
}

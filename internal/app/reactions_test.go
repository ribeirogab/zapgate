package app_test

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

const ownerSender = `{"jid": "5511988887777@s.whatsapp.net", "phone": "+5511988887777", "name": "Alex"}`

func reaction(chat types.JID, targetID string, targetFromMe bool, emoji string, at time.Time) *waE2E.Message {
	return &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key:               &waCommon.MessageKey{RemoteJID: proto.String(chat.String()), FromMe: proto.Bool(targetFromMe), ID: proto.String(targetID)},
		Text:              proto.String(emoji),
		SenderTimestampMS: proto.Int64(at.UnixMilli()),
	}}
}

func reactionInfo(info types.MessageInfo) types.MessageInfo {
	info.Type = "reaction"
	return info
}

func TestReactionsAppearOnTheMessageAndAnEmptyEmojiRemovesThem(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "M1", "Maria", morning), text("vamos?")))
	h.WhatsApp.Emit(workPhone, message(ownFromPhone(mariaLID, mariaJID, "MINE", morning.Add(time.Minute)), fromPhone(mariaLID, "bora")))
	at := morning.Add(2 * time.Minute)

	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R1", "Maria", at)), reaction(mariaJID, "MINE", true, "👍", at)))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R2", "Maria", at)), reaction(mariaJID, "M1", false, "😂", at)))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R3", "Maria", at.Add(time.Minute))), reaction(mariaJID, "M1", false, "", at.Add(time.Minute))))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(ownFromPhone(mariaLID, mariaJID, "R4", at.Add(2*time.Minute))), &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{
		DestinationJID: proto.String(mariaLID.String()),
		Message:        reaction(mariaLID, "M1", false, "❤️", at.Add(2*time.Minute)),
	}}))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R5", "Maria", at)), reaction(mariaJID, "MINE", true, "😮", at.Add(-time.Second))))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(direct(mariaJID, "R6", "Maria", at)), reaction(mariaJID, "UNKNOWN", true, "👍", at)))

	result := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	assertJSON(t, result.StructuredContent, mariaMessages(
		fromMaria("M1", "2026-10-13T09:00:00Z", `"kind": "text", "third_party": {"text": "vamos?"},
			"reactions": [{"emoji": "❤️", "from_me": true, "sender": `+ownerSender+`, "reacted_at": "2026-10-13T09:04:00Z"}]`)+`,
		{"id": "MINE", "from_me": true, "sender": `+ownerSender+`, "sent_at": "2026-10-13T09:01:00Z", "kind": "text", "third_party": {"text": "bora"},
			"reactions": [{"emoji": "👍", "from_me": false, "sender": `+mariaSender+`, "reacted_at": "2026-10-13T09:02:00Z"}]}`))
}

func TestAReactionFromAnUnknownLIDMovesToThePhoneNumberOnceItIsKnown(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "G1", "Maria", morning), text("bom dia")))
	h.WhatsApp.Emit(workPhone, message(reactionInfo(inGroup(familyJID, joaoLID, types.JID{}, "R1", "João", morning.Add(time.Minute))), reaction(familyJID, "G1", false, "☀️", morning.Add(time.Minute))))

	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, joaoLID, joaoJID, "G2", "João", morning.Add(2*time.Minute)), text("bom dia!")))

	var got struct {
		Messages []struct {
			Reactions []struct {
				Sender struct {
					JID   string `json:"jid"`
					Phone string `json:"phone"`
				} `json:"sender"`
			} `json:"reactions"`
		} `json:"messages"`
	}
	decode(t, getMessages(t, session, withWork(map[string]any{"chat": familyJID.String()})).StructuredContent, &got)
	if len(got.Messages[0].Reactions) != 1 || got.Messages[0].Reactions[0].Sender.Phone != "+"+joaoPhone {
		t.Fatalf("reactions %+v", got.Messages[0].Reactions)
	}
}

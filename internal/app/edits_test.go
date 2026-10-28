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

func edit(chat types.JID, targetID string, newContent *waE2E.Message, at time.Time) *waE2E.Message {
	return &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{
		ProtocolMessage: &waE2E.ProtocolMessage{
			Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
			Key:           &waCommon.MessageKey{RemoteJID: proto.String(chat.String()), FromMe: proto.Bool(false), ID: proto.String(targetID)},
			EditedMessage: newContent,
			TimestampMS:   proto.Int64(at.UnixMilli()),
		},
	}}}
}

func revoke(chat types.JID, targetID string) *waE2E.Message {
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  &waCommon.MessageKey{RemoteJID: proto.String(chat.String()), FromMe: proto.Bool(false), ID: proto.String(targetID)},
	}}
}

func editInfo(info types.MessageInfo, attr types.EditAttribute) types.MessageInfo {
	info.Edit = attr
	return info
}

func TestAnEditUpdatesTheTextAndMarksEditedAt(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "E1", "Maria", morning), text("oi, tudo bme?")))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "E2", "Maria", morning.Add(time.Minute)), image("foto antiga")))

	h.WhatsApp.Emit(workPhone, message(editInfo(direct(mariaJID, "X1", "Maria", morning.Add(2*time.Minute)), types.EditAttributeMessageEdit), edit(mariaJID, "E1", text("oi, tudo bem?"), morning.Add(2*time.Minute))))
	h.WhatsApp.Emit(workPhone, message(editInfo(direct(mariaJID, "X2", "Maria", morning.Add(3*time.Minute)), types.EditAttributeMessageEdit), edit(mariaJID, "E2", image("foto nova"), morning.Add(3*time.Minute))))
	h.WhatsApp.Emit(workPhone, message(editInfo(direct(mariaJID, "X3", "Maria", morning.Add(4*time.Minute)), types.EditAttributeMessageEdit), edit(mariaJID, "E1", text("versão velha"), morning.Add(time.Minute))))

	result := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	assertJSON(t, result.StructuredContent, mariaMessages(
		fromMaria("E1", "2026-10-13T09:00:00Z", `"kind": "text", "edited_at": "2026-10-13T09:02:00Z", "third_party": {"text": "oi, tudo bem?"}`)+`, `+
			fromMaria("E2", "2026-10-13T09:01:00Z", `"kind": "image", "edited_at": "2026-10-13T09:03:00Z", "media": {"mime": "image/jpeg", "size": 48213}, "third_party": {"text": "foto nova"}`)))
}

func TestOnlyTheSenderCanEditAMessageInAGroup(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "G1", "Maria", morning), text("almoço às 13h")))

	h.WhatsApp.Emit(workPhone, message(editInfo(inGroup(familyJID, joaoJID, types.JID{}, "X1", "João", morning.Add(time.Minute)), types.EditAttributeMessageEdit), edit(familyJID, "G1", text("almoço cancelado"), morning.Add(time.Minute))))

	got := page(t, session, map[string]any{"chat": familyJID.String()})
	if got.Messages[0].ThirdParty.Text != "almoço às 13h" {
		t.Fatalf("another member edited the message: %q", got.Messages[0].ThirdParty.Text)
	}
}

func TestARevokeForEveryoneMarksRevokedAtAndKeepsTheText(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)
	family(h)
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "D1", "Maria", morning), text("mensagem errada")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "G1", "Maria", morning), text("spam no grupo")))
	h.WhatsApp.Emit(workPhone, message(inGroup(familyJID, mariaLID, mariaJID, "G2", "Maria", morning.Add(time.Minute)), text("fica")))

	h.WhatsApp.Emit(workPhone, message(editInfo(direct(mariaJID, "X1", "Maria", morning.Add(5*time.Minute)), types.EditAttributeSenderRevoke), revoke(mariaJID, "D1")))
	h.WhatsApp.Emit(workPhone, message(editInfo(inGroup(familyJID, joaoJID, types.JID{}, "X2", "João", morning.Add(6*time.Minute)), types.EditAttributeAdminRevoke), revoke(familyJID, "G1")))
	h.WhatsApp.Emit(workPhone, message(editInfo(inGroup(familyJID, joaoJID, types.JID{}, "X3", "João", morning.Add(6*time.Minute)), types.EditAttributeSenderRevoke), revoke(familyJID, "G2")))

	result := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	assertJSON(t, result.StructuredContent, mariaMessages(
		fromMaria("D1", "2026-10-13T09:00:00Z", `"kind": "text", "revoked_at": "2026-10-13T09:05:00Z", "third_party": {"text": "mensagem errada"}`)))
	var group struct {
		Messages []struct {
			ID        string `json:"id"`
			RevokedAt string `json:"revoked_at"`
		} `json:"messages"`
	}
	decode(t, getMessages(t, session, withWork(map[string]any{"chat": familyJID.String()})).StructuredContent, &group)
	if len(group.Messages) != 2 || group.Messages[0].RevokedAt != "2026-10-13T09:06:00Z" || group.Messages[1].RevokedAt != "" {
		t.Fatalf("group messages %+v; want the admin revoke on G1 only", group.Messages)
	}
}

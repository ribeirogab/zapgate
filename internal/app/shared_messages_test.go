package app_test

import (
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/apptest"
)

func pollMessage() *waE2E.Message {
	return &waE2E.Message{
		PollCreationMessageV3: &waE2E.PollCreationMessage{
			Name:                   proto.String("Almoço?"),
			Options:                []*waE2E.PollCreationMessage_Option{{OptionName: proto.String("Sim")}, {OptionName: proto.String("Não")}},
			SelectableOptionsCount: proto.Uint32(1),
		},
		MessageContextInfo: &waE2E.MessageContextInfo{MessageSecret: make([]byte, 32)},
	}
}

func locationMessage() *waE2E.Message {
	return &waE2E.Message{LocationMessage: &waE2E.LocationMessage{
		DegreesLatitude: proto.Float64(-23.55), DegreesLongitude: proto.Float64(-46.63),
		Name: proto.String("Escritório"), Address: proto.String("Av. Paulista, 1000"),
	}}
}

func contactMessage() *waE2E.Message {
	return &waE2E.Message{ContactMessage: &waE2E.ContactMessage{
		DisplayName: proto.String("João"),
		Vcard:       proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:João\nTEL;type=CELL;waid=5511977776666:+55 11 97777-6666\nEND:VCARD"),
	}}
}

func contactsMessage() *waE2E.Message {
	return &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
		DisplayName: proto.String("2 contatos"),
		Contacts: []*waE2E.ContactMessage{
			{DisplayName: proto.String("Ana"), Vcard: proto.String("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Ana\r\nTEL;type=CELL;waid=5521966665555:+55 21 96666-5555\r\nEND:VCARD")},
			{DisplayName: proto.String("Escritório"), Vcard: proto.String("BEGIN:VCARD\nVERSION:3.0\nFN:Escritório\nTEL;type=WORK:+55 11 3333-4444\nEND:VCARD")},
		},
	}}
}

func TestPollLocationAndContactCardsAreReadable(t *testing.T) {
	h := apptest.Start(t)
	session := workSession(t, h)

	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "S1", "Maria", morning), pollMessage()))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "S2", "Maria", morning.Add(time.Minute)), locationMessage()))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "S3", "Maria", morning.Add(2*time.Minute)), contactMessage()))
	h.WhatsApp.Emit(workPhone, message(direct(mariaJID, "S4", "Maria", morning.Add(3*time.Minute)), contactsMessage()))

	result := getMessages(t, session, withWork(map[string]any{"chat": mariaJID.String()}))
	assertJSON(t, result.StructuredContent, mariaMessages(
		fromMaria("S1", "2026-10-13T09:00:00Z", `"kind": "poll", "third_party": {"text": "Almoço?", "poll": {"options": ["Sim", "Não"], "max_choices": 1}}`)+`, `+
			fromMaria("S2", "2026-10-13T09:01:00Z", `"kind": "location", "third_party": {"text": "Escritório", "location": {"latitude": -23.55, "longitude": -46.63, "address": "Av. Paulista, 1000"}}`)+`, `+
			fromMaria("S3", "2026-10-13T09:02:00Z", `"kind": "contact", "third_party": {"text": "João", "contacts": [{"name": "João", "phones": ["+5511977776666"]}]}`)+`, `+
			fromMaria("S4", "2026-10-13T09:03:00Z", `"kind": "contact", "third_party": {"text": "2 contatos", "contacts": [{"name": "Ana", "phones": ["+5521966665555"]}, {"name": "Escritório", "phones": ["+55 11 3333-4444"]}]}`)))
}

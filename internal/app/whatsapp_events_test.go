package app_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
)

const (
	mariaPhone = "5531944443333"
	joaoPhone  = "5541933332222"
)

var (
	workJID   = types.NewJID(workPhone, types.DefaultUserServer)
	workLID   = types.NewJID("100000000000001", types.HiddenUserServer)
	mariaJID  = types.NewJID(mariaPhone, types.DefaultUserServer)
	mariaLID  = types.NewJID("187654321098765", types.HiddenUserServer)
	joaoJID   = types.NewJID(joaoPhone, types.DefaultUserServer)
	joaoLID   = types.NewJID("176543210987654", types.HiddenUserServer)
	pedroLID  = types.NewJID("165432109876543", types.HiddenUserServer)
	familyJID = types.NewJID("120363012345678901", types.GroupServer)
	morning   = time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
)

func message(info types.MessageInfo, raw *waE2E.Message) *events.Message {
	return (&events.Message{Info: info, RawMessage: raw}).UnwrapRaw()
}

func text(body string) *waE2E.Message {
	return &waE2E.Message{Conversation: proto.String(body)}
}

func reply(body, quotedID string, quotedSender types.JID) *waE2E.Message {
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text: proto.String(body),
		ContextInfo: &waE2E.ContextInfo{
			StanzaID:      proto.String(quotedID),
			Participant:   proto.String(quotedSender.String()),
			QuotedMessage: &waE2E.Message{Conversation: proto.String("quoted")},
		},
	}}
}

func fromPhone(destination types.JID, body string) *waE2E.Message {
	return &waE2E.Message{DeviceSentMessage: &waE2E.DeviceSentMessage{
		DestinationJID: proto.String(destination.String()),
		Message:        text(body),
	}}
}

func direct(peer types.JID, id, pushName string, at time.Time) types.MessageInfo {
	return types.MessageInfo{
		MessageSource: types.MessageSource{Chat: peer, Sender: peer, AddressingMode: types.AddressingModePN},
		ID:            id,
		PushName:      pushName,
		Timestamp:     at,
		Type:          "text",
	}
}

func directFromLID(lid, pn types.JID, id, pushName string, at time.Time) types.MessageInfo {
	return types.MessageInfo{
		MessageSource: types.MessageSource{Chat: lid, Sender: lid, SenderAlt: pn, AddressingMode: types.AddressingModeLID},
		ID:            id,
		PushName:      pushName,
		Timestamp:     at,
		Type:          "text",
	}
}

func ownFromPhone(chat, recipientAlt types.JID, id string, at time.Time) types.MessageInfo {
	return types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: workLID, IsFromMe: true, RecipientAlt: recipientAlt},
		ID:            id,
		PushName:      "Alex",
		Timestamp:     at,
		Type:          "text",
	}
}

func inGroup(group, sender, senderAlt types.JID, id, pushName string, at time.Time) types.MessageInfo {
	mode := types.AddressingModePN
	if sender.Server == types.HiddenUserServer {
		mode = types.AddressingModeLID
	}
	return types.MessageInfo{
		MessageSource: types.MessageSource{Chat: group, Sender: sender, SenderAlt: senderAlt, IsGroup: true, AddressingMode: mode},
		ID:            id,
		PushName:      pushName,
		Timestamp:     at,
		Type:          "text",
	}
}

func assertJSON(t *testing.T, got any, want string) {
	t.Helper()
	gotBody, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotBody, &gotValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(want), &wantValue); err != nil {
		t.Fatalf("the expected JSON is not valid: %v", err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("got  %s\nwant %s", gotBody, want)
	}
}

func decode(t *testing.T, value any, target any) {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		t.Fatal(err)
	}
}

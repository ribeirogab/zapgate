package webhooks

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"time"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const fetchTool = "get_messages"

type receivedData struct {
	Account      string        `json:"account"`
	AccountPhone string        `json:"account_phone"`
	Chat         chatRef       `json:"chat"`
	Messages     []messageView `json:"messages"`
	Truncated    bool          `json:"truncated,omitempty"`
	Fetch        fetchCall     `json:"fetch"`
}

type chatRef struct {
	JID   string `json:"jid"`
	Kind  string `json:"kind"`
	Name  string `json:"name,omitempty"`
	Phone string `json:"phone,omitempty"`
}

type personRef struct {
	JID   string `json:"jid"`
	Phone string `json:"phone,omitempty"`
	Name  string `json:"name,omitempty"`
}

type messageView struct {
	ID         string             `json:"id"`
	Kind       string             `json:"kind"`
	FromMe     bool               `json:"from_me"`
	SentAt     string             `json:"sent_at"`
	Sender     personRef          `json:"sender"`
	Media      *mediaView         `json:"media,omitempty"`
	ThirdParty *thirdPartyContent `json:"third_party,omitempty"`
}

type mediaView struct {
	Mime      string `json:"mime,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Seconds   int    `json:"seconds,omitempty"`
	VoiceNote bool   `json:"voice_note,omitempty"`
}

type fetchCall struct {
	Tool      string         `json:"tool"`
	Arguments fetchArguments `json:"arguments"`
}

type fetchArguments struct {
	Account string `json:"account"`
	Chat    string `json:"chat"`
	After   string `json:"after,omitempty"`
}

func (d *Dispatcher) receivedData(ctx context.Context, a attempt) (receivedData, error) {
	chat, err := d.deps.Store.Chat(ctx, a.delivery.AccountID, a.delivery.ChatJID)
	if err != nil {
		return receivedData{}, err
	}
	messages, err := d.deliveryMessages(ctx, a.delivery)
	if err != nil {
		return receivedData{}, err
	}
	after, err := d.newestBefore(ctx, a.delivery, messages)
	if err != nil {
		return receivedData{}, err
	}
	data := receivedData{
		Account:      a.account.Name,
		AccountPhone: a.account.Phone,
		Chat:         chatRefOf(chat),
		Messages:     minimalMessages(messages),
		Fetch:        fetchCall{Tool: fetchTool, Arguments: fetchArguments{Account: a.account.Name, Chat: chat.JID, After: after}},
	}
	if a.webhook.Content == ContentFull {
		data.addThirdParty(chat, messages)
	}
	return data, nil
}

func (d *Dispatcher) deliveryMessages(ctx context.Context, delivery store.Delivery) ([]store.Message, error) {
	journal, err := d.deps.Store.DeliveryEvents(ctx, delivery)
	if err != nil {
		return nil, err
	}
	var messages []store.Message
	for _, event := range journal {
		msg, err := d.deps.Store.Message(ctx, delivery.AccountID, delivery.ChatJID, event.MessageID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		messages = append(messages, msg)
	}
	slices.SortFunc(messages, inChatOrder)
	return messages, nil
}

func (d *Dispatcher) newestBefore(ctx context.Context, delivery store.Delivery, messages []store.Message) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}
	first := messages[0]
	older, err := d.deps.Store.Messages(ctx, delivery.AccountID, delivery.ChatJID, store.MessageQuery{
		Before: &store.MessageBound{SentAt: first.SentAt, ID: first.ID},
		Limit:  1,
	})
	if err != nil || len(older) == 0 {
		return "", err
	}
	return older[0].ID, nil
}

func inChatOrder(a, b store.Message) int {
	return cmp.Or(a.SentAt.Compare(b.SentAt), cmp.Compare(a.ID, b.ID))
}

func chatRefOf(chat store.Chat) chatRef {
	return chatRef{JID: chat.JID, Kind: chat.Kind, Phone: phoneOf(chat.JID)}
}

func minimalMessages(messages []store.Message) []messageView {
	views := make([]messageView, 0, len(messages))
	for _, msg := range messages {
		views = append(views, minimalMessage(msg))
	}
	return views
}

func minimalMessage(msg store.Message) messageView {
	view := messageView{
		ID:     msg.ID,
		Kind:   msg.Kind,
		FromMe: msg.FromMe,
		SentAt: msg.SentAt.UTC().Format(time.RFC3339),
		Sender: personRef{JID: msg.SenderJID, Phone: phoneOf(msg.SenderJID)},
	}
	if media := msg.Media; media != nil {
		view.Media = &mediaView{Mime: media.Mime, Size: media.Size, Seconds: media.Seconds, VoiceNote: media.VoiceNote}
	}
	return view
}

func phoneOf(jid string) string {
	parsed, err := types.ParseJID(jid)
	if err != nil {
		return ""
	}
	return whatsapp.Phone(parsed)
}

func receivedBody(a attempt, data receivedData) (Body, error) {
	body := newBody(EventMessageReceived, a.at, data)
	if a.webhook.Content == ContentFull {
		body.Notice = thirdPartyNotice
	}
	return withinCap(body, data)
}

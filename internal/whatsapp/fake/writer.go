package fake

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/clock"
)

var errNotConnected = errors.New("websocket not connected")

type SentMessage struct {
	To        types.JID
	Message   *waE2E.Message
	ID        types.MessageID
	Timestamp time.Time
}

type Receipt struct {
	Chat   types.JID
	Sender types.JID
	IDs    []types.MessageID
	At     time.Time
}

type outbox struct {
	uploads  []Upload
	sent     []SentMessage
	receipts []Receipt
	failure  error
}

func (w *World) SetClock(c clock.Clock) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.clock = c
}

func (w *World) Sent(phone string) []SentMessage {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.outboxOf(phone).sent)
}

func (w *World) Receipts(phone string) []Receipt {
	w.mu.Lock()
	defer w.mu.Unlock()
	return slices.Clone(w.outboxOf(phone).receipts)
}

func (w *World) FailSends(phone string, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.outboxOf(phone).failure = err
}

func (w *World) outboxOf(phone string) *outbox {
	if w.outboxes == nil {
		w.outboxes = map[string]*outbox{}
	}
	box, ok := w.outboxes[phone]
	if !ok {
		box = &outbox{}
		w.outboxes[phone] = box
	}
	return box
}

func (w *World) currentTime() time.Time {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.now()
}

func (w *World) now() time.Time {
	if w.clock == nil {
		return time.Now()
	}
	return w.clock.Now()
}

func (c *Client) onlineDevice() (*device, error) {
	dev, err := c.linkedDevice()
	if err != nil {
		return nil, err
	}
	if !dev.online {
		return nil, errNotConnected
	}
	return dev, nil
}

func (c *Client) SendMessage(_ context.Context, to types.JID, msg *waE2E.Message) (whatsmeow.SendResponse, error) {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.onlineDevice()
	if err != nil {
		return whatsmeow.SendResponse{}, err
	}
	box := c.world.outboxOf(dev.phone)
	if box.failure != nil {
		return whatsmeow.SendResponse{}, box.failure
	}
	sent := SentMessage{To: to, Message: msg, ID: fmt.Sprintf("3EB0FAKE%04d", len(box.sent)+1), Timestamp: c.world.now().Truncate(time.Second)}
	box.sent = append(box.sent, sent)
	return whatsmeow.SendResponse{ID: sent.ID, Timestamp: sent.Timestamp, Chat: c.world.addressOf(to), Sender: dev.jid.ToNonAD()}, nil
}

func (w *World) addressOf(to types.JID) types.JID {
	for lid, pn := range w.lids {
		if pn == to.ToNonAD() {
			return lid
		}
	}
	return to
}

func (c *Client) MarkRead(_ context.Context, ids []types.MessageID, at time.Time, chat, sender types.JID) error {
	c.world.mu.Lock()
	defer c.world.mu.Unlock()
	dev, err := c.onlineDevice()
	if err != nil {
		return err
	}
	box := c.world.outboxOf(dev.phone)
	box.receipts = append(box.receipts, Receipt{Chat: chat, Sender: sender, IDs: slices.Clone(ids), At: at})
	return nil
}

func (c *Client) BuildEdit(chat types.JID, id types.MessageID, content *waE2E.Message) *waE2E.Message {
	return &waE2E.Message{EditedMessage: &waE2E.FutureProofMessage{Message: &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Key:           &waCommon.MessageKey{FromMe: proto.Bool(true), ID: proto.String(id), RemoteJID: proto.String(chat.String())},
		Type:          waE2E.ProtocolMessage_MESSAGE_EDIT.Enum(),
		EditedMessage: content,
		TimestampMS:   proto.Int64(c.world.currentTime().UnixMilli()),
	}}}}
}

func (c *Client) BuildRevoke(chat, sender types.JID, id types.MessageID) *waE2E.Message {
	return &waE2E.Message{ProtocolMessage: &waE2E.ProtocolMessage{
		Type: waE2E.ProtocolMessage_REVOKE.Enum(),
		Key:  c.messageKey(chat, sender, id),
	}}
}

func (c *Client) BuildReaction(chat, sender types.JID, id types.MessageID, emoji string) *waE2E.Message {
	return &waE2E.Message{ReactionMessage: &waE2E.ReactionMessage{
		Key:               c.messageKey(chat, sender, id),
		Text:              proto.String(emoji),
		SenderTimestampMS: proto.Int64(c.world.currentTime().UnixMilli()),
	}}
}

func (c *Client) messageKey(chat, sender types.JID, id types.MessageID) *waCommon.MessageKey {
	key := &waCommon.MessageKey{FromMe: proto.Bool(true), ID: proto.String(id), RemoteJID: proto.String(chat.String())}
	own, _ := types.ParseJID(c.device())
	if !sender.IsEmpty() && sender.User != own.User {
		key.FromMe = proto.Bool(false)
		if chat.Server != types.DefaultUserServer && chat.Server != types.HiddenUserServer {
			key.Participant = proto.String(sender.ToNonAD().String())
		}
	}
	return key
}

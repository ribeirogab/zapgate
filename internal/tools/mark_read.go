package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	markReadName      = "mark_read"
	maxMarkedMessages = 200
	minMarkedMessages = 1
)

type markReadInput struct {
	Account    string   `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat       string   `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	MessageIDs []string `json:"message_ids" jsonschema:"the ids of received messages of the chat, from get_messages"`
}

type markReadOutput struct {
	accountFields
	Chat        chatRef  `json:"chat"`
	MessageIDs  []string `json:"message_ids" jsonschema:"the messages now marked as read"`
	UnreadCount int      `json:"unread_count" jsonschema:"the received messages of the chat that are still unread"`
}

type receipt struct {
	sender types.JID
	ids    []types.MessageID
}

func (r *Registry) markRead() Tool {
	return Tool{
		Name:         markReadName,
		Class:        MarkRead,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			schema := withText(accountSchema[markReadInput](scope.AccountNames()))
			ids := schema.Properties["message_ids"]
			ids.MinItems = jsonschema.Ptr(minMarkedMessages)
			ids.MaxItems = jsonschema.Ptr(maxMarkedMessages)
			mcp.AddTool(srv, &mcp.Tool{
				Name:        markReadName,
				Title:       "Mark messages as read",
				Description: "Marks received messages of one chat as read on WhatsApp, so their senders see the blue ticks. No approval is asked. Reading with the other tools never marks anything as read.",
				Annotations: &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(false), IdempotentHint: true, OpenWorldHint: jsonschema.Ptr(true)},
				InputSchema: schema,
			}, r.handleMarkRead)
		},
	}
}

func (r *Registry) handleMarkRead(ctx context.Context, req *mcp.CallToolRequest, in markReadInput) (*mcp.CallToolResult, markReadOutput, error) {
	scope, err := r.scopeFor(ctx, req)
	if err != nil {
		return nil, markReadOutput{}, err
	}
	w, err := r.writeTarget(ctx, scope, in.Account)
	if err != nil {
		return nil, markReadOutput{}, err
	}
	chat, err := r.knownChat(ctx, w, in.Chat)
	if err != nil {
		return nil, markReadOutput{}, err
	}
	receipts, newest, err := r.receipts(ctx, w, chat, in.MessageIDs)
	if err != nil {
		return nil, markReadOutput{}, err
	}
	jid, _ := types.ParseJID(chat.JID)
	at := r.deps.Clock.Now()
	for _, rc := range receipts {
		if err := w.client.MarkRead(ctx, rc.ids, at, jid, rc.sender); err != nil {
			return nil, markReadOutput{}, fmt.Errorf("WhatsApp did not accept the read receipt: %w", err)
		}
	}
	if err := r.deps.Store.MarkChatReadUntil(ctx, w.account.ID, chat.JID, newest); err != nil {
		return nil, markReadOutput{}, err
	}
	if chat, err = r.deps.Store.Chat(ctx, w.account.ID, chat.JID); err != nil {
		return nil, markReadOutput{}, err
	}
	return nil, markReadOutput{accountFields: fieldsOf(w.account), Chat: chatRefOf(chat), MessageIDs: in.MessageIDs, UnreadCount: chat.UnreadCount}, nil
}

func (r *Registry) receipts(ctx context.Context, w writeTarget, chat store.Chat, ids []string) ([]receipt, time.Time, error) {
	var (
		receipts []receipt
		newest   time.Time
	)
	bySender := map[types.JID]int{}
	for _, id := range ids {
		msg, err := r.storedMessage(ctx, w, chat, id)
		if err != nil {
			return nil, time.Time{}, err
		}
		if msg.FromMe {
			return nil, time.Time{}, fmt.Errorf("the message %s was sent by the account %s; mark_read only marks received messages", msg.ID, w.account.Name)
		}
		if msg.SentAt.After(newest) {
			newest = msg.SentAt
		}
		sender := receiptSender(chat, msg)
		index, ok := bySender[sender]
		if !ok {
			index = len(receipts)
			bySender[sender] = index
			receipts = append(receipts, receipt{sender: sender})
		}
		receipts[index].ids = append(receipts[index].ids, msg.ID)
	}
	return receipts, newest, nil
}

func receiptSender(chat store.Chat, msg store.Message) types.JID {
	if chat.Kind != store.ChatGroup {
		return types.EmptyJID
	}
	sender, err := types.ParseJID(msg.SenderJID)
	if err != nil {
		return types.EmptyJID
	}
	return sender
}

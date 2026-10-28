package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"
)

const (
	deleteMessageName = "delete_message"
	deleteWindow      = 48 * time.Hour
)

type deleteMessageInput struct {
	Account   string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat      string `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	MessageID string `json:"message_id" jsonschema:"the id of a message the account sent less than about 2 days ago"`
}

type deleteMessageOutput struct {
	writeResult
	MessageID string `json:"message_id" jsonschema:"the id of the deleted message"`
	RevokedAt string `json:"revoked_at" jsonschema:"the WhatsApp server time of the delete, RFC 3339 in UTC"`
}

func (r *Registry) deleteMessage() Tool {
	return Tool{
		Name:         deleteMessageName,
		Class:        Write,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        deleteMessageName,
				Title:       "Delete a sent message for everyone",
				Description: "Deletes for everyone a message the account sent, up to about 2 days after sending it. The server keeps the text and marks revoked_at. When the client supports elicitation, the owner sees the account, the chat and the message and must accept first.",
				Annotations: &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(true), OpenWorldHint: jsonschema.Ptr(true)},
				InputSchema: withText(accountSchema[deleteMessageInput](scope.AccountNames()), "message_id"),
			}, r.handleDeleteMessage)
		},
	}
}

func (r *Registry) handleDeleteMessage(ctx context.Context, req *mcp.CallToolRequest, in deleteMessageInput) (*mcp.CallToolResult, deleteMessageOutput, error) {
	t, err := r.target(ctx, req, in.Account, in.Chat, in.MessageID)
	if err != nil {
		return nil, deleteMessageOutput{}, err
	}
	if err := r.deletable(t); err != nil {
		return nil, deleteMessageOutput{}, err
	}
	question := t.describe("Delete this WhatsApp message for everyone?")
	pending, verdict, err := r.approvals.gate(ctx, req, t.action(deleteMessageName, question, "the message was not deleted", t.chat.JID, t.message.ID))
	if pending != nil || err != nil {
		return pending, deleteMessageOutput{}, err
	}
	ctx = context.WithoutCancel(ctx)
	chat := t.chatJID()
	resp, err := t.client.SendMessage(ctx, chat, t.client.BuildRevoke(chat, types.EmptyJID, t.message.ID))
	if err != nil {
		return nil, deleteMessageOutput{}, fmt.Errorf("WhatsApp did not accept the delete, so the message was not deleted: %w", err)
	}
	r.logUnsaved(t.writeTarget, deleteMessageName, r.deps.Store.RevokeMessage(ctx, t.account.ID, t.chat.JID, t.message.ID, t.message.SenderJID, false, resp.Timestamp))
	return nil, deleteMessageOutput{writeResult: t.result(t.chat, verdict), MessageID: t.message.ID, RevokedAt: timestamp(resp.Timestamp)}, nil
}

func (r *Registry) deletable(t target) error {
	if err := t.ownMessage("deleted for everyone"); err != nil {
		return err
	}
	if r.deps.Clock.Now().Sub(t.message.SentAt) > deleteWindow {
		return fmt.Errorf("the message %s was sent at %s; WhatsApp only allows deleting a message for everyone within about 2 days of sending it, so it can no longer be deleted for everyone", t.message.ID, timestamp(t.message.SentAt))
	}
	return nil
}

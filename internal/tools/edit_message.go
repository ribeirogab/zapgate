package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	editMessageName = "edit_message"
	editWindow      = 15 * time.Minute
)

type editMessageInput struct {
	Account   string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat      string `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	MessageID string `json:"message_id" jsonschema:"the id of a text message the account sent less than 15 minutes ago"`
	Text      string `json:"text" jsonschema:"the exact new text"`
}

type editMessageOutput struct {
	writeResult
	MessageID string `json:"message_id" jsonschema:"the id of the edited message"`
	EditedAt  string `json:"edited_at" jsonschema:"the WhatsApp server time of the edit, RFC 3339 in UTC"`
}

func (r *Registry) editMessage() Tool {
	return Tool{
		Name:         editMessageName,
		Class:        Write,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        editMessageName,
				Title:       "Edit a sent message",
				Description: "Replaces the text of a text message the account sent, up to 15 minutes after sending it. When the client supports elicitation, the owner sees the account, the chat, the old message and the new text and must accept first.",
				Annotations: &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(true), OpenWorldHint: jsonschema.Ptr(true)},
				InputSchema: withText(accountSchema[editMessageInput](scope.AccountNames()), "message_id", "text"),
			}, r.handleEditMessage)
		},
	}
}

func (r *Registry) handleEditMessage(ctx context.Context, req *mcp.CallToolRequest, in editMessageInput) (*mcp.CallToolResult, editMessageOutput, error) {
	t, err := r.target(ctx, req, in.Account, in.Chat, in.MessageID)
	if err != nil {
		return nil, editMessageOutput{}, err
	}
	if err := r.editable(t); err != nil {
		return nil, editMessageOutput{}, err
	}
	question := t.describe("Edit this WhatsApp message?") + "\n\nNew text:\n" + in.Text
	pending, verdict, err := r.approvals.gate(ctx, req, t.action(editMessageName, question, "the message was not edited", t.chat.JID, t.message.ID, in.Text))
	if pending != nil || err != nil {
		return pending, editMessageOutput{}, err
	}
	ctx = context.WithoutCancel(ctx)
	chat := t.chatJID()
	resp, err := t.client.SendMessage(ctx, chat, t.client.BuildEdit(chat, t.message.ID, &waE2E.Message{Conversation: proto.String(in.Text)}))
	if err != nil {
		return nil, editMessageOutput{}, fmt.Errorf("WhatsApp did not accept the edit, so the message was not edited: %w", err)
	}
	r.logUnsaved(t.writeTarget, editMessageName, r.deps.Store.EditMessage(ctx, t.account.ID, t.chat.JID, t.message.ID, t.message.SenderJID, in.Text, resp.Timestamp))
	return nil, editMessageOutput{writeResult: t.result(t.chat, verdict), MessageID: t.message.ID, EditedAt: timestamp(resp.Timestamp)}, nil
}

func (r *Registry) editable(t target) error {
	if err := t.ownMessage("edited"); err != nil {
		return err
	}
	if r.deps.Clock.Now().Sub(t.message.SentAt) > editWindow {
		return fmt.Errorf("the message %s was sent at %s; WhatsApp only allows editing a message within 15 minutes of sending it, so it can no longer be edited", t.message.ID, timestamp(t.message.SentAt))
	}
	if t.message.Kind != store.MessageText {
		return fmt.Errorf("the message %s is a %s message; edit_message only edits text messages", t.message.ID, t.message.Kind)
	}
	return nil
}

package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"

	"github.com/ribeirogab/zapgate/internal/store"
)

const sendMessageName = "send_message"

type sendMessageInput struct {
	Account string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	To      string `json:"to" jsonschema:"the phone number with the country code, for example +5511988887777, or a chat JID from list_chats"`
	Text    string `json:"text" jsonschema:"the exact text to send"`
	ReplyTo string `json:"reply_to,omitempty" jsonschema:"the id of a message of the same chat to reply to, from get_messages"`
}

type sendMessageOutput struct {
	writeResult
	MessageID string `json:"message_id" jsonschema:"the id of the sent message; pass it to edit_message, delete_message or react"`
	SentAt    string `json:"sent_at" jsonschema:"the WhatsApp server time, RFC 3339 in UTC"`
}

func (r *Registry) sendMessage() Tool {
	return Tool{
		Name:         sendMessageName,
		Class:        Write,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        sendMessageName,
				Title:       "Send a text message",
				Description: "Sends a text message from one account to a person or a group, optionally as a reply. When the client supports elicitation, the owner sees the account, the destination and the exact text and must accept first. The tool answers after WhatsApp confirms the message.",
				Annotations: &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(true), OpenWorldHint: jsonschema.Ptr(true)},
				InputSchema: withText(accountSchema[sendMessageInput](scope.AccountNames()), "text"),
			}, r.handleSendMessage)
		},
	}
}

func (r *Registry) handleSendMessage(ctx context.Context, req *mcp.CallToolRequest, in sendMessageInput) (*mcp.CallToolResult, sendMessageOutput, error) {
	scope, err := r.scopeFor(ctx, req)
	if err != nil {
		return nil, sendMessageOutput{}, err
	}
	s, err := r.sendTarget(ctx, scope, in.Account, in.To, in.ReplyTo)
	if err != nil {
		return nil, sendMessageOutput{}, err
	}
	pending, verdict, err := r.approvals.gate(ctx, req, s.action(sendMessageName, s.question("Send this WhatsApp message?")+"\nText:\n"+in.Text, "the message was not sent", s.to.String(), in.Text, in.ReplyTo))
	if pending != nil || err != nil {
		return pending, sendMessageOutput{}, err
	}
	ctx = context.WithoutCancel(ctx)
	resp, err := s.client.SendMessage(ctx, s.to, s.textMessage(in.Text))
	if err != nil {
		return nil, sendMessageOutput{}, fmt.Errorf("WhatsApp did not accept the message, so it was not sent: %w", err)
	}
	r.logUnsaved(s.writeTarget, sendMessageName, r.deps.Store.SaveMessage(ctx, s.account.ID, s.chat, s.sentMessage(resp, store.MessageText, in.Text)))
	return nil, sendMessageOutput{writeResult: s.result(s.chat, verdict), MessageID: resp.ID, SentAt: timestamp(resp.Timestamp)}, nil
}

func (s sendTarget) textMessage(text string) *waE2E.Message {
	if s.quoted == nil {
		return &waE2E.Message{Conversation: proto.String(text)}
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{Text: proto.String(text), ContextInfo: s.quoteContext()}}
}

func withText(schema *jsonschema.Schema, names ...string) *jsonschema.Schema {
	for _, name := range names {
		schema.Properties[name].MinLength = jsonschema.Ptr(1)
	}
	return schema
}

package tools

import (
	"context"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	reactName      = "react"
	maxEmojiLength = 32
)

type reactInput struct {
	Account   string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat      string `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	MessageID string `json:"message_id" jsonschema:"the id of the message to react to, from get_messages"`
	Emoji     string `json:"emoji" jsonschema:"one emoji, for example 👍; an empty string removes the reaction of the account"`
}

type reactOutput struct {
	writeResult
	MessageID string `json:"message_id" jsonschema:"the id of the message that got the reaction"`
	Emoji     string `json:"emoji" jsonschema:"the reaction now on the message; empty when it was removed"`
}

func (r *Registry) react() Tool {
	return Tool{
		Name:         reactName,
		Class:        Write,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			schema := accountSchema[reactInput](scope.AccountNames())
			schema.Properties["emoji"].MaxLength = jsonschema.Ptr(maxEmojiLength)
			mcp.AddTool(srv, &mcp.Tool{
				Name:        reactName,
				Title:       "React to a message",
				Description: "Adds, changes or removes (empty emoji) the reaction of one account to a stored message. When the client supports elicitation, the owner sees the account, the chat, the message and the emoji and must accept first.",
				Annotations: &mcp.ToolAnnotations{DestructiveHint: jsonschema.Ptr(true), OpenWorldHint: jsonschema.Ptr(true)},
				InputSchema: withText(schema, "message_id"),
			}, r.handleReact)
		},
	}
}

func (r *Registry) handleReact(ctx context.Context, req *mcp.CallToolRequest, in reactInput) (*mcp.CallToolResult, reactOutput, error) {
	t, err := r.target(ctx, req, in.Account, in.Chat, in.MessageID)
	if err != nil {
		return nil, reactOutput{}, err
	}
	question := t.describe("React to this WhatsApp message?") + "\n\nReaction: " + in.Emoji
	if in.Emoji == "" {
		question = t.describe("Remove your reaction from this WhatsApp message?")
	}
	pending, verdict, err := r.approvals.gate(ctx, req, t.action(reactName, question, "the reaction was not changed", t.chat.JID, t.message.ID, in.Emoji))
	if pending != nil || err != nil {
		return pending, reactOutput{}, err
	}
	ctx = context.WithoutCancel(ctx)
	chat := t.chatJID()
	resp, err := t.client.SendMessage(ctx, chat, t.client.BuildReaction(chat, t.senderOf(t.message), t.message.ID, in.Emoji))
	if err != nil {
		return nil, reactOutput{}, fmt.Errorf("WhatsApp did not accept the reaction, so it was not changed: %w", err)
	}
	reaction := store.Reaction{
		ChatJID:    t.chat.JID,
		MessageID:  t.message.ID,
		SenderJID:  t.self().String(),
		SenderName: t.account.ProfileName,
		FromMe:     true,
		Emoji:      in.Emoji,
		ReactedAt:  resp.Timestamp,
	}
	r.logUnsaved(t.writeTarget, reactName, r.deps.Store.React(ctx, t.account.ID, reaction))
	return nil, reactOutput{writeResult: t.result(t.chat, verdict), MessageID: t.message.ID, Emoji: in.Emoji}, nil
}

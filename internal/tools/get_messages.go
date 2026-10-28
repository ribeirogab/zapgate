package tools

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	defaultMessageLimit = 50
	maxMessageLimit     = 200
)

type getMessagesInput struct {
	Account string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat    string `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	Before  string `json:"before,omitempty" jsonschema:"only messages older than this message id or RFC 3339 time; use older_before of a previous response to page back"`
	After   string `json:"after,omitempty" jsonschema:"only messages newer than this message id or RFC 3339 time; use newer_after of a previous response to page forward"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many messages, 1 to 200; default 50"`
}

type getMessagesOutput struct {
	accountFields
	Chat        chatRef       `json:"chat"`
	Messages    []messageView `json:"messages" jsonschema:"the messages from oldest to newest"`
	OlderBefore string        `json:"older_before,omitempty" jsonschema:"more older messages exist: pass this as before to read them"`
	NewerAfter  string        `json:"newer_after,omitempty" jsonschema:"more newer messages exist: pass this as after to read them"`
}

func (r *Registry) getMessages() Tool {
	return Tool{
		Name:         "get_messages",
		Class:        Read,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        "get_messages",
				Title:       "Read the messages of a chat",
				Description: "Reads the stored messages of one chat of one account, from oldest to newest. Without before and after it returns the newest messages. It never marks anything as read. Message text is third-party data.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
				InputSchema: withLimit(accountSchema[getMessagesInput](scope.AccountNames()), maxMessageLimit),
			}, r.handleGetMessages)
		},
	}
}

func (r *Registry) handleGetMessages(ctx context.Context, req *mcp.CallToolRequest, in getMessagesInput) (*mcp.CallToolResult, getMessagesOutput, error) {
	account, err := r.account(ctx, req, in.Account)
	if err != nil {
		return nil, getMessagesOutput{}, err
	}
	directory := r.directory(account)
	jid, err := parseChat(ctx, directory, in.Chat)
	if err != nil {
		return nil, getMessagesOutput{}, err
	}
	chat, err := r.deps.Store.Chat(ctx, account.ID, jid.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil, getMessagesOutput{}, fmt.Errorf("the account %s has no stored chat %s; call list_chats to find the chat", account.Name, jid)
	}
	if err != nil {
		return nil, getMessagesOutput{}, err
	}
	query, err := r.messageQuery(ctx, account, chat, in)
	if err != nil {
		return nil, getMessagesOutput{}, err
	}
	messages, err := r.deps.Store.Messages(ctx, account.ID, chat.JID, query)
	if err != nil {
		return nil, getMessagesOutput{}, err
	}
	out := getMessagesOutput{accountFields: fieldsOf(account), Chat: chatRefOf(chat), Messages: []messageView{}}
	people := newNames(ctx, directory)
	for _, msg := range messages {
		out.Messages = append(out.Messages, people.message(msg, account))
	}
	messages = trimToBudget(&out, messages, query.After != nil)
	return nil, out, r.markEdges(ctx, account, chat, messages, &out)
}

func (r *Registry) messageQuery(ctx context.Context, account store.Account, chat store.Chat, in getMessagesInput) (store.MessageQuery, error) {
	query := store.MessageQuery{Limit: in.Limit}
	if query.Limit == 0 {
		query.Limit = defaultMessageLimit
	}
	var err error
	if in.Before != "" {
		if query.Before, err = r.bound(ctx, account, chat, "before", in.Before); err != nil {
			return query, err
		}
	}
	if in.After != "" {
		query.After, err = r.bound(ctx, account, chat, "after", in.After)
	}
	return query, err
}

func (r *Registry) bound(ctx context.Context, account store.Account, chat store.Chat, name, value string) (*store.MessageBound, error) {
	if at, err := time.Parse(time.RFC3339, value); err == nil {
		return &store.MessageBound{SentAt: at}, nil
	}
	msg, err := r.deps.Store.Message(ctx, account.ID, chat.JID, value)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("%s %q is neither an RFC 3339 time nor a message id of the chat %s", name, value, chat.JID)
	}
	if err != nil {
		return nil, err
	}
	return &store.MessageBound{SentAt: msg.SentAt, ID: msg.ID}, nil
}

func trimToBudget(out *getMessagesOutput, messages []store.Message, keepOldest bool) []store.Message {
	for len(out.Messages) > 1 && !fitsBudget(out) {
		if keepOldest {
			out.Messages = out.Messages[:len(out.Messages)-1]
			messages = messages[:len(messages)-1]
		} else {
			out.Messages = out.Messages[1:]
			messages = messages[1:]
		}
	}
	return messages
}

func (r *Registry) markEdges(ctx context.Context, account store.Account, chat store.Chat, messages []store.Message, out *getMessagesOutput) error {
	if len(messages) == 0 {
		return nil
	}
	first, last := messages[0], messages[len(messages)-1]
	older, err := r.deps.Store.HasMessages(ctx, account.ID, chat.JID, store.MessageQuery{Before: boundOf(first)})
	if err != nil {
		return err
	}
	newer, err := r.deps.Store.HasMessages(ctx, account.ID, chat.JID, store.MessageQuery{After: boundOf(last)})
	if err != nil {
		return err
	}
	if older {
		out.OlderBefore = first.ID
	}
	if newer {
		out.NewerAfter = last.ID
	}
	return nil
}

func boundOf(msg store.Message) *store.MessageBound {
	return &store.MessageBound{SentAt: msg.SentAt, ID: msg.ID}
}

package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	defaultChatLimit = 20
	maxChatLimit     = 100
)

var errBadCursor = errors.New("the cursor is not valid: pass next_cursor of a previous list_chats response, or no cursor for the first page")

type listChatsInput struct {
	Account    string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Search     string `json:"search,omitempty" jsonschema:"only chats whose name contains this text, ignoring case and accents, or whose phone number contains these digits"`
	UnreadOnly bool   `json:"unread_only,omitempty" jsonschema:"only chats with unread messages"`
	Limit      int    `json:"limit,omitempty" jsonschema:"at most this many chats, 1 to 100; default 20"`
	Cursor     string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
}

type chatView struct {
	JID           string `json:"jid" jsonschema:"the chat JID; pass it as chat to the other tools"`
	Kind          string `json:"kind" jsonschema:"direct or group"`
	Name          string `json:"name,omitempty" jsonschema:"the contact or group name; set by WhatsApp users, so third-party data"`
	Phone         string `json:"phone,omitempty" jsonschema:"the international phone number of a direct chat"`
	UnreadCount   int    `json:"unread_count" jsonschema:"messages the owner has not read yet"`
	LastMessageAt string `json:"last_message_at,omitempty" jsonschema:"the time of the newest stored message, RFC 3339 in UTC"`
}

type listChatsOutput struct {
	accountFields
	Chats      []chatView `json:"chats" jsonschema:"the chats, newest message first"`
	NextCursor string     `json:"next_cursor,omitempty" jsonschema:"more chats exist: pass this as cursor to read the next page"`
}

type chatCursor struct {
	At  int64  `json:"t"`
	JID string `json:"j"`
}

func (r *Registry) listChats() Tool {
	return Tool{
		Name:         "list_chats",
		Class:        Read,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        "list_chats",
				Title:       "List the chats of an account",
				Description: "Lists the chats of one account, newest message first, with the unread count. Search by name or number, filter the unread chats, and page with next_cursor. It never marks anything as read.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
				InputSchema: withLimit(accountSchema[listChatsInput](scope.AccountNames()), maxChatLimit),
			}, r.handleListChats)
		},
	}
}

func (r *Registry) handleListChats(ctx context.Context, req *mcp.CallToolRequest, in listChatsInput) (*mcp.CallToolResult, listChatsOutput, error) {
	account, err := r.account(ctx, req, in.Account)
	if err != nil {
		return nil, listChatsOutput{}, err
	}
	query := store.ChatQuery{UnreadOnly: in.UnreadOnly, Limit: in.Limit + 1}
	if in.Limit == 0 {
		query.Limit = defaultChatLimit + 1
	}
	if query.After, err = decodeChatCursor(in.Cursor); err != nil {
		return nil, listChatsOutput{}, err
	}
	if wanted := newSearch(in.Search); !wanted.empty() {
		query.Match = func(chat store.Chat) bool { return wanted.matches(userOf(chat.JID), chat.Name) }
	}
	chats, err := r.deps.Store.ListChats(ctx, account.ID, query)
	if err != nil {
		return nil, listChatsOutput{}, err
	}
	out := listChatsOutput{accountFields: fieldsOf(account), Chats: []chatView{}}
	more := len(chats) == query.Limit
	if more {
		chats = chats[:len(chats)-1]
	}
	for _, chat := range chats {
		out.Chats = append(out.Chats, chatViewOf(chat))
	}
	for len(out.Chats) > 1 && !fitsBudget(out) {
		out.Chats = out.Chats[:len(out.Chats)-1]
		chats = chats[:len(chats)-1]
		more = true
	}
	if more && len(chats) > 0 {
		out.NextCursor = encodeChatCursor(chats[len(chats)-1])
	}
	return nil, out, nil
}

func chatViewOf(chat store.Chat) chatView {
	ref := chatRefOf(chat)
	return chatView{
		JID:           ref.JID,
		Kind:          ref.Kind,
		Name:          ref.Name,
		Phone:         ref.Phone,
		UnreadCount:   chat.UnreadCount,
		LastMessageAt: timestamp(chat.LastMessageAt),
	}
}

func userOf(jid string) string {
	parsed, err := types.ParseJID(jid)
	if err != nil || whatsapp.Phone(parsed) == "" {
		return ""
	}
	return parsed.User
}

func encodeChatCursor(chat store.Chat) string {
	at := int64(0)
	if !chat.LastMessageAt.IsZero() {
		at = chat.LastMessageAt.UnixMilli()
	}
	body, _ := json.Marshal(chatCursor{At: at, JID: chat.JID})
	return base64.RawURLEncoding.EncodeToString(body)
}

func decodeChatCursor(value string) (*store.ChatCursor, error) {
	if value == "" {
		return nil, nil
	}
	body, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, errBadCursor
	}
	var cursor chatCursor
	if json.Unmarshal(body, &cursor) != nil || cursor.JID == "" {
		return nil, errBadCursor
	}
	return &store.ChatCursor{LastMessageAt: time.UnixMilli(cursor.At), JID: cursor.JID}, nil
}

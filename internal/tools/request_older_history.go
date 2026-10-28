package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	defaultHistoryCount = 50
	maxHistoryCount     = 50
	historyInterval     = 5 * time.Second
)

type requestOlderHistoryInput struct {
	Account string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Chat    string `json:"chat" jsonschema:"the chat JID from list_chats, or a phone number with the country code"`
	Count   int    `json:"count,omitempty" jsonschema:"ask for at most this many messages older than the oldest stored one, 1 to 50; default 50"`
}

type requestOlderHistoryOutput struct {
	accountFields
	Chat            chatRef `json:"chat"`
	Requested       int     `json:"requested" jsonschema:"how many older messages the server asked the phone for"`
	BeforeMessageID string  `json:"before_message_id" jsonschema:"the oldest stored message of the chat; the phone sends messages older than this one"`
}

type historyPace struct {
	mu   sync.Mutex
	last map[int64]time.Time
}

func (p *historyPace) wait(accountID int64, now time.Time) time.Duration {
	p.mu.Lock()
	defer p.mu.Unlock()
	last, ok := p.last[accountID]
	if !ok {
		return 0
	}
	return max(0, last.Add(historyInterval).Sub(now))
}

func (p *historyPace) record(accountID int64, at time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last == nil {
		p.last = map[int64]time.Time{}
	}
	p.last[accountID] = at
}

func (r *Registry) requestOlderHistory() Tool {
	return Tool{
		Name:         "request_older_history",
		Class:        Read,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			schema := accountSchema[requestOlderHistoryInput](scope.AccountNames())
			prop := schema.Properties["count"]
			prop.Minimum = jsonschema.Ptr(1.0)
			prop.Maximum = jsonschema.Ptr(float64(maxHistoryCount))
			mcp.AddTool(srv, &mcp.Tool{
				Name:  "request_older_history",
				Title: "Ask the phone for older messages of a chat",
				Description: "Asks the account's phone for messages older than the oldest stored message of one chat. " +
					"The phone answers in the background, only while it is online, and may send fewer messages or none. " +
					"Call get_messages for the chat a few seconds later to read what arrived. It sends nothing to the chat and marks nothing as read.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true},
				InputSchema: schema,
			}, r.handleRequestOlderHistory)
		},
	}
}

func (r *Registry) handleRequestOlderHistory(ctx context.Context, req *mcp.CallToolRequest, in requestOlderHistoryInput) (*mcp.CallToolResult, requestOlderHistoryOutput, error) {
	account, err := r.account(ctx, req, in.Account)
	if err != nil {
		return nil, requestOlderHistoryOutput{}, err
	}
	client, ok := r.deps.Clients.Client(account.ID)
	if !ok {
		return nil, requestOlderHistoryOutput{}, fmt.Errorf("the account %s has no WhatsApp session (state %s); the owner must pair it again", account.Name, account.State)
	}
	jid, err := parseChat(ctx, client, in.Chat)
	if err != nil {
		return nil, requestOlderHistoryOutput{}, err
	}
	oldest, err := r.deps.Store.OldestMessage(ctx, account.ID, jid.String())
	if errors.Is(err, store.ErrNotFound) {
		return nil, requestOlderHistoryOutput{}, fmt.Errorf("the account %s has no stored message in the chat %s; WhatsApp needs one stored message to ask for older ones", account.Name, jid)
	}
	if err != nil {
		return nil, requestOlderHistoryOutput{}, err
	}
	if wait := r.historyPace.wait(account.ID, r.deps.Clock.Now()); wait > 0 {
		return nil, requestOlderHistoryOutput{}, fmt.Errorf("the account %s asked its phone for history less than %s ago; wait %d seconds and try again", account.Name, historyInterval, int(math.Ceil(wait.Seconds())))
	}
	count := in.Count
	if count == 0 {
		count = defaultHistoryCount
	}
	info := types.MessageInfo{MessageSource: types.MessageSource{Chat: jid, IsFromMe: oldest.FromMe}, ID: oldest.ID, Timestamp: oldest.SentAt}
	if err := client.RequestOlderHistory(ctx, info, count); err != nil {
		return nil, requestOlderHistoryOutput{}, fmt.Errorf("WhatsApp did not take the request; the phone and the account must be online: %v", err)
	}
	r.historyPace.record(account.ID, r.deps.Clock.Now())
	chat, err := r.deps.Store.Chat(ctx, account.ID, jid.String())
	if err != nil {
		return nil, requestOlderHistoryOutput{}, err
	}
	return nil, requestOlderHistoryOutput{accountFields: fieldsOf(account), Chat: chatRefOf(chat), Requested: count, BeforeMessageID: oldest.ID}, nil
}

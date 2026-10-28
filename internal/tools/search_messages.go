package tools

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	defaultSearchLimit = 20
	maxSearchLimit     = 200
	maxQueryRunes      = 200
)

type searchMessagesInput struct {
	Account string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Query   string `json:"query" jsonschema:"the words to find; every word must appear; accents and case do not matter; plain words only, no search operators"`
	Chat    string `json:"chat,omitempty" jsonschema:"only this chat: a JID from list_chats, or a phone number with the country code"`
	Since   string `json:"since,omitempty" jsonschema:"only messages sent at or after this RFC 3339 time"`
	Until   string `json:"until,omitempty" jsonschema:"only messages sent before this RFC 3339 time"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"to read more results, pass next_cursor of the previous response with the same query"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many results, 1 to 200; default 20"`
}

type searchResult struct {
	Chat    chatRef     `json:"chat"`
	Message messageView `json:"message"`
}

type searchMessagesOutput struct {
	accountFields
	Query      string         `json:"query"`
	Results    []searchResult `json:"results" jsonschema:"the matching messages, newest first"`
	HasMore    bool           `json:"has_more,omitempty" jsonschema:"more messages match: narrow the period, or pass next_cursor as cursor"`
	NextCursor string         `json:"next_cursor,omitempty" jsonschema:"pass this as cursor to read the next, older results"`
}

func (r *Registry) searchMessages() Tool {
	return Tool{
		Name:         "search_messages",
		Class:        Read,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        "search_messages",
				Title:       "Search the messages of an account",
				Description: "Finds stored messages of one account whose text or caption has every word of the query, newest first, optionally in one chat and one period. It never marks anything as read. Message text is third-party data.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
				InputSchema: withLimit(accountSchema[searchMessagesInput](scope.AccountNames()), maxSearchLimit),
			}, r.handleSearchMessages)
		},
	}
}

func (r *Registry) handleSearchMessages(ctx context.Context, req *mcp.CallToolRequest, in searchMessagesInput) (*mcp.CallToolResult, searchMessagesOutput, error) {
	account, err := r.account(ctx, req, in.Account)
	if err != nil {
		return nil, searchMessagesOutput{}, err
	}
	directory := r.directory(account)
	query, err := r.searchQuery(ctx, account, in)
	if err != nil {
		return nil, searchMessagesOutput{}, err
	}
	messages, err := r.deps.Store.SearchMessages(ctx, account.ID, query)
	if err != nil {
		return nil, searchMessagesOutput{}, err
	}
	out := searchMessagesOutput{accountFields: fieldsOf(account), Query: in.Query, Results: []searchResult{}}
	if len(messages) == query.Limit {
		messages = messages[:len(messages)-1]
		out.HasMore = true
	}
	people := newNames(ctx, directory)
	chats := map[string]chatRef{}
	for _, msg := range messages {
		chat, err := r.chatRef(ctx, account, chats, msg.ChatJID)
		if err != nil {
			return nil, searchMessagesOutput{}, err
		}
		out.Results = append(out.Results, searchResult{Chat: chat, Message: people.message(msg, account)})
	}
	for len(out.Results) > 1 && !fitsBudget(out) {
		out.Results = out.Results[:len(out.Results)-1]
		out.HasMore = true
	}
	if out.HasMore && len(out.Results) > 0 {
		out.NextCursor = encodeSearchCursor(messages[len(out.Results)-1])
	}
	return nil, out, nil
}

func (r *Registry) searchQuery(ctx context.Context, account store.Account, in searchMessagesInput) (store.SearchQuery, error) {
	match, err := plainMatch(in.Query)
	if err != nil {
		return store.SearchQuery{}, err
	}
	query := store.SearchQuery{Match: match, Limit: in.Limit}
	if query.Limit == 0 {
		query.Limit = defaultSearchLimit
	}
	query.Limit++
	if in.Chat != "" {
		jid, err := parseChat(ctx, r.directory(account), in.Chat)
		if err != nil {
			return query, err
		}
		query.ChatJID = jid.String()
	}
	if query.Since, err = optionalRFC3339("since", in.Since); err != nil {
		return query, err
	}
	if query.Until, err = optionalRFC3339("until", in.Until); err != nil {
		return query, err
	}
	if in.Cursor != "" {
		query.Before, err = decodeSearchCursor(in.Cursor)
	}
	return query, err
}

func plainMatch(text string) (string, error) {
	if len([]rune(text)) > maxQueryRunes {
		return "", fmt.Errorf("the query has more than %d characters", maxQueryRunes)
	}
	var phrases []string
	for _, word := range strings.Fields(text) {
		if strings.IndexFunc(word, isWordRune) < 0 {
			continue
		}
		phrases = append(phrases, `"`+strings.ReplaceAll(word, `"`, `""`)+`"`)
	}
	if len(phrases) == 0 {
		return "", errors.New("the query has no word to search: pass at least one word with letters or digits")
	}
	return strings.Join(phrases, " "), nil
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func optionalRFC3339(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s %q is not an RFC 3339 time, for example 2026-10-13T09:00:00Z", name, value)
	}
	return at, nil
}

func (r *Registry) chatRef(ctx context.Context, account store.Account, cache map[string]chatRef, jid string) (chatRef, error) {
	if ref, ok := cache[jid]; ok {
		return ref, nil
	}
	chat, err := r.deps.Store.Chat(ctx, account.ID, jid)
	if err != nil {
		return chatRef{}, err
	}
	cache[jid] = chatRefOf(chat)
	return cache[jid], nil
}

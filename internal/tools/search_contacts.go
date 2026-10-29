package tools

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const (
	defaultContactLimit = 20
	maxContactLimit     = 100
)

var errEmptyQuery = errors.New("the query is empty: pass part of a name or of a phone number")

type searchContactsInput struct {
	Account string `json:"account" jsonschema:"the account name, from list_accounts; ask the owner when the request does not name the account and more than one fits"`
	Query   string `json:"query" jsonschema:"part of the name, ignoring case and accents, or digits of the phone number"`
	Limit   int    `json:"limit,omitempty" jsonschema:"at most this many contacts, 1 to 100; default 20"`
	Cursor  string `json:"cursor,omitempty" jsonschema:"next_cursor of the previous page"`
}

type searchContactsOutput struct {
	accountFields
	Contacts   []personView `json:"contacts" jsonschema:"the matching contacts, by name"`
	NextCursor string       `json:"next_cursor,omitempty" jsonschema:"more contacts match: pass this as cursor to read the next page"`
}

func (r *Registry) searchContacts() Tool {
	return Tool{
		Name:         "search_contacts",
		Class:        Read,
		NeedsAccount: true,
		Install: func(srv *mcp.Server, scope Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        "search_contacts",
				Title:       "Search the contacts of an account",
				Description: "Finds people known to one account, by name or phone number: the address book of the phone and everyone who wrote to the account. Use the jid as chat in the other tools.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
				InputSchema: withLimit(accountSchema[searchContactsInput](scope.AccountNames()), maxContactLimit),
			}, r.handleSearchContacts)
		},
	}
}

func (r *Registry) handleSearchContacts(ctx context.Context, req *mcp.CallToolRequest, in searchContactsInput) (*mcp.CallToolResult, searchContactsOutput, error) {
	account, err := r.account(ctx, req, in.Account)
	if err != nil {
		return nil, searchContactsOutput{}, err
	}
	wanted := newSearch(in.Query)
	if wanted.empty() {
		return nil, searchContactsOutput{}, errEmptyQuery
	}
	offset, err := contactOffset(in.Cursor)
	if err != nil {
		return nil, searchContactsOutput{}, err
	}
	directory, err := r.connectedDirectory(account)
	if err != nil {
		return nil, searchContactsOutput{}, err
	}
	all, err := directory.Contacts(ctx)
	if err != nil {
		return nil, searchContactsOutput{}, fmt.Errorf("cannot read the contacts of the account %s: %v", account.Name, err)
	}
	found := matchingContacts(ctx, directory, all, wanted)
	out := searchContactsOutput{accountFields: fieldsOf(account), Contacts: []personView{}}
	pageContacts(&out, found, offset, cmp.Or(in.Limit, defaultContactLimit))
	return nil, out, nil
}

func matchingContacts(ctx context.Context, directory whatsapp.Directory, all map[types.JID]types.ContactInfo, wanted search) []personView {
	byJID := map[types.JID]personView{}
	for jid, contact := range all {
		if jid.Server != types.DefaultUserServer && jid.Server != types.HiddenUserServer {
			continue
		}
		resolved := whatsapp.ResolvePhone(ctx, directory, jid, types.JID{})
		if !wanted.matches(phoneUser(resolved), contact.FullName, contact.FirstName, contact.BusinessName, contact.PushName) {
			continue
		}
		view := personView{JID: resolved.String(), Phone: whatsapp.Phone(resolved), Name: whatsapp.DisplayName(contact)}
		if known, ok := byJID[resolved]; !ok || known.Name == "" || jid == resolved {
			byJID[resolved] = view
		}
	}
	found := make([]personView, 0, len(byJID))
	for _, view := range byJID {
		found = append(found, view)
	}
	slices.SortFunc(found, func(a, b personView) int {
		return cmp.Or(strings.Compare(fold(a.Name), fold(b.Name)), strings.Compare(a.JID, b.JID))
	})
	return found
}

func phoneUser(jid types.JID) string {
	if whatsapp.Phone(jid) == "" {
		return ""
	}
	return jid.User
}

func pageContacts(out *searchContactsOutput, found []personView, offset, limit int) {
	if offset >= len(found) {
		return
	}
	end := min(offset+limit, len(found))
	out.Contacts = found[offset:end]
	for len(out.Contacts) > 1 && !fitsBudget(out) {
		end--
		out.Contacts = found[offset:end]
	}
	if end < len(found) {
		out.NextCursor = strconv.Itoa(end)
	}
}

func contactOffset(cursor string) (int, error) {
	if strings.TrimSpace(cursor) == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0, fmt.Errorf("the cursor %q is not valid: pass next_cursor of a previous search_contacts response", cursor)
	}
	return offset, nil
}

package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

type accountFields struct {
	Account      string `json:"account" jsonschema:"the account this response is about"`
	AccountPhone string `json:"account_phone" jsonschema:"the international phone number of the account"`
}

func fieldsOf(account store.Account) accountFields {
	return accountFields{Account: account.Name, AccountPhone: account.Phone}
}

func (r *Registry) account(ctx context.Context, req *mcp.CallToolRequest, name string) (store.Account, error) {
	scope, err := r.scopeFor(ctx, req)
	if err != nil {
		return store.Account{}, err
	}
	return scope.account(name)
}

func (scope Scope) account(name string) (store.Account, error) {
	if name == "" {
		return store.Account{}, fmt.Errorf("the argument account is required: pass one of %s from list_accounts; ask the owner when more than one fits", quoted(scope.AccountNames()))
	}
	for _, account := range scope.Accounts {
		if account.Name == name {
			return account, nil
		}
	}
	return store.Account{}, fmt.Errorf("the account %q is not in this access: use one of %s from list_accounts", name, quoted(scope.AccountNames()))
}

func (r *Registry) directory(account store.Account) whatsapp.Directory {
	client, ok := r.deps.Clients.Client(account.ID)
	if !ok {
		return nil
	}
	return client
}

func (r *Registry) connectedDirectory(account store.Account) (whatsapp.Directory, error) {
	directory := r.directory(account)
	if directory == nil {
		return nil, fmt.Errorf("the account %s has no WhatsApp session (state %s); the owner must pair it again", account.Name, account.State)
	}
	return directory, nil
}

func quoted(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	return `"` + strings.Join(names, `", "`) + `"`
}

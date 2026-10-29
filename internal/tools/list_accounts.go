package tools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type listAccountsInput struct{}

type accountItem struct {
	Name        string `json:"name"`
	Phone       string `json:"phone" jsonschema:"the international phone number of the account"`
	ProfileName string `json:"profile_name" jsonschema:"the WhatsApp profile name of the account, empty when unknown"`
	Description string `json:"description" jsonschema:"what the owner uses the account for"`
	State       string `json:"state" jsonschema:"pairing, connected, disconnected or logged_out; a logged_out account needs the owner to pair it again"`
}

type listAccountsOutput struct {
	Accounts []accountItem `json:"accounts"`
}

func (r *Registry) listAccounts() Tool {
	return Tool{
		Name:         "list_accounts",
		Class:        Read,
		NeedsAccount: false,
		Install: func(srv *mcp.Server, _ Scope) {
			mcp.AddTool(srv, &mcp.Tool{
				Name:        "list_accounts",
				Title:       "List WhatsApp accounts",
				Description: "Lists the WhatsApp accounts this access can use. Pass the account name to every other tool.",
				Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
			}, r.handleListAccounts)
		},
	}
}

func (r *Registry) handleListAccounts(ctx context.Context, req *mcp.CallToolRequest, _ listAccountsInput) (*mcp.CallToolResult, listAccountsOutput, error) {
	scope, err := r.scopeFor(ctx, req)
	if err != nil {
		return nil, listAccountsOutput{}, err
	}
	out := listAccountsOutput{Accounts: make([]accountItem, 0, len(scope.Accounts))}
	for _, account := range scope.Accounts {
		out.Accounts = append(out.Accounts, accountItem{
			Name:        account.Name,
			Phone:       account.Phone,
			ProfileName: account.ProfileName,
			Description: account.Description,
			State:       account.State,
		})
	}
	return nil, out, nil
}

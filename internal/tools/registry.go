package tools

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/media"
	"github.com/ribeirogab/zapgate/internal/oauth"
	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

type Class int

const (
	Read Class = iota
	Write
	MarkRead
)

var (
	ErrAccessRevoked = errors.New("access revoked")
	ErrDeclined      = errors.New("the owner did not approve")
)

type Tool struct {
	Name         string
	Class        Class
	NeedsAccount bool
	Install      func(srv *mcp.Server, scope Scope)
}

type Scope struct {
	Grant    store.Grant
	Accounts []store.Account
}

func (s Scope) AccountNames() []string {
	names := make([]string, 0, len(s.Accounts))
	for _, account := range s.Accounts {
		names = append(names, account.Name)
	}
	return names
}

type Clients interface {
	Client(accountID int64) (whatsapp.Client, bool)
}

type Deps struct {
	Store       *store.Store
	Clients     Clients
	Outbox      *media.Outbox
	Clock       clock.Clock
	Log         *slog.Logger
	SigningKey  []byte
	ApprovalTTL time.Duration
	Media       MediaDeps
}

type Registry struct {
	deps        Deps
	approvals   *approvals
	historyPace historyPace
}

func NewRegistry(deps Deps) *Registry {
	return &Registry{deps: deps, approvals: newApprovals(deps.SigningKey, deps.Clock, deps.ApprovalTTL, deps.Store)}
}

func (r *Registry) all() []Tool {
	return []Tool{
		r.listAccounts(),
		r.listChats(),
		r.getMessages(),
		r.getChat(),
		r.searchContacts(),
		r.searchMessages(),
		r.requestOlderHistory(),
		r.downloadMedia(),
		r.sendMessage(),
		r.sendMedia(),
		r.react(),
		r.editMessage(),
		r.deleteMessage(),
		r.markRead(),
	}
}

func (r *Registry) Install(srv *mcp.Server, scope Scope) {
	var hidden []string
	for _, tool := range r.all() {
		if !allowed(tool, scope) {
			hidden = append(hidden, tool.Name)
			continue
		}
		tool.Install(srv, scope)
	}
	if len(hidden) > 0 {
		srv.RemoveTools(hidden...)
	}
}

func (r *Registry) LoadScope(ctx context.Context, grantID int64) (Scope, error) {
	grant, err := r.deps.Store.Grant(ctx, grantID)
	if errors.Is(err, store.ErrNotFound) || (err == nil && grant.Revoked()) {
		return Scope{}, ErrAccessRevoked
	}
	if err != nil {
		return Scope{}, err
	}
	accounts, err := r.deps.Store.GrantAccounts(ctx, grant)
	if err != nil {
		return Scope{}, err
	}
	return Scope{Grant: grant, Accounts: accounts}, nil
}

func (r *Registry) scopeFor(ctx context.Context, req *mcp.CallToolRequest) (Scope, error) {
	if req.Extra == nil {
		return Scope{}, ErrAccessRevoked
	}
	grantID, ok := oauth.GrantID(req.Extra.TokenInfo)
	if !ok {
		return Scope{}, ErrAccessRevoked
	}
	return r.LoadScope(ctx, grantID)
}

func allowed(tool Tool, scope Scope) bool {
	if tool.NeedsAccount && len(scope.Accounts) == 0 {
		return false
	}
	return tool.Class == Read || !scope.Grant.ReadOnly
}

func accountSchema[T any](accounts []string) *jsonschema.Schema {
	if len(accounts) == 0 {
		panic("accountSchema needs at least one account; Install hides tools that need an account")
	}
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		panic(fmt.Sprintf("schema for %T: %v", *new(T), err))
	}
	prop := schema.Properties["account"]
	prop.MinLength = jsonschema.Ptr(1)
	prop.Enum = make([]any, 0, len(accounts))
	for _, name := range accounts {
		prop.Enum = append(prop.Enum, name)
	}
	return schema
}

func withLimit(schema *jsonschema.Schema, max int) *jsonschema.Schema {
	prop := schema.Properties["limit"]
	prop.Minimum = jsonschema.Ptr(1.0)
	prop.Maximum = jsonschema.Ptr(float64(max))
	return schema
}

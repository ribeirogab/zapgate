package tools

import (
	"context"
	"encoding/json"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/oauth"
)

const toolsCallMethod = "tools/call"

func (r *Registry) AccountGuard(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != toolsCallMethod {
			return next(ctx, method, req)
		}
		if refusal := r.refuseAccount(ctx, req); refusal != nil {
			return refusal, nil
		}
		return next(ctx, method, req)
	}
}

func (r *Registry) refuseAccount(ctx context.Context, req mcp.Request) *mcp.CallToolResult {
	params, ok := req.GetParams().(*mcp.CallToolParamsRaw)
	if !ok || !r.needsAccount(params.Name) {
		return nil
	}
	extra := req.GetExtra()
	if extra == nil {
		return nil
	}
	grantID, ok := oauth.GrantID(extra.TokenInfo)
	if !ok {
		return nil
	}
	scope, err := r.LoadScope(ctx, grantID)
	if err != nil {
		return refusal(err)
	}
	if _, err := scope.account(accountArgument(params.Arguments)); err != nil {
		return refusal(err)
	}
	return nil
}

func (r *Registry) needsAccount(name string) bool {
	for _, tool := range r.all() {
		if tool.Name == name {
			return tool.NeedsAccount
		}
	}
	return false
}

func accountArgument(arguments json.RawMessage) string {
	var args struct {
		Account any `json:"account"`
	}
	if json.Unmarshal(arguments, &args) != nil {
		return ""
	}
	name, _ := args.Account.(string)
	return name
}

func refusal(err error) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}
	result.SetError(err)
	return result
}

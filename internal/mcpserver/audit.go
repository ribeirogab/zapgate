package mcpserver

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/oauth"
	"github.com/ribeirogab/zapgate/internal/tools"
)

const maxAuditedAccountRunes = 64

func (e *Endpoint) auditToolCalls(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		result, err := next(ctx, method, req)
		if method == toolsCallMethod {
			e.deps.Audit.ToolCall(ctx, toolCallEvent(req, result, err))
		}
		return result, err
	}
}

func toolCallEvent(req mcp.Request, result mcp.Result, err error) audit.ToolCall {
	event := audit.ToolCall{Result: callResult(result, err)}
	if params, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok {
		event.Tool = params.Name
		event.Account = accountOf(params.Arguments)
	}
	if extra := req.GetExtra(); extra != nil {
		event.GrantID, _ = oauth.GrantID(extra.TokenInfo)
		event.Grant = oauth.GrantName(extra.TokenInfo)
		event.IP = extra.Header.Get(audit.ClientIPHeader)
	}
	return event
}

func callResult(result mcp.Result, err error) string {
	if err != nil {
		return audit.ResultError
	}
	call, ok := result.(*mcp.CallToolResult)
	switch {
	case !ok || call == nil:
		return audit.ResultOK
	case call.IsError && errors.Is(call.GetError(), tools.ErrDeclined):
		return audit.ResultDeclined
	case call.IsError:
		return audit.ResultError
	case len(call.InputRequests) > 0:
		return audit.ResultInputRequired
	}
	return audit.ResultOK
}

func accountOf(arguments json.RawMessage) string {
	var args map[string]json.RawMessage
	if json.Unmarshal(arguments, &args) != nil {
		return ""
	}
	var account string
	if json.Unmarshal(args[accountArgument], &account) != nil {
		return ""
	}
	return truncateRunes(account, maxAuditedAccountRunes)
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

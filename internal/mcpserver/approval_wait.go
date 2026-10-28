package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (e *Endpoint) waitForTheOwner(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method != toolsCallMethod {
			return next(ctx, method, req)
		}
		ctx, cancel := context.WithTimeout(ctx, e.deps.ApprovalTimeout)
		defer cancel()
		if session, ok := req.GetSession().(*mcp.ServerSession); ok && session.ID() != "" {
			stop := make(chan struct{})
			defer close(stop)
			go pingUntil(ctx, session, e.deps.ApprovalPing, stop)
		}
		result, err := next(ctx, method, req)
		if err != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return unanswered(e.deps.ApprovalTimeout), nil
		}
		return result, err
	}
}

func pingUntil(ctx context.Context, session *mcp.ServerSession, every time.Duration, stop <-chan struct{}) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, every)
			session.Ping(pingCtx, nil)
			cancel()
		}
	}
}

func unanswered(timeout time.Duration) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}
	result.SetError(fmt.Errorf("the owner did not answer within %s, so nothing was done; call the tool again to ask the owner again", timeout))
	return result
}

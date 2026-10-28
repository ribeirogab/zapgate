package app_test

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ribeirogab/zapgate/internal/apptest"
	"github.com/ribeirogab/zapgate/internal/config"
)

func countPings(count *atomic.Int64) apptest.ConnectOption {
	return apptest.WithReceivingMiddleware(func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method == "ping" {
				count.Add(1)
			}
			return next(ctx, method, req)
		}
	})
}

func TestTheServerPingsTheClientWhileTheOwnerDecides(t *testing.T) {
	h := apptest.Start(t, apptest.WithConfig(func(cfg *config.Config) { cfg.ApprovalPing = 40 * time.Millisecond }))
	var pings atomic.Int64
	slowOwner := apptest.WithElicitation(func(context.Context, *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		time.Sleep(300 * time.Millisecond)
		return &mcp.ElicitResult{Action: "accept"}, nil
	})
	session := writerSession(t, h, apptest.WithProtocol(apptest.LegacyProtocol), slowOwner, countPings(&pings))

	result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi"}))

	if result.IsError {
		t.Fatalf("send_message failed: %s", errorText(result))
	}
	if got := pings.Load(); got < 3 {
		t.Fatalf("the client got %d pings while the owner decided for 300 ms at one ping every 40 ms", got)
	}
}

func TestTheServerStopsWaitingForTheOwnerAtTheDeadline(t *testing.T) {
	h := apptest.Start(t, apptest.WithConfig(func(cfg *config.Config) { cfg.ApprovalTimeout = 200 * time.Millisecond }))
	released := make(chan struct{})
	absentOwner := apptest.WithElicitation(func(ctx context.Context, _ *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
		select {
		case <-ctx.Done():
		case <-released:
		}
		return &mcp.ElicitResult{Action: "cancel"}, nil
	})
	session := writerSession(t, h, apptest.WithProtocol(apptest.LegacyProtocol), absentOwner)

	started := time.Now()
	result := callTool(t, session, "send_message", withWork(map[string]any{"to": mariaJID.String(), "text": "oi"}))
	close(released)

	if !result.IsError || errorText(result) != "the owner did not answer within 200ms, so nothing was done; call the tool again to ask the owner again" {
		t.Fatalf("error %v %q", result.IsError, errorText(result))
	}
	if waited := time.Since(started); waited > 2*time.Second {
		t.Fatalf("the call took %s", waited)
	}
	if sent := h.WhatsApp.Sent(workPhone); len(sent) != 0 {
		t.Fatalf("WhatsApp got %+v", sent)
	}
	if !strings.Contains(h.AuditRaw(), `"tool":"send_message"`) {
		t.Fatalf("the call is not audited: %s", h.AuditRaw())
	}
}

func TestTheDefaultDeadlineIsFiveMinutesWithAPingEvery25Seconds(t *testing.T) {
	cfg, err := config.FromEnv(func(key string) string {
		return map[string]string{
			"ZAPGATE_PUBLIC_URL":     apptest.PublicURL,
			"ZAPGATE_OWNER_PASSWORD": "x",
			"ZAPGATE_SIGNING_KEY":    strings.Repeat("ab", 32),
		}[key]
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ApprovalTimeout != 5*time.Minute || cfg.ApprovalPing != 25*time.Second {
		t.Fatalf("timeout %s, ping %s", cfg.ApprovalTimeout, cfg.ApprovalPing)
	}
}

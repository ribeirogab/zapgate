package audit

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/ribeirogab/zapgate/internal/clock"
)

const (
	ClientIPHeader         = "CF-Connecting-IP"
	ResultOK               = "ok"
	ResultError            = "error"
	ResultInputRequired    = "input_required"
	ResultDeclined         = "declined"
	ReasonMissing          = "missing"
	ReasonInvalid          = "invalid"
	ActionTokenCreate      = "token_create"
	ActionAccessRevoke     = "access_revoke"
	ActionAccessSet        = "access_set"
	ActionAccountAdd       = "account_add"
	ActionAccountDescribe  = "account_describe"
	ActionAccountRemove    = "account_remove"
	ActionAccountRetention = "account_retention"
	RetentionDefault       = "default"
)

type Logger struct {
	handler slog.Handler
	clock   clock.Clock
}

type ToolCall struct {
	GrantID int64
	Grant   string
	Tool    string
	Account string
	Result  string
	IP      string
}

type AuthFailure struct {
	Path   string
	Reason string
	IP     string
}

type OwnerAction struct {
	Action   string
	GrantID  int64
	Grant    string
	Kind     string
	Approval string
}

type Upload struct {
	GrantID int64
	Grant   string
	Result  string
	Size    int64
	IP      string
}

func New(w io.Writer, clk clock.Clock) *Logger {
	return &Logger{handler: slog.NewJSONHandler(w, nil), clock: clk}
}

func (l *Logger) ToolCall(ctx context.Context, e ToolCall) {
	l.write(ctx, "tool_call",
		slog.Int64("grant_id", e.GrantID),
		slog.String("grant", e.Grant),
		slog.String("tool", e.Tool),
		slog.String("account", e.Account),
		slog.String("result", e.Result),
		slog.String("ip", e.IP),
	)
}

func (l *Logger) AuthFailure(ctx context.Context, e AuthFailure) {
	l.write(ctx, "auth",
		slog.String("result", ResultDenied),
		slog.String("reason", e.Reason),
		slog.String("path", e.Path),
		slog.String("ip", e.IP),
	)
}

func (l *Logger) OwnerAction(ctx context.Context, e OwnerAction) {
	attrs := []slog.Attr{
		slog.String("action", e.Action),
		slog.Int64("grant_id", e.GrantID),
		slog.String("grant", e.Grant),
		slog.String("kind", e.Kind),
	}
	if e.Approval != "" {
		attrs = append(attrs, slog.String("approval", e.Approval))
	}
	l.write(ctx, "admin", attrs...)
}

func (l *Logger) Upload(ctx context.Context, e Upload) {
	l.write(ctx, "upload",
		slog.Int64("grant_id", e.GrantID),
		slog.String("grant", e.Grant),
		slog.String("result", e.Result),
		slog.Int64("size", e.Size),
		slog.String("ip", e.IP),
	)
}

func (l *Logger) AccountAction(ctx context.Context, action, account string) {
	l.write(ctx, "admin",
		slog.String("action", action),
		slog.String("account", account),
	)
}

func (l *Logger) AccountRetention(ctx context.Context, account, retention string) {
	l.write(ctx, "admin",
		slog.String("action", ActionAccountRetention),
		slog.String("account", account),
		slog.String("retention", retention),
	)
}

func (l *Logger) write(ctx context.Context, msg string, attrs ...slog.Attr) {
	record := slog.NewRecord(l.clock.Now().UTC(), slog.LevelInfo, msg, 0)
	record.AddAttrs(attrs...)
	l.handler.Handle(ctx, record)
}

func ClientIP(r *http.Request) string {
	if ip, ok := parseIP(r.Header.Get(ClientIPHeader)); ok {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func parseIP(value string) (string, bool) {
	ip := net.ParseIP(strings.TrimSpace(value))
	if ip == nil {
		return "", false
	}
	return ip.String(), true
}

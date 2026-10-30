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
	ClientIPHeader         = "X-Zapgate-Client-Ip"
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
	ActionWebhookAdd       = "webhook_add"
	ActionWebhookRemove    = "webhook_remove"
	ActionWebhookTest      = "webhook_test"
	ActionWebhookRotate    = "webhook_rotate_secret"
	ActionWebhookEnable    = "webhook_enable"
	ActionWebhookRedeliver = "webhook_redeliver"
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

type WebhookDelivery struct {
	Webhook  string
	Account  string
	Delivery string
	Events   int
	Attempt  int
	Status   int
	Result   string
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

func (l *Logger) WebhookAction(ctx context.Context, action, webhook string) {
	l.write(ctx, "admin",
		slog.String("action", action),
		slog.String("webhook", webhook),
	)
}

func (l *Logger) WebhookTest(ctx context.Context, webhook string, status int, result string) {
	l.write(ctx, "admin",
		slog.String("action", ActionWebhookTest),
		slog.String("webhook", webhook),
		slog.Int("status", status),
		slog.String("result", result),
	)
}

func (l *Logger) WebhookRedeliver(ctx context.Context, webhook string, deliveries int) {
	l.write(ctx, "admin",
		slog.String("action", ActionWebhookRedeliver),
		slog.String("webhook", webhook),
		slog.Int("deliveries", deliveries),
	)
}

func (l *Logger) WebhookDelivery(ctx context.Context, e WebhookDelivery) {
	l.write(ctx, "webhook_delivery",
		slog.String("webhook", e.Webhook),
		slog.String("account", e.Account),
		slog.String("delivery", e.Delivery),
		slog.Int("events", e.Events),
		slog.Int("attempt", e.Attempt),
		slog.Int("status", e.Status),
		slog.String("result", e.Result),
	)
}

func (l *Logger) write(ctx context.Context, msg string, attrs ...slog.Attr) {
	record := slog.NewRecord(l.clock.Now().UTC(), slog.LevelInfo, msg, 0)
	record.AddAttrs(attrs...)
	l.handler.Handle(ctx, record)
}

func WithClientIP(trustedHeader string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Header.Set(ClientIPHeader, resolveClientIP(r, trustedHeader))
		next.ServeHTTP(w, r)
	})
}

func ClientIP(r *http.Request) string {
	if ip, ok := parseIP(r.Header.Get(ClientIPHeader)); ok {
		return ip
	}
	return peerIP(r)
}

func resolveClientIP(r *http.Request, trustedHeader string) string {
	if trustedHeader == "" {
		return peerIP(r)
	}
	if ip, ok := parseIP(r.Header.Get(trustedHeader)); ok {
		return ip
	}
	return peerIP(r)
}

func peerIP(r *http.Request) string {
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

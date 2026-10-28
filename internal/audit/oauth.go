package audit

import (
	"context"
	"log/slog"
)

const (
	EventClientRegister = "client_register"
	EventAuthorize      = "authorize"
	EventTokenIssue     = "token_issue"
	EventTokenRefresh   = "token_refresh"
	EventTokenRevoke    = "token_revoke"
	ResultDenied        = "denied"
	ResultBlocked       = "blocked"
	ReasonWrongPassword = "wrong_password"
	ReasonOwnerDeclined = "owner_declined"
	ReasonCodeReused    = "code_reused"
	ReasonRefreshReused = "refresh_reused"
	ActionPasswordShow  = "password_show"
)

type OAuthEvent struct {
	Event    string
	Result   string
	Reason   string
	ClientID string
	Client   string
	GrantID  int64
	Grant    string
	IP       string
}

func (l *Logger) OAuth(ctx context.Context, e OAuthEvent) {
	l.write(ctx, "oauth",
		slog.String("event", e.Event),
		slog.String("result", e.Result),
		slog.String("reason", e.Reason),
		slog.String("client_id", e.ClientID),
		slog.String("client", e.Client),
		slog.Int64("grant_id", e.GrantID),
		slog.String("grant", e.Grant),
		slog.String("ip", e.IP),
	)
}

func (l *Logger) PasswordShown(ctx context.Context) {
	l.write(ctx, "admin", slog.String("action", ActionPasswordShow))
}

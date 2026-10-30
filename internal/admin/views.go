package admin

import (
	"time"

	"github.com/ribeirogab/zapgate/internal/retention"
	"github.com/ribeirogab/zapgate/internal/store"
)

type AccountView struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	ProfileName string `json:"profile_name"`
	Description string `json:"description"`
	State       string `json:"state"`

	RetentionMonths  int  `json:"retention_months"`
	RetentionDefault bool `json:"retention_default"`
}

type AccountStatus struct {
	AccountView
	Messages   int64 `json:"messages"`
	MediaBytes int64 `json:"media_bytes"`
}

type RetentionView struct {
	Running         bool   `json:"running"`
	StartedAt       string `json:"started_at,omitempty"`
	FinishedAt      string `json:"finished_at,omitempty"`
	DeletedMessages int    `json:"deleted_messages"`
	DeletedFiles    int    `json:"deleted_files"`
}

type StatusView struct {
	Accounts  []AccountStatus `json:"accounts"`
	Retention RetentionView   `json:"retention"`
}

type SetRetentionRequest struct {
	Months int `json:"months"`
}

type AccessView struct {
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	ReadOnly  bool     `json:"read_only"`
	Approval  string   `json:"approval"`
	Accounts  []string `json:"accounts"`
	CreatedAt string   `json:"created_at"`
}

type CreateTokenRequest struct {
	Name                string   `json:"name"`
	Accounts            []string `json:"accounts"`
	AllExistingAccounts bool     `json:"all_existing_accounts"`
	ReadOnly            bool     `json:"read_only"`
	Approval            string   `json:"approval,omitempty"`
}

type CreatedToken struct {
	Access AccessView `json:"access"`
	Token  string     `json:"token"`
}

type AddAccountRequest struct {
	Name        string `json:"name"`
	Phone       string `json:"phone"`
	Description string `json:"description"`
	QR          bool   `json:"qr"`
}

const (
	PairingCode      = "code"
	PairingQR        = "qr"
	PairingConnected = "connected"
	PairingFailed    = "failed"
)

type PairingEvent struct {
	Event   string       `json:"event"`
	Code    string       `json:"code,omitempty"`
	QR      string       `json:"qr,omitempty"`
	Account *AccountView `json:"account,omitempty"`
	Error   string       `json:"error,omitempty"`
}

type DescribeAccountRequest struct {
	Description string `json:"description"`
}

type RemovedAccount struct {
	Account AccountView `json:"account"`
	Warning string      `json:"warning,omitempty"`
}

type SetAccessRequest struct {
	Accounts            []string `json:"accounts"`
	AllExistingAccounts bool     `json:"all_existing_accounts"`
	Approval            string   `json:"approval,omitempty"`
}

type WebhookOptions struct {
	URL                 string   `json:"url,omitempty"`
	Accounts            []string `json:"accounts"`
	AllExistingAccounts bool     `json:"all_existing_accounts,omitempty"`
	Chats               string   `json:"chats,omitempty"`
	Events              []string `json:"events"`
	Content             string   `json:"content,omitempty"`
	Format              string   `json:"format,omitempty"`
	Headers             []string `json:"headers"`
}

type AddWebhookRequest struct {
	Name string `json:"name"`
	WebhookOptions
}

type WebhookView struct {
	Name        string   `json:"name"`
	URL         string   `json:"url"`
	Accounts    []string `json:"accounts"`
	Events      []string `json:"events"`
	Chats       string   `json:"chats"`
	ListedChats []string `json:"listed_chats,omitempty"`
	Content     string   `json:"content"`
	Format      string   `json:"format"`
	HeaderNames []string `json:"header_names,omitempty"`
	State       string   `json:"state"`
	CreatedAt   string   `json:"created_at"`

	Pending     int            `json:"pending"`
	Failed      int            `json:"failed"`
	LastSuccess string         `json:"last_success,omitempty"`
	LastError   *DeliveryError `json:"last_error,omitempty"`
}

type DeliveryError struct {
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

type CreatedWebhook struct {
	Webhook WebhookView `json:"webhook"`
	Secret  string      `json:"secret"`
}

type RotatedWebhookSecret struct {
	Name                string `json:"name"`
	Secret              string `json:"secret"`
	PreviousSecretUntil string `json:"previous_secret_until"`
}

type RemovedWebhook struct {
	Name string `json:"name"`
}

type WebhookTest struct {
	Name   string `json:"name"`
	Status int    `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
}

type webhookResponse struct {
	Webhook WebhookView `json:"webhook"`
}

type webhookListResponse struct {
	Webhooks []WebhookView `json:"webhooks"`
}

type accountResponse struct {
	Account AccountView `json:"account"`
}

type accountsResponse struct {
	Accounts []AccountView `json:"accounts"`
}

type accessListResponse struct {
	Access []AccessView `json:"access"`
}

type accessResponse struct {
	Access AccessView `json:"access"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *handler) accountView(a store.Account) AccountView {
	return AccountView{
		Name:             a.Name,
		Phone:            a.Phone,
		ProfileName:      a.ProfileName,
		Description:      a.Description,
		State:            a.State,
		RetentionMonths:  retention.Months(a, h.deps.DefaultRetentionMonths),
		RetentionDefault: a.RetentionMonths == 0,
	}
}

func (h *handler) accountViews(accounts []store.Account) []AccountView {
	views := make([]AccountView, 0, len(accounts))
	for _, account := range accounts {
		views = append(views, h.accountView(account))
	}
	return views
}

func retentionView(pass retention.Pass) RetentionView {
	return RetentionView{
		Running:         pass.Running,
		StartedAt:       optionalTime(pass.StartedAt),
		FinishedAt:      optionalTime(pass.FinishedAt),
		DeletedMessages: pass.Messages,
		DeletedFiles:    pass.Files,
	}
}

func optionalTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func accessView(g store.Grant, accounts []store.Account) AccessView {
	names := make([]string, 0, len(accounts))
	for _, account := range accounts {
		names = append(names, account.Name)
	}
	return AccessView{
		Name:      g.Name,
		Kind:      g.Kind,
		ReadOnly:  g.ReadOnly,
		Approval:  g.Approval,
		Accounts:  names,
		CreatedAt: g.CreatedAt.UTC().Format(time.RFC3339),
	}
}

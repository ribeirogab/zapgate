package admin

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
	"github.com/ribeirogab/zapgate/internal/webhooks"
)

func (h *handler) registerWebhookRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /webhooks", h.serveCreated(h.addWebhook))
	mux.HandleFunc("GET /webhooks", h.serve(h.listWebhooks))
	mux.HandleFunc("DELETE /webhooks/{name}", h.serve(h.removeWebhook))
	mux.HandleFunc("POST /webhooks/{name}/test", h.serve(h.testWebhook))
	mux.HandleFunc("POST /webhooks/{name}/secret", h.serve(h.rotateWebhookSecret))
}

func (h *handler) rotateWebhookSecret(r *http.Request) (any, error) {
	name := r.PathValue("name")
	rotated, err := h.deps.Webhooks.RotateSecret(r.Context(), name)
	if err != nil {
		return nil, webhookError(name, err)
	}
	h.deps.Audit.WebhookAction(r.Context(), audit.ActionWebhookRotate, rotated.Name)
	return RotatedWebhookSecret{
		Name:                rotated.Name,
		Secret:              rotated.Secret,
		PreviousSecretUntil: rotated.PreviousSecretUntil.UTC().Format(time.RFC3339),
	}, nil
}

func (h *handler) addWebhook(r *http.Request) (any, error) {
	var req AddWebhookRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	registered, secret, err := h.deps.Webhooks.Add(r.Context(), webhooks.Registration{
		Name:                req.Name,
		URL:                 req.URL,
		AllExistingAccounts: req.AllExistingAccounts,
		Accounts:            req.Accounts,
		Content:             req.Content,
		Quiet:               req.Quiet,
		MaxWait:             req.MaxWait,
	})
	if err != nil {
		return nil, webhookError(req.Name, err)
	}
	h.deps.Audit.WebhookAction(r.Context(), audit.ActionWebhookAdd, registered.Name)
	return CreatedWebhook{Webhook: webhookView(registered), Secret: secret}, nil
}

func (h *handler) listWebhooks(r *http.Request) (any, error) {
	list, err := h.deps.Webhooks.List(r.Context())
	if err != nil {
		return nil, err
	}
	views := make([]WebhookView, 0, len(list))
	for _, registered := range list {
		views = append(views, webhookView(registered))
	}
	return webhookListResponse{Webhooks: views}, nil
}

func (h *handler) removeWebhook(r *http.Request) (any, error) {
	name := r.PathValue("name")
	removed, err := h.deps.Webhooks.Remove(r.Context(), name)
	if err != nil {
		return nil, webhookError(name, err)
	}
	h.deps.Audit.WebhookAction(r.Context(), audit.ActionWebhookRemove, removed.Name)
	return RemovedWebhook{Name: removed.Name}, nil
}

func (h *handler) testWebhook(r *http.Request) (any, error) {
	name := r.PathValue("name")
	tested, result, err := h.deps.Webhooks.Test(r.Context(), name)
	if err != nil {
		return nil, webhookError(name, err)
	}
	h.deps.Audit.WebhookTest(r.Context(), tested.Name, result.Status, testResult(result))
	return webhookTestView(tested, result), nil
}

func testResult(result webhooks.Result) string {
	switch {
	case result.Accepted():
		return audit.ResultOK
	case result.Blocked():
		return audit.ResultBlocked
	}
	return audit.ResultError
}

func webhookTestView(tested store.Webhook, result webhooks.Result) WebhookTest {
	view := WebhookTest{Name: tested.Name, Status: result.Status}
	if result.Err != nil {
		view.Error = result.Err.Error()
	}
	return view
}

func webhookError(name string, err error) error {
	var invalid *webhooks.InvalidError
	var unknown *store.UnknownAccountError
	switch {
	case errors.As(err, &invalid):
		return &apiError{status: http.StatusBadRequest, message: invalid.Message}
	case errors.Is(err, store.ErrNameInUse):
		return &apiError{status: http.StatusConflict, message: fmt.Sprintf("the webhook name %q is already in use", name)}
	case errors.As(err, &unknown):
		return &apiError{status: http.StatusBadRequest, message: fmt.Sprintf("there is no account named %q", unknown.Name)}
	case errors.Is(err, store.ErrNotFound):
		return &apiError{status: http.StatusNotFound, message: fmt.Sprintf("there is no webhook named %q", name)}
	}
	return err
}

func webhookView(r webhooks.Registered) WebhookView {
	return WebhookView{
		Name:      r.Name,
		URL:       r.URL,
		Accounts:  r.Accounts,
		Events:    r.Events,
		Chats:     r.Chats,
		Content:   r.Content,
		Format:    r.Format,
		State:     webhooks.State(r.Webhook),
		CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
	}
}

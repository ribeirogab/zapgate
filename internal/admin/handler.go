package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/ribeirogab/zapgate/internal/accounts"
	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/clock"
	"github.com/ribeirogab/zapgate/internal/oauth"
	"github.com/ribeirogab/zapgate/internal/retention"
	"github.com/ribeirogab/zapgate/internal/store"
)

const maxBodyBytes = 1 << 20

type GrantEvents interface {
	GrantChanged(ctx context.Context, grantID int64)
	GrantRevoked(grantID int64)
}

type RetentionPasses interface {
	LastPass() retention.Pass
}

type DiskUsage interface {
	AccountBytes(accountID int64) (int64, error)
}

type Deps struct {
	Store                  *store.Store
	Accounts               *accounts.Manager
	Clock                  clock.Clock
	Grants                 GrantEvents
	Audit                  *audit.Logger
	OwnerPassword          string
	Media                  DiskUsage
	Retention              RetentionPasses
	DefaultRetentionMonths int
}

type handler struct {
	deps Deps
}

type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string { return e.message }

func Handler(deps Deps) http.Handler {
	h := &handler{deps: deps}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /status", h.serve(h.status))
	mux.HandleFunc("GET /accounts", h.serve(h.accounts))
	mux.HandleFunc("POST /accounts", h.addAccount)
	mux.HandleFunc("GET /accounts/{name}", h.serve(h.account))
	mux.HandleFunc("PUT /accounts/{name}/description", h.serve(h.describeAccount))
	mux.HandleFunc("PUT /accounts/{name}/retention", h.serve(h.setRetention))
	mux.HandleFunc("DELETE /accounts/{name}", h.serve(h.removeAccount))
	mux.HandleFunc("POST /tokens", h.serveCreated(h.createToken))
	mux.HandleFunc("GET /access", h.serve(h.listAccess))
	mux.HandleFunc("PATCH /access/{name}", h.serve(h.setAccess))
	mux.HandleFunc("POST /access/{name}/revoke", h.serve(h.revokeAccess))
	mux.HandleFunc("GET /password", h.serve(h.password))
	return mux
}

type endpoint func(r *http.Request) (any, error)

func (h *handler) serve(fn endpoint) http.HandlerFunc {
	return h.respond(http.StatusOK, fn)
}

func (h *handler) serveCreated(fn endpoint) http.HandlerFunc {
	return h.respond(http.StatusCreated, fn)
}

func (h *handler) respond(status int, fn endpoint) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := fn(r)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, status, body)
	}
}

func (h *handler) createToken(r *http.Request) (any, error) {
	var req CreateTokenRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if err := validateTokenRequest(req); err != nil {
		return nil, err
	}
	token, err := oauth.NewStaticToken()
	if err != nil {
		return nil, err
	}
	now := h.deps.Clock.Now()
	grant, err := h.deps.Store.CreateGrant(r.Context(), store.NewGrant{
		Name:                req.Name,
		Kind:                store.GrantStatic,
		ReadOnly:            req.ReadOnly,
		Approval:            req.Approval,
		AllExistingAccounts: req.AllExistingAccounts,
		AccountNames:        req.Accounts,
		CreatedAt:           now,
		Tokens:              []store.Token{{Hash: oauth.HashToken(token), Kind: store.TokenStatic, CreatedAt: now}},
	})
	if err != nil {
		return nil, grantError(req.Name, err)
	}
	h.auditGrantSettings(r.Context(), audit.ActionTokenCreate, grant)
	view, err := h.accessView(r.Context(), grant)
	if err != nil {
		return nil, err
	}
	return CreatedToken{Access: view, Token: token}, nil
}

func validateTokenRequest(req CreateTokenRequest) error {
	if err := oauth.ValidateGrantName(req.Name); err != nil {
		return &apiError{status: http.StatusBadRequest, message: err.Error()}
	}
	if err := validateAccountChoice(req.AllExistingAccounts, req.Accounts); err != nil {
		return err
	}
	return validateApproval(req.Approval)
}

func validateApproval(mode string) error {
	if mode == "" || store.ValidApproval(mode) {
		return nil
	}
	return &apiError{status: http.StatusBadRequest, message: fmt.Sprintf("the approval mode %q is not valid: use %s or %s", mode, store.ApprovalElicitation, store.ApprovalApp)}
}

func validateAccountChoice(all bool, names []string) error {
	if all && len(names) > 0 {
		return &apiError{status: http.StatusBadRequest, message: "choose all existing accounts or a list of accounts, not both"}
	}
	if !all && len(names) == 0 {
		return &apiError{status: http.StatusBadRequest, message: "the access needs a list of accounts, or all existing accounts"}
	}
	return nil
}

func grantError(name string, err error) error {
	var unknown *store.UnknownAccountError
	switch {
	case errors.Is(err, store.ErrNameInUse):
		return &apiError{status: http.StatusConflict, message: fmt.Sprintf("the access name %q is already in use", name)}
	case errors.As(err, &unknown):
		return &apiError{status: http.StatusBadRequest, message: fmt.Sprintf("there is no account named %q", unknown.Name)}
	case errors.Is(err, store.ErrNotFound):
		return &apiError{status: http.StatusNotFound, message: fmt.Sprintf("there is no access named %q", name)}
	}
	return err
}

func (h *handler) listAccess(r *http.Request) (any, error) {
	grants, err := h.deps.Store.ListGrants(r.Context())
	if err != nil {
		return nil, err
	}
	views := make([]AccessView, 0, len(grants))
	for _, grant := range grants {
		view, err := h.accessView(r.Context(), grant)
		if err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return accessListResponse{Access: views}, nil
}

func (h *handler) revokeAccess(r *http.Request) (any, error) {
	name := r.PathValue("name")
	grant, err := h.deps.Store.RevokeGrant(r.Context(), name, h.deps.Clock.Now())
	if err != nil {
		return nil, grantError(name, err)
	}
	h.deps.Grants.GrantRevoked(grant.ID)
	h.auditOwnerAction(r.Context(), audit.ActionAccessRevoke, grant)
	view, err := h.accessView(r.Context(), grant)
	if err != nil {
		return nil, err
	}
	return accessResponse{Access: view}, nil
}

func (h *handler) auditOwnerAction(ctx context.Context, action string, grant store.Grant) {
	h.deps.Audit.OwnerAction(ctx, audit.OwnerAction{Action: action, GrantID: grant.ID, Grant: grant.Name, Kind: grant.Kind})
}

func (h *handler) auditGrantSettings(ctx context.Context, action string, grant store.Grant) {
	h.deps.Audit.OwnerAction(ctx, audit.OwnerAction{Action: action, GrantID: grant.ID, Grant: grant.Name, Kind: grant.Kind, Approval: grant.Approval})
}

func (h *handler) accessView(ctx context.Context, grant store.Grant) (AccessView, error) {
	accounts, err := h.deps.Store.GrantAccounts(ctx, grant)
	if err != nil {
		return AccessView{}, err
	}
	return accessView(grant, accounts), nil
}

func decodeBody(r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return &apiError{status: http.StatusBadRequest, message: fmt.Sprintf("the request body is not valid JSON: %v", err)}
	}
	return nil
}

func writeError(w http.ResponseWriter, err error) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		writeJSON(w, apiErr.status, errorResponse{Error: apiErr.message})
		return
	}
	writeJSON(w, http.StatusInternalServerError, errorResponse{Error: "internal error: " + err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

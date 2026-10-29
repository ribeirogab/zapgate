package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ribeirogab/zapgate/internal/accounts"
	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/store"
)

const ndjsonContentType = "application/x-ndjson"

func (h *handler) accounts(r *http.Request) (any, error) {
	list, err := h.deps.Accounts.List(r.Context())
	if err != nil {
		return nil, err
	}
	return accountsResponse{Accounts: h.accountViews(list)}, nil
}

func (h *handler) account(r *http.Request) (any, error) {
	account, err := h.deps.Accounts.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		return nil, accountError(err)
	}
	return accountResponse{Account: h.accountView(account)}, nil
}

func (h *handler) describeAccount(r *http.Request) (any, error) {
	var req DescribeAccountRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	account, err := h.deps.Accounts.Describe(r.Context(), r.PathValue("name"), req.Description)
	if err != nil {
		return nil, accountError(err)
	}
	h.deps.Audit.AccountAction(r.Context(), audit.ActionAccountDescribe, account.Name)
	return accountResponse{Account: h.accountView(account)}, nil
}

func (h *handler) removeAccount(r *http.Request) (any, error) {
	removal, err := h.deps.Accounts.Remove(r.Context(), r.PathValue("name"))
	if err != nil {
		return nil, accountError(err)
	}
	h.deps.Audit.AccountAction(r.Context(), audit.ActionAccountRemove, removal.Account.Name)
	return RemovedAccount{Account: h.accountView(removal.Account), Warning: removal.Warning}, nil
}

func (h *handler) addAccount(w http.ResponseWriter, r *http.Request) {
	var req AddAccountRequest
	if err := decodeBody(r, &req); err != nil {
		writeError(w, err)
		return
	}
	pairing, err := h.deps.Accounts.BeginPairing(r.Context(), accounts.NewAccount{Name: req.Name, Phone: req.Phone, Description: req.Description, QR: req.QR})
	if err != nil {
		writeError(w, accountError(err))
		return
	}
	stream := newEventStream(w)
	account, err := pairing.Run(r.Context(), func(p accounts.Progress) {
		stream.send(progressEvent(p))
	})
	if err != nil {
		stream.send(PairingEvent{Event: PairingFailed, Error: err.Error()})
		return
	}
	h.deps.Audit.AccountAction(r.Context(), audit.ActionAccountAdd, account.Name)
	view := h.accountView(account)
	stream.send(PairingEvent{Event: PairingConnected, Account: &view})
}

func progressEvent(p accounts.Progress) PairingEvent {
	if p.QR != "" {
		return PairingEvent{Event: PairingQR, QR: p.QR}
	}
	return PairingEvent{Event: PairingCode, Code: p.Code}
}

type eventStream struct {
	w       http.ResponseWriter
	flusher http.Flusher
	encoder *json.Encoder
}

func newEventStream(w http.ResponseWriter) *eventStream {
	w.Header().Set("Content-Type", ndjsonContentType)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	s := &eventStream{w: w, flusher: flusher, encoder: json.NewEncoder(w)}
	s.flush()
	return s
}

func (s *eventStream) send(event PairingEvent) {
	s.encoder.Encode(event)
	s.flush()
}

func (s *eventStream) flush() {
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

func accountError(err error) error {
	var failure *accounts.Error
	if !errors.As(err, &failure) {
		return err
	}
	return &apiError{status: accountStatus(failure.Kind), message: failure.Message}
}

func accountStatus(kind accounts.Kind) int {
	switch kind {
	case accounts.Invalid:
		return http.StatusBadRequest
	case accounts.Conflict:
		return http.StatusConflict
	case accounts.NotFound:
		return http.StatusNotFound
	}
	return http.StatusServiceUnavailable
}

func (h *handler) setAccess(r *http.Request) (any, error) {
	var req SetAccessRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	change, err := accessChange(r.PathValue("name"), req)
	if err != nil {
		return nil, err
	}
	grant, err := h.deps.Store.ChangeGrant(r.Context(), change)
	if err != nil {
		return nil, grantError(change.Name, err)
	}
	if change.ChangeAccounts {
		h.deps.Grants.GrantChanged(r.Context(), grant.ID)
	}
	h.auditGrantSettings(r.Context(), audit.ActionAccessSet, grant)
	view, err := h.accessView(r.Context(), grant)
	if err != nil {
		return nil, err
	}
	return accessResponse{Access: view}, nil
}

func accessChange(name string, req SetAccessRequest) (store.GrantChange, error) {
	changeAccounts := req.Accounts != nil || req.AllExistingAccounts
	if !changeAccounts && req.Approval == "" {
		return store.GrantChange{}, &apiError{status: http.StatusBadRequest, message: "the change needs accounts, an approval mode, or both"}
	}
	if changeAccounts {
		if err := validateAccountChoice(req.AllExistingAccounts, req.Accounts); err != nil {
			return store.GrantChange{}, err
		}
	}
	if err := validateApproval(req.Approval); err != nil {
		return store.GrantChange{}, err
	}
	return store.GrantChange{
		Name:                name,
		ChangeAccounts:      changeAccounts,
		AllExistingAccounts: req.AllExistingAccounts,
		AccountNames:        req.Accounts,
		Approval:            req.Approval,
	}, nil
}

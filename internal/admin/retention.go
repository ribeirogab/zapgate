package admin

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/ribeirogab/zapgate/internal/audit"
	"github.com/ribeirogab/zapgate/internal/config"
)

const useDefaultRetention = 0

func (h *handler) status(r *http.Request) (any, error) {
	list, err := h.deps.Accounts.List(r.Context())
	if err != nil {
		return nil, err
	}
	accounts := make([]AccountStatus, 0, len(list))
	for _, account := range list {
		messages, err := h.deps.Store.CountMessages(r.Context(), account.ID)
		if err != nil {
			return nil, err
		}
		mediaBytes, err := h.deps.Media.AccountBytes(account.ID)
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, AccountStatus{AccountView: h.accountView(account), Messages: messages, MediaBytes: mediaBytes})
	}
	return StatusView{Accounts: accounts, Retention: retentionView(h.deps.Retention.LastPass())}, nil
}

func (h *handler) setRetention(r *http.Request) (any, error) {
	var req SetRetentionRequest
	if err := decodeBody(r, &req); err != nil {
		return nil, err
	}
	if req.Months != useDefaultRetention && !config.ValidRetentionMonths(req.Months) {
		return nil, &apiError{status: http.StatusBadRequest, message: fmt.Sprintf("the retention must be from %d to %d months, or the default", config.MinRetentionMonths, config.MaxRetentionMonths)}
	}
	account, err := h.deps.Accounts.SetRetention(r.Context(), r.PathValue("name"), req.Months)
	if err != nil {
		return nil, accountError(err)
	}
	h.deps.Audit.AccountRetention(r.Context(), account.Name, retentionLabel(req.Months))
	return accountResponse{Account: h.accountView(account)}, nil
}

func retentionLabel(months int) string {
	if months == useDefaultRetention {
		return audit.RetentionDefault
	}
	return strconv.Itoa(months)
}

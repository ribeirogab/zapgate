package admin

import (
	"context"
	"net/http"
)

type passwordResponse struct {
	Password string `json:"password"`
}

func (h *handler) password(r *http.Request) (any, error) {
	h.deps.Audit.PasswordShown(r.Context())
	return passwordResponse{Password: h.deps.OwnerPassword}, nil
}

func (c *Client) Password(ctx context.Context) (string, error) {
	var out passwordResponse
	err := c.call(ctx, http.MethodGet, "/password", nil, &out)
	return out.Password, err
}

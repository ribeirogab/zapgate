package ingest

import (
	"context"

	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

type identities struct {
	ctx       context.Context
	directory whatsapp.Directory
	merge     func(lid, pn types.JID)
}

func (ids identities) phone(jid, alt types.JID) types.JID {
	resolved := whatsapp.ResolvePhone(ids.ctx, ids.directory, jid, alt)
	if jid.Server == types.HiddenUserServer && resolved.Server == types.DefaultUserServer {
		ids.merge(jid.ToNonAD(), resolved)
	}
	return resolved
}

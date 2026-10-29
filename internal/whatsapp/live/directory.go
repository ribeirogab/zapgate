package live

import (
	"context"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

func (c *client) contacts() (store.ContactStore, error) {
	if c.cli.Store.ID == nil || c.cli.Store.Contacts == nil {
		return nil, whatsapp.ErrNotPaired
	}
	return c.cli.Store.Contacts, nil
}

func (c *client) Contact(ctx context.Context, jid types.JID) (types.ContactInfo, error) {
	contacts, err := c.contacts()
	if err != nil {
		return types.ContactInfo{}, err
	}
	return contacts.GetContact(ctx, jid.ToNonAD())
}

func (c *client) Contacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	contacts, err := c.contacts()
	if err != nil {
		return nil, err
	}
	return contacts.GetAllContacts(ctx)
}

func (c *client) GroupInfo(ctx context.Context, group types.JID) (*types.GroupInfo, error) {
	if c.cli.Store.ID == nil {
		return nil, whatsapp.ErrNotPaired
	}
	return c.cli.GetGroupInfo(ctx, group)
}

func (c *client) PhoneForLID(ctx context.Context, lid types.JID) (types.JID, error) {
	pn, err := c.lids.GetPNForLID(ctx, lid.ToNonAD())
	if err != nil || pn.IsEmpty() {
		return types.JID{}, err
	}
	return pn.ToNonAD(), nil
}

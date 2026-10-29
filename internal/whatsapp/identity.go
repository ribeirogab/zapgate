package whatsapp

import (
	"context"

	"go.mau.fi/whatsmeow/types"
)

func DisplayName(contact types.ContactInfo) string {
	for _, name := range []string{contact.FullName, contact.BusinessName, contact.PushName} {
		if name != "" {
			return name
		}
	}
	return ""
}

func ResolvePhone(ctx context.Context, directory Directory, jid, alt types.JID) types.JID {
	jid = jid.ToNonAD()
	if jid.Server != types.HiddenUserServer {
		return jid
	}
	if alt.Server == types.DefaultUserServer {
		return alt.ToNonAD()
	}
	if directory == nil {
		return jid
	}
	if pn, err := directory.PhoneForLID(ctx, jid); err == nil && pn.Server == types.DefaultUserServer {
		return pn.ToNonAD()
	}
	return jid
}

func Phone(jid types.JID) string {
	if jid.Server != types.DefaultUserServer || jid.User == "" {
		return ""
	}
	return "+" + jid.User
}

const (
	MinPhoneDigits = 8
	MaxPhoneDigits = 15
)

func ValidPhoneDigits(digits string) bool {
	return len(digits) >= MinPhoneDigits && len(digits) <= MaxPhoneDigits && digits[0] != '0'
}

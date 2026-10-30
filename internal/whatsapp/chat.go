package whatsapp

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"go.mau.fi/whatsmeow/types"
)

var ErrNotPhoneOrJID = errors.New("the chat is not a JID or a phone number with the country code")

func ParseChat(ctx context.Context, directory Directory, value string) (types.JID, error) {
	value = strings.TrimSpace(value)
	if strings.Contains(value, "@") {
		return parseChatJID(ctx, directory, value)
	}
	digits, ok := PhoneDigits(value)
	if !ok || !ValidPhoneDigits(digits) {
		return types.JID{}, ErrNotPhoneOrJID
	}
	return types.NewJID(digits, types.DefaultUserServer), nil
}

func parseChatJID(ctx context.Context, directory Directory, value string) (types.JID, error) {
	jid, err := types.ParseJID(value)
	if err != nil {
		return types.JID{}, fmt.Errorf("the chat %q is not a valid JID: %v", value, err)
	}
	switch jid.Server {
	case types.DefaultUserServer, types.GroupServer:
		return jid.ToNonAD(), nil
	case types.HiddenUserServer:
		return ResolvePhone(ctx, directory, jid, types.JID{}), nil
	}
	return types.JID{}, fmt.Errorf("the chat %q is not a person or a group", value)
}

func PhoneDigits(value string) (string, bool) {
	var digits strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case strings.ContainsRune("+-() .", r):
		default:
			return "", false
		}
	}
	return digits.String(), true
}

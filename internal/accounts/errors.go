package accounts

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ribeirogab/zapgate/internal/names"
	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

type Kind int

const (
	Invalid Kind = iota + 1
	Conflict
	NotFound
	Unavailable
)

type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string { return e.Message }

func failure(kind Kind, format string, args ...any) *Error {
	return &Error{Kind: kind, Message: fmt.Sprintf(format, args...)}
}

const (
	maxNameChars        = 32
	maxDescriptionChars = 500
)

func ValidateName(name string) error {
	if !names.Valid(name, maxNameChars) {
		return failure(Invalid, "the account name %q is not valid: use lowercase letters, digits and single hyphens, at most %d characters", name, maxNameChars)
	}
	return nil
}

func phoneDigits(phone string) (string, error) {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if !whatsapp.ValidPhoneDigits(digits) || strings.ContainsAny(phone, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ") {
		return "", failure(Invalid, "the phone number %q is not valid: use the international number with the country code, for example +5511988887777", phone)
	}
	return digits, nil
}

func validateDescription(description string) error {
	if utf8.RuneCountInString(description) > maxDescriptionChars {
		return failure(Invalid, "the description is too long: at most %d characters", maxDescriptionChars)
	}
	return nil
}

func notFound(name string) *Error {
	return failure(NotFound, "there is no account named %q", name)
}

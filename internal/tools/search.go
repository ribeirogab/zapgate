package tools

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"github.com/ribeirogab/zapgate/internal/whatsapp"
)

const minSearchDigits = 3

var stripMarks = transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)

type search struct {
	text   string
	digits string
}

func newSearch(query string) search {
	s := search{text: fold(query)}
	if digits, ok := whatsapp.PhoneDigits(query); ok && len(digits) >= minSearchDigits {
		s.digits = digits
	}
	return s
}

func (s search) empty() bool {
	return s.text == ""
}

func (s search) matches(phoneDigits string, names ...string) bool {
	if s.digits != "" && strings.Contains(phoneDigits, s.digits) {
		return true
	}
	for _, name := range names {
		if name != "" && strings.Contains(fold(name), s.text) {
			return true
		}
	}
	return false
}

func fold(value string) string {
	folded, _, err := transform.String(stripMarks, value)
	if err != nil {
		folded = value
	}
	return strings.ToLower(strings.TrimSpace(folded))
}

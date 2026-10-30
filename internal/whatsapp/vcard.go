package whatsapp

import "strings"

const (
	vcardPhonePrefix  = "TEL"
	vcardWhatsAppAttr = "waid="
)

func VCardPhones(vcard string, limit int) []string {
	var phones []string
	for _, line := range strings.Split(strings.ReplaceAll(vcard, "\r\n", "\n"), "\n") {
		if len(phones) == limit {
			break
		}
		if phone, ok := vcardPhone(line); ok {
			phones = append(phones, phone)
		}
	}
	return phones
}

func vcardPhone(line string) (string, bool) {
	if !strings.HasPrefix(strings.ToUpper(line), vcardPhonePrefix) {
		return "", false
	}
	params, value, found := strings.Cut(line, ":")
	if !found {
		return "", false
	}
	for _, param := range strings.Split(params, ";") {
		if waid, ok := strings.CutPrefix(strings.ToLower(param), vcardWhatsAppAttr); ok && waid != "" {
			return "+" + waid, true
		}
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

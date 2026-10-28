package tools

import (
	"strings"

	"github.com/ribeirogab/zapgate/internal/store"
)

const (
	maxPollOptions    = 12
	maxContactCards   = 20
	maxPhonesPerCard  = 5
	vcardPhonePrefix  = "TEL"
	vcardWhatsAppAttr = "waid="
)

type pollView struct {
	Options    []string `json:"options" jsonschema:"the poll options"`
	MaxChoices int      `json:"max_choices" jsonschema:"how many options one person may choose; 0 means any number"`
}

type locationView struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Address   string  `json:"address,omitempty"`
	URL       string  `json:"url,omitempty"`
	Live      bool    `json:"live,omitempty" jsonschema:"true when the sender shared a live location; this is its first point"`
}

type contactCardView struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones,omitempty"`
}

func (c *thirdPartyContent) addDetails(details *store.Details) {
	if details == nil {
		return
	}
	if poll := details.Poll; poll != nil {
		c.Poll = &pollView{Options: cutAll(poll.Options, maxPollOptions), MaxChoices: poll.MaxChoices}
	}
	if place := details.Location; place != nil {
		c.Location = &locationView{Latitude: place.Latitude, Longitude: place.Longitude, Address: short(place.Address), URL: short(place.URL), Live: place.Live}
	}
	for i, card := range details.Contacts {
		if i == maxContactCards {
			break
		}
		c.Contacts = append(c.Contacts, contactCardView{Name: short(card.Name), Phones: vcardPhones(card.VCard)})
	}
}

func vcardPhones(vcard string) []string {
	var phones []string
	for _, line := range strings.Split(strings.ReplaceAll(vcard, "\r\n", "\n"), "\n") {
		if len(phones) == maxPhonesPerCard {
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
	return short(value), value != ""
}

func cutAll(values []string, limit int) []string {
	out := make([]string, 0, min(len(values), limit))
	for _, value := range values[:min(len(values), limit)] {
		out = append(out, short(value))
	}
	return out
}

func short(value string) string {
	cutValue, _ := cut(value, maxNameRunes)
	return cutValue
}

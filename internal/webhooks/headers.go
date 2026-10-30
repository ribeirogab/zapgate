package webhooks

import (
	"maps"
	"net/http"
	"slices"
	"strings"

	"golang.org/x/net/http/httpguts"

	"github.com/ribeirogab/zapgate/internal/store"
)

const reservedHeaderPrefix = "Webhook-"

var reservedHeaders = []string{"Content-Type", "Content-Length", "Host", "User-Agent"}

func parseHeaders(lines []string) (map[string]string, error) {
	headers := make(map[string]string, len(lines))
	for _, line := range lines {
		name, value, err := parseHeader(line)
		if err != nil {
			return nil, err
		}
		if _, given := headers[name]; given {
			return nil, invalid("the header %s is given twice", name)
		}
		headers[name] = value
	}
	return headers, nil
}

func parseHeader(line string) (string, string, error) {
	name, value, found := strings.Cut(line, ":")
	name, value = strings.TrimSpace(name), strings.TrimSpace(value)
	if !found || name == "" || value == "" {
		return "", "", invalid(`a header is not valid: use "<name>: <value>"`)
	}
	if !httpguts.ValidHeaderFieldName(name) {
		return "", "", invalid("the header name %q is not valid", name)
	}
	name = http.CanonicalHeaderKey(name)
	if !httpguts.ValidHeaderFieldValue(value) {
		return "", "", invalid("the value of the header %s is not valid", name)
	}
	if reservedHeader(name) {
		return "", "", invalid("the header %s is set by zapgate and cannot be replaced", name)
	}
	return name, value, nil
}

func reservedHeader(name string) bool {
	return slices.Contains(reservedHeaders, name) || strings.HasPrefix(name, reservedHeaderPrefix)
}

func HeaderNames(webhook store.Webhook) []string {
	return slices.Sorted(maps.Keys(webhook.Headers))
}

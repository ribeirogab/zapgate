package names

import "regexp"

var slug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func Valid(name string, maxChars int) bool {
	return len(name) <= maxChars && slug.MatchString(name)
}

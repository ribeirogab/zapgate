package sqlite

import (
	"strings"

	_ "modernc.org/sqlite"
)

const (
	Driver  = "sqlite"
	pragmas = "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)&_pragma=synchronous(NORMAL)&_txlock=immediate"
)

var uriEscapes = strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23")

func DSN(path string) string {
	return "file:" + uriEscapes.Replace(path) + pragmas
}

package sqlite_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ribeirogab/zapgate/internal/sqlite"
)

func TestTheDatabaseFileKeepsItsExactPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "odd#dir?name%41")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "app.db")
	db, err := sql.Open(sqlite.Driver, sqlite.DSN(path))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	if _, err := db.Exec(`CREATE TABLE probe (id INTEGER)`); err != nil {
		t.Fatalf("create a table: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("no database at %s: %v", path, err)
	}
	var foreignKeys int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&foreignKeys); err != nil || foreignKeys != 1 {
		t.Fatalf("foreign_keys %d, error %v", foreignKeys, err)
	}
}

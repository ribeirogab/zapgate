package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strconv"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

var embeddedMigrations = mustSub(migrationFiles, "migrations")

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

type migration struct {
	version int
	name    string
	body    string
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err)
	}
	return sub
}

func migrate(ctx context.Context, db *sql.DB, fsys fs.FS) error {
	migrations, err := loadMigrations(fsys)
	if err != nil {
		return err
	}
	if err := createMigrationTable(ctx, db); err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return err
	}
	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := apply(ctx, db, m); err != nil {
			return fmt.Errorf("migration %04d_%s: %w", m.version, m.name, err)
		}
	}
	return nil
}

func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var migrations []migration
	for _, entry := range entries {
		m, err := readMigration(fsys, entry.Name())
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, m)
	}
	slices.SortFunc(migrations, func(a, b migration) int { return a.version - b.version })
	return migrations, checkSequence(migrations)
}

func readMigration(fsys fs.FS, file string) (migration, error) {
	match := migrationName.FindStringSubmatch(path.Base(file))
	if match == nil {
		return migration{}, fmt.Errorf("migration file %q does not match NNNN_name.sql", file)
	}
	version, _ := strconv.Atoi(match[1])
	body, err := fs.ReadFile(fsys, file)
	if err != nil {
		return migration{}, err
	}
	return migration{version: version, name: match[2], body: string(body)}, nil
}

func checkSequence(migrations []migration) error {
	for i, m := range migrations {
		want := i + 1
		if m.version < want {
			return fmt.Errorf("two migrations share the number %04d", m.version)
		}
		if m.version > want {
			return fmt.Errorf("migration %04d is missing", want)
		}
	}
	return nil
}

func createMigrationTable(ctx context.Context, db *sql.DB) error {
	_, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  applied_at INTEGER NOT NULL
)`)
	return err
}

func appliedVersions(ctx context.Context, db *sql.DB) (map[int]bool, error) {
	rows, err := db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	applied := map[int]bool{}
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}
	return applied, rows.Err()
}

func apply(ctx context.Context, db *sql.DB, m migration) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, m.body); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`, m.version, m.name, millis(time.Now())); err != nil {
		return err
	}
	return tx.Commit()
}

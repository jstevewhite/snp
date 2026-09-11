package store

import (
	"context"
	"embed"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

var migrationRe = regexp.MustCompile(`^(\d+)_.*\.sql$`)

// migrate applies pending migrations in version order, each in its own
// transaction.
func (s *Store) migrate() error {
	ctx := context.Background()
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var current int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return err
	}
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	var names []string
	for _, e := range entries {
		if migrationRe.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		v, _ := strconv.Atoi(migrationRe.FindStringSubmatch(name)[1])
		if v <= current {
			continue
		}
		script, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(string(script)) {
			if _, err := tx.ExecContext(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %s: %w", name, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_version (version) VALUES (?)`, v); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// splitStatements splits a migration script into individual statements
// on semicolons. Semicolons inside single-quoted string literals or in
// `--` line comments do not terminate a statement.
func splitStatements(script string) []string {
	var out []string
	var cur strings.Builder
	inStr := false
	rs := []rune(script)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case inStr:
			cur.WriteRune(r)
			if r == '\'' {
				inStr = false
			}
		case r == '\'':
			inStr = true
			cur.WriteRune(r)
		case r == '-' && i+1 < len(rs) && rs[i+1] == '-':
			// line comment: skip to end of line, keep the newline
			for i < len(rs) && rs[i] != '\n' {
				i++
			}
			if i < len(rs) {
				cur.WriteRune('\n')
			}
		case r == ';':
			if s := strings.TrimSpace(cur.String()); s != "" {
				out = append(out, s)
			}
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	if s := strings.TrimSpace(cur.String()); s != "" {
		out = append(out, s)
	}
	return out
}

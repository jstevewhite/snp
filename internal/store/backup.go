package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Backup writes a consistent copy of the database to dest via
// VACUUM INTO, then verifies the copy with PRAGMA quick_check (or the
// full integrity_check if fullCheck). The destination must not exist;
// parent directories are created as needed. The copy is a standalone
// database file (no WAL sidecar), safe to move or restore directly.
func (s *Store) Backup(dest string, fullCheck bool) error {
	if _, err := os.Stat(dest); err == nil {
		return fmt.Errorf("backup destination %s already exists", dest)
	} else if !os.IsNotExist(err) {
		return err
	}
	if dir := filepath.Dir(dest); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	// VACUUM INTO takes a string literal, not a bound parameter.
	lit := "'" + strings.ReplaceAll(dest, "'", "''") + "'"
	if _, err := s.db.ExecContext(context.Background(), "VACUUM INTO "+lit); err != nil {
		os.Remove(dest) // never leave a partial file behind
		return fmt.Errorf("vacuum into %s: %w", dest, err)
	}
	return checkDatabase(dest, fullCheck)
}

// checkDatabase opens path read-only and runs the requested integrity
// pragma; anything other than "ok" is an error.
func checkDatabase(path string, full bool) error {
	pragma := "quick_check"
	if full {
		pragma = "integrity_check"
	}
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return err
	}
	defer db.Close()
	var out string
	if err := db.QueryRow("PRAGMA " + pragma).Scan(&out); err != nil {
		return err
	}
	if out != "ok" {
		return fmt.Errorf("integrity check on %s failed: %s", path, out)
	}
	return nil
}

// Package store implements snp's persistence layer: SQLite + FTS5,
// encryption at rest, sync, purge, and import/export (spec §4–§5).
package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// Clock abstracts time for testability.
type Clock interface{ Now() time.Time }

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Store is the persistence layer.
type Store struct {
	db    *sql.DB
	clock Clock
	key   *Key
}

// Open opens (creating if needed) the SQLite database at path and
// applies pending migrations.
func Open(path string) (*Store, error) {
	// busy_timeout comes FIRST: the journal-mode switch needs an
	// exclusive lock, and when another process (a stale desktop
	// instance) holds the database, a busy handler has to be active
	// before that switch runs or the open can stall for tens of
	// seconds instead of failing cleanly after 5s.
	dsn := "file:" + path +
		"?_pragma=busy_timeout(5000)" +
		"&_pragma=journal_mode(WAL)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, clock: realClock{}}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// SetClock overrides the clock (tests).
func (s *Store) SetClock(c Clock) { s.clock = c }

// SetKey sets the encryption key; required before any sensitive
// operation.
func (s *Store) SetKey(k *Key) { s.key = k }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// now returns the current storage timestamp: RFC3339 UTC, second
// precision, no fractions, always Z (spec §4).
func (s *Store) now() string {
	return s.clock.Now().UTC().Truncate(time.Second).Format("2006-01-02T15:04:05Z")
}

func (s *Store) requireKey() (*Key, error) {
	if s.key == nil {
		return nil, ErrNoKey
	}
	return s.key, nil
}

// Errors returned by the store; the server maps them to status codes
// (spec §8).
var (
	ErrNotFound       = errors.New("not found")
	ErrInvalid        = errors.New("invalid input")
	ErrInvalidTag     = errors.New("invalid tag name")
	ErrFolderNotEmpty = errors.New("folder is not empty")
	ErrFolderCycle    = errors.New("folder move would create a cycle")
	ErrNameTaken      = errors.New("name already exists under this parent")
	ErrFTS            = errors.New("invalid search expression")
	ErrImport         = errors.New("import error")
	ErrDecrypt        = errors.New("decryption failed")
	ErrNoKey          = errors.New("encryption key not set")
)

// boolInt renders a bool as a SQLite 0/1.
func boolInt(b bool) any {
	if b {
		return 1
	}
	return 0
}

// placeholders returns n comma-separated "?"s.
func placeholders(n int) string {
	return strings.TrimRight(strings.Repeat("?,", n), ",")
}

// toAny converts []string to []any for query args.
func toAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// dedupe removes empties and duplicates, preserving order.
func dedupe(ss []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

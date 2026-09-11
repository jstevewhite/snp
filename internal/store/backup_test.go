package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// openStoreWithKey opens a store and key in dir so tests that need to
// use the key on a second handle (e.g. a restored backup) can share it.
func openStoreWithKey(t *testing.T, dir string) (*Store, *Key) {
	t.Helper()
	s, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	s.SetClock(&fakeClock{t: baseTime})
	k, err := LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	s.SetKey(k)
	return s, k
}

func TestBackupRoundTrip(t *testing.T) {
	s, k := openStoreWithKey(t, t.TempDir())
	f, err := s.CreateFolder("ops", nil)
	if err != nil {
		t.Fatal(err)
	}
	plain := mustCreate(t, s, SnippetInput{
		Title: "a", Body: "plain body", Language: "bash",
		Notes: "note a", Tags: []string{"ops"}, FolderID: &f.ID,
	})
	secret := mustCreate(t, s, SnippetInput{Title: "b", Body: "topsecret", IsSensitive: true})

	dest := filepath.Join(t.TempDir(), "backups", "snp.db")
	if err := s.Backup(dest, false); err != nil {
		t.Fatalf("Backup: %v", err)
	}

	// The copy must be standalone: no WAL sidecar next to it.
	if _, err := os.Stat(dest + "-wal"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("unexpected WAL sidecar: %v", err)
	}

	// Restore drill: open the backup as a fresh store and diff live rows.
	b, err := Open(dest)
	if err != nil {
		t.Fatalf("Open backup: %v", err)
	}
	defer b.Close()
	b.SetKey(k)

	got, err := b.ListSnippets(ListFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("backup lists %d snippets, want 2", len(got))
	}
	byID := map[string]SnippetOut{}
	for _, sn := range got {
		byID[sn.ID] = sn
	}
	p, ok := byID[plain.ID]
	if !ok {
		t.Fatalf("plain snippet missing from backup: %+v", got)
	}
	if p.Title != "a" || p.Language != "bash" || p.Notes != "note a" {
		t.Errorf("plain = %+v", p)
	}
	if p.FolderID == nil || *p.FolderID != f.ID {
		t.Errorf("plain folder_id = %v", p.FolderID)
	}
	if len(p.Tags) != 1 || p.Tags[0] != "ops" {
		t.Errorf("plain tags = %v", p.Tags)
	}
	if p.Body == nil || *p.Body != "plain body" {
		t.Errorf("plain body = %v", p.Body)
	}
	se, ok := byID[secret.ID]
	if !ok {
		t.Fatalf("secret snippet missing from backup: %+v", got)
	}
	if !se.IsSensitive || se.Body != nil {
		t.Errorf("secret = %+v", se)
	}

	// Sensitive bodies must still be encrypted at rest in the copy,
	// and decryptable with the same key.
	raw, err := b.RawBody(secret.ID)
	if err != nil {
		t.Fatal(err)
	}
	if raw != "topsecret" {
		t.Errorf("raw body via backup: %q", raw)
	}
}

func TestBackupFullCheck(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "a", Body: "x"})
	dest := filepath.Join(t.TempDir(), "snp.db")
	if err := s.Backup(dest, true); err != nil {
		t.Fatalf("Backup(fullCheck): %v", err)
	}
}

func TestBackupDestExists(t *testing.T) {
	s, _ := newTestStore(t)
	dest := filepath.Join(t.TempDir(), "snp.db")
	if err := os.WriteFile(dest, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Backup(dest, false); err == nil {
		t.Fatal("expected error for existing destination")
	}
}

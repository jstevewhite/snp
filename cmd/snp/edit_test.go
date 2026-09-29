package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/pick"
)

// openTestEditor opens a lazy editor over a fresh database under cfg,
// with no key attached and no key file written.
func openTestEditor(t *testing.T, cfg config.Config) (pick.Editor, func()) {
	t.Helper()
	st, err := openStore(cfg, false)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	return &lazyKeyEditor{Editor: pick.Local{Store: st}, st: st, path: keyPath(cfg)}, func() { st.Close() }
}

func TestLazyKeyNeverWrittenForPlainWork(t *testing.T) {
	cfg := testConfig(t)
	ed, done := openTestEditor(t, cfg)
	defer done()
	ctx := context.Background()

	// A plain create and a read-back need no key.
	row, err := ed.Create(ctx, pick.Input{Title: "plain", Body: "echo hi"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := ed.Get(ctx, row.ID); err != nil {
		t.Fatalf("get: %v", err)
	}
	// A full-replace edit of a non-sensitive row rewrites the row
	// without a key too.
	if _, err := ed.Update(ctx, row.ID, pick.Input{Title: "plain2", Body: "echo hi", Pinned: true}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(keyPath(cfg)); !os.IsNotExist(err) {
		t.Fatalf("key file exists after plain work: %v", err)
	}
}

func TestLazyKeyAttachedOnDemand(t *testing.T) {
	cfg := testConfig(t)
	ed, done := openTestEditor(t, cfg)
	ctx := context.Background()

	// A sensitive create trips ErrNoKey, the wrapper creates the key,
	// retries, and the row lands encrypted.
	row, err := ed.Create(ctx, pick.Input{Title: "secret", Body: "hunter2", IsSensitive: true})
	if err != nil {
		t.Fatalf("sensitive create: %v", err)
	}
	if _, err := os.Stat(keyPath(cfg)); err != nil {
		t.Fatalf("key file missing after sensitive create: %v", err)
	}
	// A fresh editor (no key attached) reads it back — the key file
	// now exists, so attaching only loads it.
	ed2, done2 := openTestEditor(t, cfg)
	got, err := ed2.Get(ctx, row.ID)
	done2()
	if err != nil {
		t.Fatalf("get sensitive: %v", err)
	}
	if got.Body != "hunter2" {
		t.Fatalf("body = %q", got.Body)
	}
	// Black-box: the plaintext never sits in the database (WAL
	// included — the store is closed before the scan).
	done()
	for _, name := range []string{"snp.db", "snp.db-wal"} {
		data, err := os.ReadFile(filepath.Join(cfg.StateDir, name))
		if err == nil && strings.Contains(string(data), "hunter2") {
			t.Fatalf("%s contains the plaintext body", name)
		}
	}
}

func TestResolveFolderID(t *testing.T) {
	cfg := testConfig(t)
	ed, done := openTestEditor(t, cfg)
	defer done()
	ctx := context.Background()

	parent, err := ed.(*lazyKeyEditor).st.CreateFolder("ops", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := ed.(*lazyKeyEditor).st.CreateFolder("deploy", &parent.ID)
	if err != nil {
		t.Fatal(err)
	}
	byID, err := resolveFolderID(ctx, ed, child.ID)
	if err != nil || byID == nil || *byID != child.ID {
		t.Fatalf("by id = %v, %v", byID, err)
	}
	byPath, err := resolveFolderID(ctx, ed, "ops/deploy")
	if err != nil || byPath == nil || *byPath != child.ID {
		t.Fatalf("by path = %v, %v", byPath, err)
	}
	if id, err := resolveFolderID(ctx, ed, ""); err != nil || id != nil {
		t.Fatalf("empty spec = %v, %v", id, err)
	}
	if _, err := resolveFolderID(ctx, ed, "nope/deep"); err == nil || !strings.Contains(err.Error(), "nope/deep") {
		t.Fatalf("miss err = %v", err)
	}
}

func TestResolveTarget(t *testing.T) {
	cfg := testConfig(t)
	ed, done := openTestEditor(t, cfg)
	defer done()
	ctx := context.Background()

	secret, err := ed.Create(ctx, pick.Input{Title: "login", Body: "hunter2", IsSensitive: true})
	if err != nil {
		t.Fatalf("create sensitive: %v", err)
	}
	plain, err := ed.Create(ctx, pick.Input{Title: "unique caddy restart", Body: "sudo systemctl restart caddy"})
	if err != nil {
		t.Fatal(err)
	}

	// By id: a full row, sensitive body included.
	got, err := resolveTarget(ctx, ed, secret.ID)
	if err != nil || got.Body != "hunter2" {
		t.Fatalf("by id = %+v, %v", got, err)
	}
	// By query, one hit: the hit itself for a non-sensitive row.
	got, err = resolveTarget(ctx, ed, "unique caddy")
	if err != nil || got.ID != plain.ID || got.Body != "sudo systemctl restart caddy" {
		t.Fatalf("by query = %+v, %v", got, err)
	}
	// By query, one sensitive hit: fetched whole, key attached on demand.
	got, err = resolveTarget(ctx, ed, "login")
	if err != nil || got.ID != secret.ID || got.Body != "hunter2" {
		t.Fatalf("sensitive query = %+v, %v", got, err)
	}
	// No match is an error naming the query.
	if _, err := resolveTarget(ctx, ed, "nothing matches this"); err == nil || !strings.Contains(err.Error(), "nothing matches this") {
		t.Fatalf("miss = %v", err)
	}
}

func TestSplitTags(t *testing.T) {
	got := splitTags("ops, bash ,,")
	if len(got) != 2 || got[0] != "ops" || got[1] != "bash" {
		t.Fatalf("splitTags = %+v", got)
	}
	if splitTags("") != nil {
		t.Fatalf("empty = %+v", splitTags(""))
	}
}

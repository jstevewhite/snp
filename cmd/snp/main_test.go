package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/starter"
	"github.com/jstevewhite/snp/internal/store"
)

// testConfig returns a config pointing at a fresh temp state dir.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	return config.Config{
		Hostname: "snp",
		Owner:    "alice@example.com",
		StateDir: t.TempDir(),
		LogLevel: "error",
	}
}

// seedStore opens the store under cfg and seeds it with one folder and
// two snippets (one sensitive).
func seedStore(t *testing.T, cfg config.Config) *store.Store {
	t.Helper()
	st, err := openStore(cfg, true)
	if err != nil {
		t.Fatalf("openStore: %v", err)
	}
	t.Cleanup(func() { st.Close() })

	f, err := st.CreateFolder("shell", nil)
	if err != nil {
		t.Fatalf("CreateFolder: %v", err)
	}
	if _, err := st.CreateSnippet(store.SnippetInput{
		Title: "plain", Body: "plain body", Language: "bash",
		FolderID: &f.ID, Tags: []string{"ops"},
	}); err != nil {
		t.Fatalf("CreateSnippet plain: %v", err)
	}
	if _, err := st.CreateSnippet(store.SnippetInput{
		Title: "secret", Body: "top secret", IsSensitive: true,
	}); err != nil {
		t.Fatalf("CreateSnippet secret: %v", err)
	}
	return st
}

func TestBackupCmd(t *testing.T) {
	cfg := testConfig(t)
	seed := seedStore(t, cfg)
	seed.Close() // the backup must open its own connection

	dest := filepath.Join(t.TempDir(), "bak.db")
	if err := backupCmd(cfg, dest, false); err != nil {
		t.Fatalf("backupCmd: %v", err)
	}

	// The backup must be a consistent, readable copy of the source.
	st, err := store.Open(dest)
	if err != nil {
		t.Fatalf("open backup: %v", err)
	}
	defer st.Close()
	k, err := store.LoadOrCreateKey(keyPath(cfg))
	if err != nil {
		t.Fatalf("load key: %v", err)
	}
	st.SetKey(k)
	doc, err := st.Export()
	if err != nil {
		t.Fatalf("export backup: %v", err)
	}
	if len(doc.Folders) != 1 || len(doc.Snippets) != 2 {
		t.Fatalf("backup content: got %d folders, %d snippets", len(doc.Folders), len(doc.Snippets))
	}
	for _, sn := range doc.Snippets {
		if sn.IsSensitive && (sn.Body == nil || *sn.Body != "top secret") {
			t.Fatalf("sensitive body not decrypted in backup: %v", sn.Body)
		}
	}
}

func TestBackupCmdNoDatabase(t *testing.T) {
	cfg := testConfig(t) // fresh state dir, no database
	dest := filepath.Join(t.TempDir(), "bak.db")
	err := backupCmd(cfg, dest, false)
	if err == nil {
		t.Fatal("backupCmd: expected error when no database exists")
	}
	if !strings.Contains(err.Error(), "no database") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExportCmd(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)

	var buf bytes.Buffer
	if err := exportCmd(cfg, &buf); err != nil {
		t.Fatalf("exportCmd: %v", err)
	}
	var doc store.ExportDoc
	if err := json.Unmarshal(buf.Bytes(), &doc); err != nil {
		t.Fatalf("decode export: %v", err)
	}
	if doc.Version != 1 {
		t.Fatalf("version = %d, want 1", doc.Version)
	}
	if len(doc.Folders) != 1 || doc.Folders[0].Name != "shell" {
		t.Fatalf("folders = %+v", doc.Folders)
	}
	if len(doc.Snippets) != 2 {
		t.Fatalf("snippets = %d, want 2", len(doc.Snippets))
	}
	// Bodies must be plaintext, including the sensitive one.
	bodies := map[string]string{}
	for _, sn := range doc.Snippets {
		b := ""
		if sn.Body != nil {
			b = *sn.Body
		}
		bodies[sn.Title] = b
	}
	if bodies["plain"] != "plain body" || bodies["secret"] != "top secret" {
		t.Fatalf("bodies = %v", bodies)
	}
	if !strings.Contains(buf.String(), "\n  ") {
		t.Fatal("export is not pretty-printed")
	}
}

func TestImportCmd(t *testing.T) {
	cfg := testConfig(t)
	folderID := "01J9TESTFOLDER000000000000"
	doc := store.ImportDoc{
		Version: 1,
		Folders: []store.ImportFolder{{ID: folderID, Name: "shell"}},
		Snippets: []store.ImportSnippet{
			{ID: "01J9TESTSNIPA0000000000000", Title: "plain", Body: "plain body", FolderPath: "shell", Tags: []string{"ops"}},
			{ID: "01J9TESTSNIPB0000000000000", Title: "secret", Body: "top secret", IsSensitive: true},
		},
	}
	path := filepath.Join(t.TempDir(), "import.json")
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := importCmd(cfg, path, "merge")
	if err != nil {
		t.Fatalf("importCmd: %v", err)
	}
	if res.Created != 2 || res.Updated != 0 {
		t.Fatalf("first import = %+v, want {2 0}", res)
	}

	// Re-importing the same document is idempotent.
	res, err = importCmd(cfg, path, "merge")
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if res.Created != 0 || res.Updated != 2 {
		t.Fatalf("re-import = %+v, want {0 2}", res)
	}

	// Verify the imported data: folder_path resolved to the document's
	// folder, sensitive body re-encrypted then decrypted.
	st, err := openStore(cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Folders) != 1 || got.Folders[0].Name != "shell" {
		t.Fatalf("folders = %+v", got.Folders)
	}
	if len(got.Snippets) != 2 {
		t.Fatalf("snippets = %d, want 2", len(got.Snippets))
	}
	for _, sn := range got.Snippets {
		switch sn.Title {
		case "plain":
			if sn.FolderID == nil || *sn.FolderID != folderID {
				t.Fatalf("plain folder = %v, want %s", sn.FolderID, folderID)
			}
		case "secret":
			if sn.Body == nil || *sn.Body != "top secret" {
				t.Fatalf("secret body = %v", sn.Body)
			}
		}
	}
}

func TestExportImportRoundTrip(t *testing.T) {
	src := testConfig(t)
	seedStore(t, src)

	var buf bytes.Buffer
	if err := exportCmd(src, &buf); err != nil {
		t.Fatalf("exportCmd: %v", err)
	}
	var exp store.ExportDoc
	if err := json.Unmarshal(buf.Bytes(), &exp); err != nil {
		t.Fatalf("decode export: %v", err)
	}

	// Map the export document to an import document.
	doc := store.ImportDoc{Version: exp.Version}
	for _, f := range exp.Folders {
		doc.Folders = append(doc.Folders, store.ImportFolder{ID: f.ID, ParentID: f.ParentID, Name: f.Name})
	}
	for _, sn := range exp.Snippets {
		doc.Snippets = append(doc.Snippets, store.ImportSnippet{
			ID: sn.ID, Title: sn.Title, Body: derefStr(sn.Body),
			Language: sn.Language, Notes: sn.Notes, FolderID: sn.FolderID,
			Tags: sn.Tags, IsSensitive: sn.IsSensitive,
			CreatedAt: sn.CreatedAt, UpdatedAt: sn.UpdatedAt,
		})
	}

	// Import into a fresh state dir (fresh key: sensitive bodies are
	// re-encrypted under the new key, same ids).
	dst := testConfig(t)
	path := filepath.Join(t.TempDir(), "roundtrip.json")
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	res, err := importCmd(dst, path, "merge")
	if err != nil {
		t.Fatalf("importCmd: %v", err)
	}
	if res.Created != 2 || res.Updated != 0 {
		t.Fatalf("round-trip import = %+v, want {2 0}", res)
	}

	st, err := openStore(dst, true)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	got, err := st.Export()
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Folders) != len(exp.Folders) || len(got.Snippets) != len(exp.Snippets) {
		t.Fatalf("round-trip counts: %d folders/%d snippets, want %d/%d",
			len(got.Folders), len(got.Snippets), len(exp.Folders), len(exp.Snippets))
	}
	want := map[string]store.SnippetOut{}
	for _, sn := range exp.Snippets {
		want[sn.ID] = sn
	}
	for _, sn := range got.Snippets {
		w, ok := want[sn.ID]
		if !ok {
			t.Fatalf("unexpected snippet %s after round trip", sn.ID)
		}
		if sn.Title != w.Title || sn.Language != w.Language || sn.Notes != w.Notes ||
			sn.IsSensitive != w.IsSensitive || !samePtrStr(sn.FolderID, w.FolderID) ||
			derefStr(sn.Body) != derefStr(w.Body) || sn.CreatedAt != w.CreatedAt ||
			sn.UpdatedAt != w.UpdatedAt {
			t.Fatalf("snippet %s mismatch after round trip:\n got %+v\nwant %+v", sn.ID, sn, w)
		}
	}
}

func derefStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func samePtrStr(a, b *string) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && *a == *b)
}

func TestSeedCmd(t *testing.T) {
	cfg := testConfig(t)
	doc, err := starter.Pack()
	if err != nil {
		t.Fatal(err)
	}

	res, err := seedCmd(cfg)
	if err != nil {
		t.Fatalf("seedCmd: %v", err)
	}
	if res.Created != len(doc.Snippets) || res.Updated != 0 {
		t.Fatalf("first seed = %+v, want %d created", res, len(doc.Snippets))
	}

	// Running it again updates in place rather than duplicating.
	res, err = seedCmd(cfg)
	if err != nil {
		t.Fatalf("seedCmd (second): %v", err)
	}
	if res.Created != 0 || res.Updated != len(doc.Snippets) {
		t.Errorf("second seed = %+v, want 0 created / %d updated", res, len(doc.Snippets))
	}
}

func TestImportCmdErrors(t *testing.T) {
	cfg := testConfig(t)

	// Malformed JSON.
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := importCmd(cfg, bad, "merge"); err == nil {
		t.Fatal("importCmd: expected error for malformed JSON")
	}

	// Unsupported mode.
	doc := filepath.Join(t.TempDir(), "doc.json")
	contents := `{"version":1,"snippets":[{"id":"01J9TESTSNIPA0000000000000","title":"x","body":"y"}]}`
	if err := os.WriteFile(doc, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := importCmd(cfg, doc, "bogus"); err == nil {
		t.Fatal("importCmd: expected error for bad mode")
	}
}

func TestDevListenAddr(t *testing.T) {
	ok := map[string]string{
		":8080":          "127.0.0.1:8080", // empty host must not bind every interface
		"127.0.0.1:8080": "127.0.0.1:8080",
		"127.0.0.2:8080": "127.0.0.2:8080", // any /8 loopback is fine
		"localhost:8080": "127.0.0.1:8080",
		"[::1]:8080":     "[::1]:8080",
	}
	for in, want := range ok {
		got, err := devListenAddr(in)
		if err != nil {
			t.Errorf("devListenAddr(%q): unexpected error %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("devListenAddr(%q) = %q, want %q", in, got, want)
		}
	}
	bad := []string{
		"0.0.0.0:8080",
		"192.168.1.5:8080",
		"example.com:8080",
		"8080", // missing port
		"",
	}
	for _, in := range bad {
		if got, err := devListenAddr(in); err == nil {
			t.Errorf("devListenAddr(%q) = %q, want an error (non-loopback)", in, got)
		}
	}
}

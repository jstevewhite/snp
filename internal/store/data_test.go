package store

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestBackupArchiveRestoresHistoryTrashAndKey(t *testing.T) {
	s, _ := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "secret", Body: "old-secret", IsSensitive: true})
	mustReplace(t, s, a.ID, SnippetInput{Title: "secret", Body: "new-secret", IsSensitive: true})
	b := mustCreate(t, s, SnippetInput{Title: "trashed", Body: "recover me"})
	if err := s.SoftDeleteSnippet(b.ID); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "backup.zip")
	if err := s.BackupArchive(dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dest)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("archive permissions: %v %v", info, err)
	}
	zr, err := zip.OpenReader(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	dir := t.TempDir()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if f.Name != "snp.db" && f.Name != "key" && f.Name != "RESTORE.txt" {
			t.Fatalf("unexpected entry: %s", f.Name)
		}
		reader, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatal(err)
		}
		if f.Name == "snp.db" && bytes.Contains(data, []byte("old-secret")) {
			t.Fatal("plaintext sensitive history in backup database")
		}
		if err := os.WriteFile(filepath.Join(dir, f.Name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if len(names) != 3 {
		t.Fatal(names)
	}
	restored, err := Open(filepath.Join(dir, "snp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	key, err := LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	restored.SetKey(key)
	got, err := restored.GetSnippet(a.ID)
	if err != nil || *got.Body != "new-secret" {
		t.Fatalf("current: %+v %v", got, err)
	}
	revisions := mustRevisions(t, restored, a.ID)
	if len(revisions) != 1 {
		t.Fatal(revisions)
	}
	prior, err := restored.GetRevision(a.ID, revisions[0].ID)
	if err != nil || *prior.Snippet.Body != "old-secret" {
		t.Fatalf("history: %+v %v", prior, err)
	}
	trash, err := restored.ListTrash(100, 0)
	if err != nil || len(trash) != 1 || trash[0].ID != b.ID {
		t.Fatalf("trash: %+v %v", trash, err)
	}
	original, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.BackupArchive(dest); err == nil {
		t.Fatal("overwrote archive")
	}
	after, _ := os.ReadFile(dest)
	if !bytes.Equal(original, after) {
		t.Fatal("changed existing archive")
	}
}

func TestImportPreviewRollsBackAllChanges(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "old", Body: "before"})
	b := mustCreate(t, s, SnippetInput{Title: "other", Body: "keep until apply"})
	fc.t = fc.t.Add(time.Hour)
	doc := ImportDoc{Version: 1, Folders: []ImportFolder{{ID: "new-folder", Name: "Imported"}}, Snippets: []ImportSnippet{{ID: a.ID, Title: "updated", Body: "after", IsSensitive: true}, {Title: "new", Body: "new", FolderID: strPtr("new-folder")}}}
	before, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.PreviewImport(doc, "replace")
	if err != nil {
		t.Fatal(err)
	}
	if result.Created != 1 || result.Updated != 1 || result.Trashed != 1 {
		t.Fatal(result)
	}
	after, err := s.Export()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("preview changed data: %v", err)
	}
	if len(mustRevisions(t, s, a.ID)) != 0 {
		t.Fatal("preview wrote history")
	}
	applied, err := s.Import(doc, "replace")
	if err != nil || applied != result {
		t.Fatalf("apply: %+v %v", applied, err)
	}
	if _, err := s.GetSnippet(b.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if len(mustRevisions(t, s, a.ID)) != 1 {
		t.Fatal("apply missing revision")
	}
}

func TestImportOldDocumentAppearsInIncrementalSync(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "before", Body: "before"})
	sync, err := s.SyncSince("")
	if err != nil {
		t.Fatal(err)
	}
	fc.t = fc.t.Add(time.Hour)
	doc := ImportDoc{Version: 1, Snippets: []ImportSnippet{{ID: a.ID, Title: "updated", Body: "after", CreatedAt: "2020-01-01T06:00:00.123+06:00", UpdatedAt: "2020-01-01T00:00:00Z"}, {Title: "new", Body: "new", UpdatedAt: "2020-01-01T00:00:00Z"}}}
	if _, err := s.Import(doc, "merge"); err != nil {
		t.Fatal(err)
	}
	delta, err := s.SyncSince(sync.ServerTime)
	if err != nil || len(delta.Snippets) != 2 {
		t.Fatalf("import invisible to sync: %+v %v", delta, err)
	}
	got, err := s.GetSnippet(a.ID)
	if err != nil || got.CreatedAt != "2020-01-01T00:00:00Z" || got.UpdatedAt != ts(fc.t) {
		t.Fatalf("timestamps: %+v %v", got, err)
	}
}

func TestImportPreviewRejectsCombinedFolderCycleAndKeepsPathAncestors(t *testing.T) {
	s, _ := newTestStore(t)
	a, err := s.CreateFolder("a", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateFolder("b", &a.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PreviewImport(ImportDoc{Version: 1, Folders: []ImportFolder{{ID: a.ID, Name: "a", ParentID: &b.ID}}}, "merge")
	if !errors.Is(err, ErrImport) {
		t.Fatalf("cycle allowed: %v", err)
	}
	doc := ImportDoc{Version: 1, Snippets: []ImportSnippet{{Title: "nested", Body: "body", FolderPath: "x/y/z"}}}
	if _, err := s.Import(doc, "replace"); err != nil {
		t.Fatal(err)
	}
	folders, err := s.ListFolders()
	if err != nil || len(folders) != 3 {
		t.Fatalf("ancestors lost: %+v %v", folders, err)
	}
}

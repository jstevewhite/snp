package store

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"
)

func mustReplace(t *testing.T, s *Store, id string, in SnippetInput) SnippetOut {
	t.Helper()
	out, err := s.ReplaceSnippet(id, in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func mustRevisions(t *testing.T, s *Store, id string) []Revision {
	t.Helper()
	out, err := s.ListRevisions(id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTrashRestoreSearchAndSync(t *testing.T) {
	s, fc := newTestStore(t)
	f, err := s.CreateFolder("old", nil)
	if err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, s, SnippetInput{Title: "Recover", Body: "uniqueneedle", FolderID: &f.ID, Tags: []string{"ops"}, Pinned: true})
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}
	trash, err := s.ListTrash(1, 0)
	if err != nil || len(trash) != 1 || trash[0].ID != a.ID {
		t.Fatalf("trash: %+v %v", trash, err)
	}
	if _, err := s.GetSnippet(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	found, err := s.ListSnippets(ListFilter{Q: "uniqueneedle"})
	if err != nil || len(found) != 0 {
		t.Fatalf("deleted search: %+v %v", found, err)
	}
	if err := s.DeleteFolder(f.ID); err != nil {
		t.Fatal(err)
	}
	fc.t = fc.t.Add(time.Hour)
	out, err := s.RestoreSnippet(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.FolderID != nil || !out.Pinned || out.CreatedAt != a.CreatedAt || out.UpdatedAt != ts(fc.t) {
		t.Fatalf("restored: %+v", out)
	}
	sync, err := s.SyncSince(ts(fc.t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sync.Snippets) != 1 {
		t.Fatalf("same-second sync: %+v", sync)
	}
	if _, ok := sync.Snippets[0].(SnippetOut); !ok {
		t.Fatal("still a tombstone")
	}
	found, err = s.ListSnippets(ListFilter{Q: "uniqueneedle"})
	if err != nil || len(found) != 1 {
		t.Fatalf("restore search: %+v %v", found, err)
	}
	mustReplace(t, s, a.ID, SnippetInput{Title: "Recover", Body: "newneedle"})
	found, err = s.ListSnippets(ListFilter{Q: "uniqueneedle"})
	if err != nil || len(found) != 0 {
		t.Fatalf("stale FTS: %+v %v", found, err)
	}
	if _, err := s.RestoreSnippet(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestRevisionsRestoreFullContentAndKeepCurrent(t *testing.T) {
	s, _ := newTestStore(t)
	f, err := s.CreateFolder("folder", nil)
	if err != nil {
		t.Fatal(err)
	}
	old := SnippetInput{Title: "original", Body: "oldneedle {{host}}", Language: "bash", Notes: "original notes", FolderID: &f.ID, Tags: []string{"ops"}, UsesVariables: true, VarDefaults: map[string]string{"host": "example"}}
	a := mustCreate(t, s, old)
	next := SnippetInput{Title: "new", Body: "newneedle", Pinned: true}
	mustReplace(t, s, a.ID, next)
	revisions := mustRevisions(t, s, a.ID)
	if len(revisions) != 1 {
		t.Fatal(revisions)
	}
	// Removing the original folder makes restoration fall back to Unfiled.
	if err := s.DeleteFolder(f.ID); err != nil {
		t.Fatal(err)
	}
	out, err := s.RestoreRevision(a.ID, revisions[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Title != old.Title || *out.Body != old.Body || out.Notes != old.Notes || out.Language != old.Language || out.FolderID != nil || !out.Pinned || !out.UsesVariables || out.VarDefaults["host"] != "example" || len(out.Tags) != 1 {
		t.Fatalf("restored: %+v", out)
	}
	revisions = mustRevisions(t, s, a.ID)
	if len(revisions) != 2 {
		t.Fatal(revisions)
	}
	r, err := s.GetRevision(a.ID, revisions[0].ID)
	if err != nil || *r.Snippet.Body != next.Body {
		t.Fatalf("current version lost: %+v %v", r, err)
	}
	found, err := s.ListSnippets(ListFilter{Q: "oldneedle"})
	if err != nil || len(found) != 1 {
		t.Fatalf("FTS restore: %+v %v", found, err)
	}
	b := mustCreate(t, s, SnippetInput{Title: "other"})
	if _, err := s.RestoreRevision(b.ID, revisions[0].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross snippet revision: %v", err)
	}
}

func TestRevisionRetentionAndNoop(t *testing.T) {
	s, _ := newTestStore(t)
	in := SnippetInput{Title: "test", Body: "0", Tags: []string{"ops", "cli"}}
	a := mustCreate(t, s, in)
	in.Tags = []string{"CLI", "ops", "ops"}
	in.Pinned = true
	in.VarDefaults = map[string]string{}
	mustReplace(t, s, a.ID, in)
	if len(mustRevisions(t, s, a.ID)) != 0 {
		t.Fatal("pin/no-op creates history")
	}
	for i := 1; i <= 55; i++ {
		in.Body = fmt.Sprint(i)
		mustReplace(t, s, a.ID, in)
	}
	revisions := mustRevisions(t, s, a.ID)
	if len(revisions) != 50 {
		t.Fatal(len(revisions))
	}
	r, err := s.GetRevision(a.ID, revisions[49].ID)
	if err != nil || *r.Snippet.Body != "5" {
		t.Fatalf("retention: %+v %v", r, err)
	}
	// All saves used the same clock tick; IDs must still order deterministically.
	for i := 1; i < len(revisions); i++ {
		if revisions[i].ID >= revisions[i-1].ID {
			t.Fatal("bad order")
		}
	}
}

func TestSensitiveRevisionPromotionAndRestore(t *testing.T) {
	s, _ := newTestStore(t)
	in := SnippetInput{Title: "secret", Body: "secretmarker", VarDefaults: map[string]string{"x": "defaultmarker"}}
	a := mustCreate(t, s, in)
	in.Body = "secondmarker"
	mustReplace(t, s, a.ID, in)
	in.IsSensitive = true
	mustReplace(t, s, a.ID, in)
	revisions := mustRevisions(t, s, a.ID)
	for _, r := range revisions {
		if !r.Protected {
			t.Fatal("plaintext revision after sensitivity enabled")
		}
		var payload []byte
		if err := s.db.QueryRow(`SELECT payload FROM snippet_revisions WHERE id=?`, r.ID).Scan(&payload); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(payload, []byte("marker")) {
			t.Fatal("plaintext stored")
		}
		if _, err := s.GetRevision(a.ID, r.ID); err != nil {
			t.Fatal(err)
		}
	}
	in.IsSensitive = false
	mustReplace(t, s, a.ID, in)
	out, err := s.RestoreRevision(a.ID, revisions[len(revisions)-1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !out.IsSensitive || out.Body != nil || out.VarDefaults != nil {
		t.Fatalf("restore published protected content: %+v", out)
	}
	revealed, err := s.GetSnippet(a.ID)
	if err != nil || *revealed.Body != "secretmarker" || revealed.VarDefaults["x"] != "defaultmarker" {
		t.Fatalf("revealed: %+v %v", revealed, err)
	}
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}
	out, err = s.RestoreSnippet(a.ID)
	if err != nil || out.Body != nil || out.VarDefaults != nil {
		t.Fatalf("trash restore leaked: %+v %v", out, err)
	}
}

func TestImportHistoryRollbackAndPurge(t *testing.T) {
	s, fc := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "original", Body: "original"})
	doc := ImportDoc{Version: 1, Snippets: []ImportSnippet{{ID: a.ID, Title: "imported", Body: "new"}}}
	if _, err := s.Import(doc, "merge"); err != nil {
		t.Fatal(err)
	}
	revisions := mustRevisions(t, s, a.ID)
	if len(revisions) != 1 {
		t.Fatal(revisions)
	}
	doc.Snippets[0].Body = "should roll back"
	doc.Snippets = append(doc.Snippets, ImportSnippet{Title: "invalid", Tags: []string{"!"}})
	if _, err := s.Import(doc, "merge"); err == nil {
		t.Fatal("invalid import succeeded")
	}
	if len(mustRevisions(t, s, a.ID)) != 1 {
		t.Fatal("history escaped rollback")
	}
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}
	fc.t = fc.t.Add(31 * 24 * time.Hour)
	if _, err := s.Purge(); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM snippet_revisions`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("orphan history: %d %v", n, err)
	}
	if _, err := s.RestoreSnippet(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestPurgeDefersFolderWithRecentTrash(t *testing.T) {
	s, fc := newTestStore(t)
	f, err := s.CreateFolder("folder", nil)
	if err != nil {
		t.Fatal(err)
	}
	a := mustCreate(t, s, SnippetInput{Title: "one", FolderID: &f.ID})
	if err := s.SoftDeleteSnippet(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteFolder(f.ID); err != nil {
		t.Fatal(err)
	}
	// A newer deletion can be retained longer than its deleted folder.
	fc.t = fc.t.Add(29 * 24 * time.Hour)
	if _, err := s.db.Exec(`UPDATE snippets SET deleted_at=? WHERE id=?`, s.now(), a.ID); err != nil {
		t.Fatal(err)
	}
	fc.t = fc.t.Add(2 * 24 * time.Hour)
	c, err := s.Purge()
	if err != nil || c.Folders != 0 || c.Snippets != 0 {
		t.Fatalf("purge: %+v %v", c, err)
	}
	out, err := s.RestoreSnippet(a.ID)
	if err != nil || out.FolderID != nil {
		t.Fatalf("restore: %+v %v", out, err)
	}
	c, err = s.Purge()
	if err != nil || c.Folders != 1 {
		t.Fatalf("folder purge: %+v %v", c, err)
	}
}

func TestRevisionCiphertextBoundToRevisionAndSnippet(t *testing.T) {
	s, _ := newTestStore(t)
	a := mustCreate(t, s, SnippetInput{Title: "a", Body: "one", IsSensitive: true})
	mustReplace(t, s, a.ID, SnippetInput{Title: "a", Body: "two", IsSensitive: true})
	mustReplace(t, s, a.ID, SnippetInput{Title: "a", Body: "three", IsSensitive: true})
	revisions := mustRevisions(t, s, a.ID)
	if _, err := s.db.Exec(`UPDATE snippet_revisions SET payload=(SELECT payload FROM snippet_revisions WHERE id=?) WHERE id=?`, revisions[0].ID, revisions[1].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRevision(a.ID, revisions[1].ID); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("moved ciphertext decrypted: %v", err)
	}
	b := mustCreate(t, s, SnippetInput{Title: "b", Body: "one", IsSensitive: true})
	mustReplace(t, s, b.ID, SnippetInput{Title: "b", Body: "two", IsSensitive: true})
	other := mustRevisions(t, s, b.ID)
	if _, err := s.db.Exec(`UPDATE snippet_revisions SET payload=(SELECT payload FROM snippet_revisions WHERE id=?) WHERE id=?`, revisions[0].ID, other[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetRevision(b.ID, other[0].ID); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("cross-snippet ciphertext decrypted: %v", err)
	}
}

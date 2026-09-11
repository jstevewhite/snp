package store

import (
	"errors"
	"testing"
)

func TestImportMerge(t *testing.T) {
	s, _ := newTestStore(t)
	x := mustCreate(t, s, SnippetInput{Title: "x", Body: "x1"})
	y := mustCreate(t, s, SnippetInput{Title: "y", Body: "y1"})
	doc := ImportDoc{
		Version: 1,
		Snippets: []ImportSnippet{
			{ID: y.ID, Title: "y2", Body: "y2"},
			{Title: "z", Body: "z"},
		},
	}
	res, err := s.Import(doc, "merge")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 || res.Updated != 1 {
		t.Errorf("result = %+v", res)
	}
	gx, err := s.GetSnippet(x.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gx.Title != "x" {
		t.Errorf("x was modified: %q", gx.Title)
	}
	gy, _ := s.GetSnippet(y.ID)
	if gy.Title != "y2" {
		t.Errorf("y not updated: %q", gy.Title)
	}
	list, _ := s.ListSnippets(ListFilter{Limit: 200})
	if len(list) != 3 {
		t.Errorf("count = %d, want 3", len(list))
	}
}

func TestImportReplace(t *testing.T) {
	s, _ := newTestStore(t)
	x := mustCreate(t, s, SnippetInput{Title: "x", Body: "x1"})
	y := mustCreate(t, s, SnippetInput{Title: "y", Body: "y1"})
	doc := ImportDoc{
		Version:  1,
		Snippets: []ImportSnippet{{ID: y.ID, Title: "y2", Body: "y2"}},
	}
	if _, err := s.Import(doc, "replace"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSnippet(x.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("x should be soft-deleted, got %v", err)
	}
	gy, err := s.GetSnippet(y.ID)
	if err != nil {
		t.Fatal(err)
	}
	if gy.Title != "y2" {
		t.Errorf("y not updated: %q", gy.Title)
	}
	list, _ := s.ListSnippets(ListFilter{Limit: 200})
	if len(list) != 1 {
		t.Errorf("count = %d, want 1", len(list))
	}
}

func TestImportReplaceDeletesOrphanFolders(t *testing.T) {
	s, _ := newTestStore(t)
	keep, _ := s.CreateFolder("keep", nil)
	orphan, _ := s.CreateFolder("orphan", nil)
	doc := ImportDoc{
		Version: 1,
		Folders: []ImportFolder{{ID: keep.ID, Name: "keep"}},
	}
	if _, err := s.Import(doc, "replace"); err != nil {
		t.Fatal(err)
	}
	list, _ := s.ListFolders()
	if len(list) != 1 || list[0].ID != keep.ID {
		t.Errorf("folders = %+v (orphan=%s)", list, orphan.ID)
	}
}

func TestImportFolderPath(t *testing.T) {
	s, _ := newTestStore(t)
	doc := ImportDoc{
		Version:  1,
		Snippets: []ImportSnippet{{Title: "t", Body: "b", FolderPath: "shell/deploy"}},
	}
	if _, err := s.Import(doc, "merge"); err != nil {
		t.Fatal(err)
	}
	folders, _ := s.ListFolders()
	if len(folders) != 2 {
		t.Fatalf("folders = %+v", folders)
	}
	var shell, deploy *Folder
	for i := range folders {
		switch folders[i].Name {
		case "shell":
			shell = &folders[i]
		case "deploy":
			deploy = &folders[i]
		}
	}
	if shell == nil || deploy == nil || deploy.ParentID == nil || *deploy.ParentID != shell.ID {
		t.Fatalf("folder tree wrong: %+v", folders)
	}
	list, _ := s.ListSnippets(ListFilter{Limit: 200})
	if len(list) != 1 || list[0].FolderID == nil || *list[0].FolderID != deploy.ID {
		t.Errorf("snippet folder = %v", list)
	}

	// A second import with the same path reuses the folders.
	doc2 := ImportDoc{
		Version:  1,
		Snippets: []ImportSnippet{{Title: "t2", Body: "b2", FolderPath: "shell/deploy"}},
	}
	if _, err := s.Import(doc2, "merge"); err != nil {
		t.Fatal(err)
	}
	folders, _ = s.ListFolders()
	if len(folders) != 2 {
		t.Errorf("folders after reuse = %+v", folders)
	}
}

func TestImportDuplicateIDs(t *testing.T) {
	s, _ := newTestStore(t)
	doc := ImportDoc{
		Version: 1,
		Snippets: []ImportSnippet{
			{ID: "dup", Title: "a", Body: "a"},
			{ID: "dup", Title: "b", Body: "b"},
		},
	}
	if _, err := s.Import(doc, "merge"); !errors.Is(err, ErrImport) {
		t.Errorf("expected ErrImport, got %v", err)
	}
}

func TestImportBadVersion(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.Import(ImportDoc{Version: 2}, "merge"); !errors.Is(err, ErrImport) {
		t.Errorf("expected ErrImport, got %v", err)
	}
}

func TestImportTimestampsPreserved(t *testing.T) {
	s, _ := newTestStore(t)
	x := mustCreate(t, s, SnippetInput{Title: "x", Body: "x1"})
	doc := ImportDoc{
		Version: 1,
		Snippets: []ImportSnippet{
			{ID: x.ID, Title: "x2", Body: "x2", CreatedAt: "2020-01-01T00:00:00Z", UpdatedAt: "2021-01-01T00:00:00Z"},
		},
	}
	if _, err := s.Import(doc, "merge"); err != nil {
		t.Fatal(err)
	}
	gx, _ := s.GetSnippet(x.ID)
	if gx.CreatedAt != "2020-01-01T00:00:00Z" || gx.UpdatedAt != "2021-01-01T00:00:00Z" {
		t.Errorf("timestamps = %q / %q", gx.CreatedAt, gx.UpdatedAt)
	}
}

func TestImportSensitiveReencrypted(t *testing.T) {
	s, _ := newTestStore(t)
	doc := ImportDoc{
		Version:  1,
		Snippets: []ImportSnippet{{Title: "secret", Body: "hunter2secretbody", IsSensitive: true}},
	}
	res, err := s.Import(doc, "merge")
	if err != nil {
		t.Fatal(err)
	}
	if res.Created != 1 {
		t.Errorf("result = %+v", res)
	}
	list, _ := s.ListSnippets(ListFilter{Limit: 200})
	if len(list) != 1 {
		t.Fatalf("list = %+v", list)
	}
	id := list[0].ID
	if !list[0].IsSensitive || list[0].Body != nil {
		t.Errorf("list entry = %+v", list[0])
	}
	// The body is not indexed.
	if got, _ := s.ListSnippets(ListFilter{Q: "hunter2secretbody"}); len(got) != 0 {
		t.Error("sensitive body is searchable")
	}
	full, err := s.GetSnippet(id)
	if err != nil {
		t.Fatal(err)
	}
	if full.Body == nil || *full.Body != "hunter2secretbody" {
		t.Errorf("decrypted body = %v", full.Body)
	}
}

func TestImportUnknownFolderID(t *testing.T) {
	s, _ := newTestStore(t)
	doc := ImportDoc{
		Version:  1,
		Snippets: []ImportSnippet{{Title: "t", Body: "b", FolderID: strPtr("nope")}},
	}
	if _, err := s.Import(doc, "merge"); !errors.Is(err, ErrImport) {
		t.Errorf("expected ErrImport, got %v", err)
	}
}

func TestExport(t *testing.T) {
	s, _ := newTestStore(t)
	f, _ := s.CreateFolder("ops", nil)
	mustCreate(t, s, SnippetInput{Title: "a", Body: "plain", Tags: []string{"ops"}, FolderID: &f.ID})
	mustCreate(t, s, SnippetInput{Title: "b", Body: "topsecret", IsSensitive: true})

	doc, err := s.Export()
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.ExportedAt == "" {
		t.Errorf("header = %+v", doc)
	}
	if len(doc.Folders) != 1 || doc.Folders[0].ID != f.ID {
		t.Errorf("folders = %+v", doc.Folders)
	}
	if len(doc.Snippets) != 2 {
		t.Fatalf("snippets = %+v", doc.Snippets)
	}
	var plain, secret *SnippetOut
	for i := range doc.Snippets {
		switch doc.Snippets[i].Title {
		case "a":
			plain = &doc.Snippets[i]
		case "b":
			secret = &doc.Snippets[i]
		}
	}
	if plain == nil || secret == nil {
		t.Fatal("snippets missing")
	}
	if *plain.Body != "plain" || len(plain.Tags) != 1 || plain.Tags[0] != "ops" {
		t.Errorf("plain = %+v", plain)
	}
	if *secret.Body != "topsecret" {
		t.Errorf("secret body not decrypted: %+v", secret)
	}
}

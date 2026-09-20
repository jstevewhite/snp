package store

import (
	"errors"
	"reflect"
	"testing"
)

func TestExportImportNestedFolders(t *testing.T) {
	for _, mode := range []string{"merge", "replace"} {
		t.Run(mode, func(t *testing.T) {
			source, _ := newTestStore(t)
			parent, err := source.CreateFolder("Work", nil)
			if err != nil {
				t.Fatal(err)
			}
			child, err := source.CreateFolder("bash", &parent.ID)
			if err != nil {
				t.Fatal(err)
			}
			leaf, err := source.CreateFolder("awk", &child.ID)
			if err != nil {
				t.Fatal(err)
			}
			snippet := mustCreate(t, source, SnippetInput{
				Title: "nested", Body: "echo {{name}}", FolderID: &leaf.ID,
				Tags: []string{"shell"}, UsesVariables: true, Pinned: true,
				VarDefaults: map[string]string{"name": "world"},
			})
			doc, err := source.Export()
			if err != nil {
				t.Fatal(err)
			}
			if len(doc.Folders) != 3 || doc.Folders[0].ID != leaf.ID || doc.Folders[2].ID != parent.ID {
				t.Fatalf("expected child-before-parent export: %+v", doc.Folders)
			}
			imp, err := jsonMarshalImportDoc(doc)
			if err != nil {
				t.Fatal(err)
			}
			target, _ := newTestStore(t)
			// Exercise both insertion into an empty store and updating existing rows.
			for attempt := 0; attempt < 2; attempt++ {
				res, err := target.Import(imp, mode)
				if err != nil {
					t.Fatalf("import attempt %d: %v", attempt, err)
				}
				if res.Created != 1-attempt || res.Updated != attempt {
					t.Fatalf("import result: %+v", res)
				}
				folders, err := target.ListFolders()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(folders, doc.Folders) {
					t.Fatalf("folder tree changed: got %+v, want %+v", folders, doc.Folders)
				}
				got, err := target.GetSnippet(snippet.ID)
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, snippet) {
					t.Fatalf("snippet changed: got %+v, want %+v", got, snippet)
				}
			}
		})
	}
}

func TestImportFolderWithExistingParent(t *testing.T) {
	s, _ := newTestStore(t)
	parent, err := s.CreateFolder("Work", nil)
	if err != nil {
		t.Fatal(err)
	}
	doc := ImportDoc{Version: 1, Folders: []ImportFolder{
		{ID: "child", Name: "bash", ParentID: &parent.ID},
	}}
	if _, err := s.Import(doc, "merge"); err != nil {
		t.Fatal(err)
	}
	folders, err := s.ListFolders()
	if err != nil || len(folders) != 2 || folders[0].ParentID == nil || *folders[0].ParentID != parent.ID {
		t.Fatalf("folder tree: %+v, error: %v", folders, err)
	}
}

func TestImportInvalidFolderDependencies(t *testing.T) {
	cases := map[string][]ImportFolder{
		"duplicate":      {{ID: "a", Name: "a"}, {ID: "a", Name: "again"}},
		"self cycle":     {{ID: "a", Name: "a", ParentID: strPtr("a")}},
		"cycle":          {{ID: "a", Name: "a", ParentID: strPtr("b")}, {ID: "b", Name: "b", ParentID: strPtr("a")}},
		"missing parent": {{ID: "a", Name: "a", ParentID: strPtr("missing")}},
	}
	for name, folders := range cases {
		t.Run(name, func(t *testing.T) {
			for _, mode := range []string{"merge", "replace"} {
				t.Run(mode, func(t *testing.T) {
					s, _ := newTestStore(t)
					keep, err := s.CreateFolder("keep", nil)
					if err != nil {
						t.Fatal(err)
					}
					doc := ImportDoc{Version: 1, Folders: append([]ImportFolder{
						{ID: keep.ID, Name: "changed"}, {ID: "new", Name: "new"},
					}, folders...)}
					if _, err := s.Import(doc, mode); !errors.Is(err, ErrImport) {
						t.Fatalf("expected ErrImport, got %v", err)
					}
					got, err := s.ListFolders()
					if err != nil || !reflect.DeepEqual(got, []Folder{keep}) {
						t.Fatalf("failed import changed folders: %+v, error: %v", got, err)
					}
				})
			}
		})
	}
}

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

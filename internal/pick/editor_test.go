package pick

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jstevewhite/snp/internal/store"
)

func newLocalLib(t *testing.T) Local {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "snp.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	key, err := store.LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatal(err)
	}
	st.SetKey(key)
	return Local{Store: st}
}

func TestLocalCreateAndUpdate(t *testing.T) {
	lib := newLocalLib(t)
	ctx := context.Background()

	parent, err := lib.Store.CreateFolder("ops", nil)
	if err != nil {
		t.Fatal(err)
	}
	row, err := lib.Create(ctx, Input{
		Title: "restart", Body: "sudo systemctl restart caddy",
		Language: "bash", Tags: []string{"ops"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID == "" || row.Title != "restart" || row.Body != "sudo systemctl restart caddy" {
		t.Fatalf("create = %+v", row)
	}

	// A full replace must keep the fields the panel did not touch.
	folderID := parent.ID
	got, err := lib.Update(ctx, row.ID, Input{
		Title: row.Title, Body: row.Body, Language: row.Language,
		Notes: row.Notes, Tags: row.Tags, FolderID: &folderID,
		Pinned: true, VarDefaults: map[string]string{"host": "nas"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.FolderID == nil || *got.FolderID != folderID || !got.Pinned {
		t.Fatalf("update dropped fields: %+v", got)
	}
	if got.VarDefaults["host"] != "nas" {
		t.Fatalf("var_defaults = %+v", got.VarDefaults)
	}

	// Get reads the same fields back.
	full, err := lib.Get(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.FolderID == nil || *full.FolderID != folderID || !full.Pinned {
		t.Fatalf("get = %+v", full)
	}
}

func TestLocalFoldersAndTags(t *testing.T) {
	lib := newLocalLib(t)
	ctx := context.Background()
	if _, err := lib.Create(ctx, Input{Title: "a", Body: "x", Tags: []string{"ops", "bash"}}); err != nil {
		t.Fatal(err)
	}
	folders, err := lib.Folders(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 0 {
		t.Fatalf("folders = %+v", folders)
	}
	tags, err := lib.Tags(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[0].Count != 1 {
		t.Fatalf("tags = %+v", tags)
	}
}

func TestLocalValidation(t *testing.T) {
	lib := newLocalLib(t)
	ctx := context.Background()

	// The store's tag grammar rejects a space (spec §4); the error must
	// surface as the typed error the panel will show inline.
	if _, err := lib.Create(ctx, Input{Title: "x", Body: "b", Tags: []string{"bad tag"}}); !errors.Is(err, store.ErrInvalidTag) {
		t.Fatalf("create err = %v, want ErrInvalidTag", err)
	}
	// ReplaceSnippet fails with ErrNotFound before any write.
	if _, err := lib.Update(ctx, "no-such-id", Input{Title: "x", Body: "b"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("update err = %v, want ErrNotFound", err)
	}
}

func TestHTTPSearchAndRevealKeepsNewFields(t *testing.T) {
	folder := "f1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := "x"
		json.NewEncoder(w).Encode(store.SnippetOut{
			ID: "a", Title: "t", Body: &body, FolderID: &folder, Pinned: true,
		})
	}))
	defer srv.Close()
	lib, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	row, err := lib.Reveal(context.Background(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if row.FolderID == nil || *row.FolderID != "f1" || !row.Pinned {
		t.Fatalf("reveal dropped fields: %+v", row)
	}
}

func TestHTTPCreateAndUpdate(t *testing.T) {
	var seen snippetReq
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/snippets", func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		body := seen.Body
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(store.SnippetOut{
			ID: "new1", Title: seen.Title, Body: &body, Tags: seen.Tags,
		})
	})
	mux.HandleFunc("PUT /api/snippets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "new1" {
			http.NotFound(w, r)
			return
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Fatal(err)
		}
		body := seen.Body
		json.NewEncoder(w).Encode(store.SnippetOut{
			ID: "new1", Title: seen.Title, Body: &body, FolderID: seen.FolderID,
			Pinned: seen.Pinned, VarDefaults: seen.VarDefaults,
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	lib, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	row, err := lib.Create(ctx, Input{Title: "t", Body: "b", Tags: []string{"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	if row.ID != "new1" || row.Body != "b" {
		t.Fatalf("create = %+v", row)
	}

	folder := "f1"
	got, err := lib.Update(ctx, "new1", Input{
		Title: "t", Body: "b", FolderID: &folder, Pinned: true,
		VarDefaults: map[string]string{"h": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.FolderID == nil || *got.FolderID != "f1" || !got.Pinned {
		t.Fatalf("update = %+v", got)
	}
	if got.VarDefaults["h"] != "1" {
		t.Fatalf("var_defaults = %+v", got.VarDefaults)
	}
	// The PUT carried the whole row: the server saw folder_id and pinned.
	if seen.FolderID == nil || *seen.FolderID != "f1" || !seen.Pinned {
		t.Fatalf("server saw %+v", seen)
	}
}

func TestHTTPCreateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid: title required"}`))
	}))
	defer srv.Close()
	lib, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lib.Create(context.Background(), Input{})
	if err == nil || err.Error() != "library: invalid: title required" {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPFoldersAndTags(t *testing.T) {
	parent := "p1"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/folders":
			json.NewEncoder(w).Encode([]store.Folder{
				{ID: "p1", Name: "ops"},
				{ID: "c1", ParentID: &parent, Name: "deploy"},
			})
		case "/api/tags":
			json.NewEncoder(w).Encode([]store.TagCount{{Name: "ops", Count: 2}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	lib, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	folders, err := lib.Folders(context.Background())
	if err != nil || len(folders) != 2 {
		t.Fatalf("folders = %+v, %v", folders, err)
	}
	if folders[1].ParentID == nil || *folders[1].ParentID != "p1" {
		t.Fatalf("child = %+v", folders[1])
	}
	tags, err := lib.Tags(context.Background())
	if err != nil || len(tags) != 1 || tags[0].Name != "ops" || tags[0].Count != 2 {
		t.Fatalf("tags = %+v, %v", tags, err)
	}
}

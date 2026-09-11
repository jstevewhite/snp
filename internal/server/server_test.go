package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	fstest "testing/fstest"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/buildinfo"
	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/starter"
	"github.com/jstevewhite/snp/internal/store"
	"github.com/jstevewhite/snp/internal/tsauth"
)

// fakeResolver returns a fixed identity (or error) for every WhoIs call.
type fakeResolver struct {
	id  tsauth.Identity
	err error
}

func (f *fakeResolver) WhoIs(ctx context.Context, remoteAddr string) (tsauth.Identity, error) {
	return f.id, f.err
}

const jsonCT = "application/json"

// newTestServer builds a Server backed by a temp-file store and a fresh
// encryption key. owner "" disables the owner check.
func newTestServer(t *testing.T, owner string, resolver tsauth.IdentityResolver) *Server {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	k, err := store.LoadOrCreateKey(filepath.Join(dir, "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	st.SetKey(k)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(st, resolver, owner, log)
}

// doReq issues a request against h. body is JSON-marshalled when non-nil;
// contentType, when non-empty, is set on the request.
func doReq(t *testing.T, h http.Handler, method, target string, body any, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, target, reader)
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// createSnippet is a convenience that POSTs a snippet and returns its id.
func createSnippet(t *testing.T, h http.Handler, body map[string]any) string {
	t.Helper()
	w := doReq(t, h, "POST", "/api/snippets", body, jsonCT)
	if w.Code != http.StatusCreated {
		t.Fatalf("create snippet: got %d, body %s", w.Code, w.Body.String())
	}
	var out store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal created snippet: %v", err)
	}
	return out.ID
}

func TestAuth(t *testing.T) {
	const owner = "alice@example.com"

	// Owner is accepted.
	h := newTestServer(t, owner, &fakeResolver{id: tsauth.Identity{Login: "alice@example.com", DisplayName: "Alice"}}).Handler()
	w := doReq(t, h, "GET", "/api/me", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("owner: got %d, want 200", w.Code)
	}
	var m meOut
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal me: %v", err)
	}
	if m.Login != "alice@example.com" || m.DisplayName != "Alice" {
		t.Errorf("me = %+v", m)
	}

	// A different login is rejected with 403 and an error field.
	h2 := newTestServer(t, owner, &fakeResolver{id: tsauth.Identity{Login: "bob@example.com"}}).Handler()
	w = doReq(t, h2, "GET", "/api/me", nil, "")
	if w.Code != http.StatusForbidden {
		t.Fatalf("other login: got %d, want 403", w.Code)
	}
	var e map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &e); err != nil {
		t.Fatalf("unmarshal 403 body: %v", err)
	}
	if e["error"] == "" {
		t.Error("403 body should carry an error field")
	}

	// A whois failure is a 500.
	h3 := newTestServer(t, owner, &fakeResolver{err: errors.New("whois down")}).Handler()
	w = doReq(t, h3, "GET", "/api/me", nil, "")
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("whois error: got %d, want 500", w.Code)
	}
}

func TestContentType415(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	// POST without a JSON content type.
	w := doReq(t, h, "POST", "/api/snippets", map[string]any{"title": "x", "body": "y"}, "text/plain")
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("POST text/plain: got %d, want 415", w.Code)
	}
	// PUT without a content type.
	w = doReq(t, h, "PUT", "/api/snippets/someid", map[string]any{"title": "x", "body": "y"}, "")
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("PUT no content-type: got %d, want 415", w.Code)
	}
	// DELETE without a content type (state-changing, so it must be JSON).
	w = doReq(t, h, "DELETE", "/api/snippets/someid", nil, "")
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("DELETE no content-type: got %d, want 415", w.Code)
	}
	// GET does not require a JSON content type.
	w = doReq(t, h, "GET", "/api/snippets", nil, "")
	if w.Code != http.StatusOK {
		t.Errorf("GET: got %d, want 200", w.Code)
	}
	// A JSON content type with a charset parameter is accepted.
	w = doReq(t, h, "POST", "/api/snippets", map[string]any{"title": "x", "body": "y"}, "application/json; charset=utf-8")
	if w.Code != http.StatusCreated {
		t.Errorf("POST json+charset: got %d, want 201", w.Code)
	}
}

func TestBodyTooLarge413(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	// A body just over the 10 MiB cap.
	big := make([]byte, 10<<20+1)
	for i := range big {
		big[i] = 'a'
	}
	r := httptest.NewRequest(http.MethodPost, "/api/snippets", bytes.NewReader(big))
	r.Header.Set("Content-Type", jsonCT)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: got %d, want 413", w.Code)
	}
}

func TestMe(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local", DisplayName: "Dev"}}).Handler()
	w := doReq(t, h, "GET", "/api/me", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var m meOut
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Login != "dev@local" || m.DisplayName != "Dev" {
		t.Errorf("got %+v", m)
	}
}

// TestVersion covers /api/version: the SPA header shows this string, so
// it must track the value stamped into the binary (internal/buildinfo).
func TestVersion(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	// Unstamped (the test binary) reports "dev".
	w := doReq(t, h, "GET", "/api/version", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("got %d", w.Code)
	}
	var v versionOut
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Version != "dev" {
		t.Errorf("unstamped: got %q, want %q", v.Version, "dev")
	}

	// Stamped: the handler reads the link-time value at request time, so
	// handle h (built above) reports it, like a release binary would.
	buildinfo.Version = "v9.9.9-test"
	t.Cleanup(func() { buildinfo.Version = "" })
	w = doReq(t, h, "GET", "/api/version", nil, "")
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v.Version != "v9.9.9-test" {
		t.Errorf("stamped: got %q", v.Version)
	}
}

func TestSnippetCRUD(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	// Create.
	id := createSnippet(t, h, map[string]any{
		"title": "restart caddy", "body": "sudo systemctl restart caddy",
		"language": "bash", "tags": []string{"ops"},
	})

	// Get returns the decrypted body.
	w := doReq(t, h, "GET", "/api/snippets/"+id, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: got %d", w.Code)
	}
	var got store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Title != "restart caddy" || got.Body == nil || *got.Body != "sudo systemctl restart caddy" {
		t.Errorf("get: %+v", got)
	}

	// Replace preserves created_at and updates the rest.
	created := got.CreatedAt
	w = doReq(t, h, "PUT", "/api/snippets/"+id, map[string]any{
		"title": "restart caddy v2", "body": "sudo systemctl restart caddy", "language": "bash",
	}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("replace: got %d, body %s", w.Code, w.Body.String())
	}
	var replaced store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &replaced); err != nil {
		t.Fatalf("unmarshal replaced: %v", err)
	}
	if replaced.Title != "restart caddy v2" {
		t.Errorf("replace title = %q", replaced.Title)
	}
	if replaced.CreatedAt != created {
		t.Errorf("created_at changed: %q -> %q", created, replaced.CreatedAt)
	}

	// Delete.
	w = doReq(t, h, "DELETE", "/api/snippets/"+id, nil, jsonCT)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", w.Code)
	}

	// Get after delete is a 404.
	w = doReq(t, h, "GET", "/api/snippets/"+id, nil, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("get after delete: got %d, want 404", w.Code)
	}
}

// TestSnippetVarDefaults: the API carries per-variable defaults (spec
// §4/§5); list responses hide them for sensitive snippets, while a
// single read returns them decrypted.
func TestSnippetVarDefaults(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	// Non-sensitive: create with defaults and read them back.
	id := createSnippet(t, h, map[string]any{
		"title": "deploy", "body": "kubectl -n {{ns}} get pods",
		"uses_variables": true,
		"var_defaults":   map[string]string{"ns": "prod"},
	})
	w := doReq(t, h, "GET", "/api/snippets/"+id, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get: got %d", w.Code)
	}
	var got store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.VarDefaults["ns"] != "prod" {
		t.Errorf("get defaults: %+v", got.VarDefaults)
	}

	// Replace updates the defaults.
	w = doReq(t, h, "PUT", "/api/snippets/"+id, map[string]any{
		"title": "deploy", "body": "kubectl -n {{ns}} get pods",
		"uses_variables": true,
		"var_defaults":   map[string]string{"ns": "staging"},
	}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("replace: got %d, body %s", w.Code, w.Body.String())
	}
	var replaced store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &replaced); err != nil {
		t.Fatalf("unmarshal replaced: %v", err)
	}
	if replaced.VarDefaults["ns"] != "staging" {
		t.Errorf("replace defaults: %+v", replaced.VarDefaults)
	}

	// Sensitive: the list hides the defaults (and the body); a single read
	// returns them decrypted.
	sid := createSnippet(t, h, map[string]any{
		"title": "secret", "body": "curl -H 'Authorization: Bearer {{token}}'",
		"is_sensitive":   true,
		"uses_variables": true,
		"var_defaults":   map[string]string{"token": "s3cret"},
	})
	w = doReq(t, h, "GET", "/api/snippets", nil, "")
	var list []store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	for _, sn := range list {
		if sn.ID == sid {
			if sn.Body != nil || sn.VarDefaults != nil {
				t.Errorf("list leaked sensitive data: body=%v defaults=%v", sn.Body, sn.VarDefaults)
			}
		}
	}
	w = doReq(t, h, "GET", "/api/snippets/"+sid, nil, "")
	var secret store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &secret); err != nil {
		t.Fatalf("unmarshal secret: %v", err)
	}
	if secret.VarDefaults["token"] != "s3cret" {
		t.Errorf("get sensitive defaults: %+v", secret.VarDefaults)
	}
}

func TestSnippetListFilters(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	// A folder with one snippet in it.
	w := doReq(t, h, "POST", "/api/folders", map[string]any{"name": "Ops"}, jsonCT)
	if w.Code != http.StatusCreated {
		t.Fatalf("create folder: %d", w.Code)
	}
	var ops store.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &ops); err != nil {
		t.Fatalf("unmarshal folder: %v", err)
	}

	a := createSnippet(t, h, map[string]any{"title": "alpha", "body": "body of alpha", "language": "go", "tags": []string{"x"}})
	b := createSnippet(t, h, map[string]any{"title": "beta", "body": "body of beta", "language": "python", "tags": []string{"y"}})
	c := createSnippet(t, h, map[string]any{"title": "gamma", "body": "body of gamma", "language": "go", "tags": []string{"x"}, "folder_id": ops.ID})
	_ = b

	list := func(target string) []store.SnippetOut {
		t.Helper()
		w := doReq(t, h, "GET", target, nil, "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: got %d, body %s", target, w.Code, w.Body.String())
		}
		var out []store.SnippetOut
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("unmarshal list: %v", err)
		}
		return out
	}

	// FTS term.
	out := list("/api/snippets?q=alpha")
	if len(out) != 1 || out[0].ID != a {
		t.Errorf("q=alpha: got %d results, want 1 (a)", len(out))
	}
	// Language filter.
	out = list("/api/snippets?lang=go")
	if len(out) != 2 {
		t.Errorf("lang=go: got %d, want 2", len(out))
	}
	// Tag filter.
	out = list("/api/snippets?tag=x")
	if len(out) != 2 {
		t.Errorf("tag=x: got %d, want 2", len(out))
	}
	// Folder filter.
	out = list("/api/snippets?folder=" + ops.ID)
	if len(out) != 1 || out[0].ID != c {
		t.Errorf("folder filter: got %d, want 1 (c)", len(out))
	}
	// Pagination.
	out = list("/api/snippets?limit=2")
	if len(out) != 2 {
		t.Errorf("limit=2: got %d, want 2", len(out))
	}
	out = list("/api/snippets?limit=2&offset=2")
	if len(out) != 1 {
		t.Errorf("limit=2&offset=2: got %d, want 1", len(out))
	}
}

func TestFTSFallback(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	// An unbalanced quote is invalid FTS5 syntax as a raw expression, but
	// the store retries it as a quoted phrase, so the API returns 200, not 400.
	w := doReq(t, h, "GET", "/api/snippets?q=foo%22", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("fts fallback: got %d, want 200", w.Code)
	}
}

func TestRawBody(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	id := createSnippet(t, h, map[string]any{"title": "run", "body": "echo hi"})

	w := doReq(t, h, "GET", "/api/snippets/"+id+"/raw", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("raw: got %d", w.Code)
	}
	if got := w.Body.String(); got != "echo hi" {
		t.Errorf("raw body = %q, want %q", got, "echo hi")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain", ct)
	}
}

func TestFolders(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	// Top-level folder.
	w := doReq(t, h, "POST", "/api/folders", map[string]any{"name": "Work"}, jsonCT)
	if w.Code != http.StatusCreated {
		t.Fatalf("create folder: got %d, body %s", w.Code, w.Body.String())
	}
	var work store.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &work); err != nil {
		t.Fatalf("unmarshal folder: %v", err)
	}

	// Child folder.
	w = doReq(t, h, "POST", "/api/folders", map[string]any{"name": "Projects", "parent_id": work.ID}, jsonCT)
	if w.Code != http.StatusCreated {
		t.Fatalf("create child: got %d, body %s", w.Code, w.Body.String())
	}
	var proj store.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &proj); err != nil {
		t.Fatalf("unmarshal child: %v", err)
	}
	if proj.ParentID == nil || *proj.ParentID != work.ID {
		t.Errorf("child parent = %v, want %s", proj.ParentID, work.ID)
	}

	// List shows both.
	w = doReq(t, h, "GET", "/api/folders", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: got %d", w.Code)
	}
	var folders []store.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &folders); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(folders) != 2 {
		t.Errorf("folders = %d, want 2", len(folders))
	}

	// Sibling name collision (case-insensitive) is 409.
	w = doReq(t, h, "POST", "/api/folders", map[string]any{"name": "work"}, jsonCT)
	if w.Code != http.StatusConflict {
		t.Errorf("collision: got %d, want 409", w.Code)
	}

	// Rename.
	w = doReq(t, h, "PUT", "/api/folders/"+work.ID, map[string]any{"name": "Work2"}, jsonCT)
	if w.Code != http.StatusOK {
		t.Errorf("rename: got %d, body %s", w.Code, w.Body.String())
	}

	// Moving a folder under itself is a cycle (400).
	w = doReq(t, h, "PUT", "/api/folders/"+proj.ID, map[string]any{"parent_id": proj.ID}, jsonCT)
	if w.Code != http.StatusBadRequest {
		t.Errorf("self-cycle: got %d, want 400", w.Code)
	}

	// Deleting a folder with a live child is 409.
	w = doReq(t, h, "DELETE", "/api/folders/"+work.ID, nil, jsonCT)
	if w.Code != http.StatusConflict {
		t.Errorf("delete non-empty: got %d, want 409", w.Code)
	}

	// Deleting an empty folder is 204.
	w = doReq(t, h, "DELETE", "/api/folders/"+proj.ID, nil, jsonCT)
	if w.Code != http.StatusNoContent {
		t.Errorf("delete empty: got %d, want 204", w.Code)
	}

	// The deleted folder is gone from the list.
	w = doReq(t, h, "GET", "/api/folders", nil, "")
	if err := json.Unmarshal(w.Body.Bytes(), &folders); err != nil {
		t.Fatalf("unmarshal list: %v", err)
	}
	if len(folders) != 1 {
		t.Errorf("after delete, folders = %d, want 1", len(folders))
	}

	// Deleting an unknown folder is 404.
	w = doReq(t, h, "DELETE", "/api/folders/nonexistent", nil, jsonCT)
	if w.Code != http.StatusNotFound {
		t.Errorf("delete unknown: got %d, want 404", w.Code)
	}
}

// TestFolderMoveValidation: moving a folder under a missing or
// soft-deleted parent is 404 (not a 500 from an unmapped FK error), and
// moving into a sibling-name collision is 409.
func TestFolderMoveValidation(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	post := func(name string, parent any) store.Folder {
		t.Helper()
		body := map[string]any{"name": name}
		if parent != nil {
			body["parent_id"] = parent
		}
		w := doReq(t, h, "POST", "/api/folders", body, jsonCT)
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: got %d, body %s", name, w.Code, w.Body.String())
		}
		var f store.Folder
		if err := json.Unmarshal(w.Body.Bytes(), &f); err != nil {
			t.Fatalf("unmarshal %s: %v", name, err)
		}
		return f
	}
	a := post("a", nil)
	b := post("b", a.ID)
	gone := post("gone", nil)

	// Delete b (empty), then try to move "gone" under it: 404.
	w := doReq(t, h, "DELETE", "/api/folders/"+b.ID, nil, jsonCT)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete b: got %d", w.Code)
	}
	w = doReq(t, h, "PUT", "/api/folders/"+gone.ID, map[string]any{"parent_id": b.ID}, jsonCT)
	if w.Code != http.StatusNotFound {
		t.Errorf("move under deleted parent: got %d, want 404 (body %s)", w.Code, w.Body.String())
	}
	// Unknown parent: 404, not 500.
	w = doReq(t, h, "PUT", "/api/folders/"+gone.ID, map[string]any{"parent_id": "nope"}, jsonCT)
	if w.Code != http.StatusNotFound {
		t.Errorf("move under missing parent: got %d, want 404", w.Code)
	}
	// Sibling-name collision on a move: 409. First move "collide" under a
	// (ok), then move a second root-level "collide" under a as well.
	w = doReq(t, h, "POST", "/api/folders", map[string]any{"name": "collide"}, jsonCT)
	if w.Code != http.StatusCreated {
		t.Fatalf("create collide: got %d", w.Code)
	}
	var c store.Folder
	if err := json.Unmarshal(w.Body.Bytes(), &c); err != nil {
		t.Fatal(err)
	}
	w = doReq(t, h, "PUT", "/api/folders/"+c.ID, map[string]any{"parent_id": a.ID}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("move collide under a: got %d, body %s", w.Code, w.Body.String())
	}
	c2 := post("collide", nil)
	w = doReq(t, h, "PUT", "/api/folders/"+c2.ID, map[string]any{"parent_id": a.ID}, jsonCT)
	if w.Code != http.StatusConflict {
		t.Errorf("move onto sibling name: got %d, want 409 (body %s)", w.Code, w.Body.String())
	}
}

// newTestServerWithAI builds a Server whose AI client points at an
// httptest upstream that serves the given model content.
func newTestServerWithAI(t *testing.T, content string, upstreamStatus int) (*Server, *httptest.Server) {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if upstreamStatus != http.StatusOK {
			w.WriteHeader(upstreamStatus)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		payload := map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{"content": content}},
			},
		}
		_ = json.NewEncoder(w).Encode(payload)
	}))
	t.Cleanup(up.Close)
	return newTestServerWithAIUpstream(t, up), up
}

// newTestServerWithAIUpstream wires an existing provider upstream into a
// Server (temp-file store, key, AI client), so a test can serve its own
// canned reply and still observe the request the provider received.
func newTestServerWithAIUpstream(t *testing.T, up *httptest.Server) *Server {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	k, err := store.LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	st.SetKey(k)
	client := ai.FromConfig(config.Config{AIEndpoint: up.URL, AIModel: "test-model", AIKey: "k"}, discardLogger())
	client.HTTP = up.Client()
	log := discardLogger()
	return NewWithAI(st, &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}, "", log, client)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAIStatus(t *testing.T) {
	// Unconfigured server reports disabled.
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	w := doReq(t, h, "GET", "/api/ai/status", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Errorf("body = %s", w.Body.String())
	}
	// Configured server reports enabled plus the model (never the key).
	srv, _ := newTestServerWithAI(t, `{"title":"T","language":"bash","body":"echo hi"}`, http.StatusOK)
	w = doReq(t, srv.Handler(), "GET", "/api/ai/status", nil, "")
	if !strings.Contains(w.Body.String(), `"enabled":true`) ||
		!strings.Contains(w.Body.String(), `"model":"test-model"`) ||
		strings.Contains(w.Body.String(), "key") {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestAIGenerate(t *testing.T) {
	srv, _ := newTestServerWithAI(t,
		`{"title":"Copy a file home","language":"bash","body":"cp -rf {{file}} ~/","notes":"Copies the file into your home directory."}`, http.StatusOK)
	h := srv.Handler()

	// Happy path: the form gets snippet fields, uses_variables set for
	// {{placeholders}}, and the explanation in Notes.
	w := doReq(t, h, "POST", "/api/ai/generate",
		map[string]any{"prompt": "give me a command to copy a file to my home dir"}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", w.Code, w.Body.String())
	}
	var out aiGenerateOut
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Body != "cp -rf {{file}} ~/" || out.Title != "Copy a file home" ||
		out.Language != "bash" || !out.UsesVariables {
		t.Errorf("out = %+v", out)
	}
	if out.Notes != "Copies the file into your home directory." {
		t.Errorf("notes = %q", out.Notes)
	}

	// Missing prompt is a 400.
	w = doReq(t, h, "POST", "/api/ai/generate", map[string]any{"prompt": "  "}, jsonCT)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty prompt: %d", w.Code)
	}
}

func TestAIGenerateKind(t *testing.T) {
	// The handler must thread the requested kind through to the
	// provider prompt (spec §13): script selects the multi-line rule.
	var captured map[string]any
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{
				map[string]any{"message": map[string]any{
					"content": `{"title":"Back up","language":"bash","body":"#!/usr/bin/env bash\nset -euo pipefail"}`,
				}},
			},
		})
	}))
	t.Cleanup(up.Close)
	h := newTestServerWithAIUpstream(t, up).Handler()

	systemContent := func() string {
		msgs, _ := captured["messages"].([]any)
		if len(msgs) != 2 {
			t.Fatalf("messages = %v", captured["messages"])
		}
		m, _ := msgs[0].(map[string]any)
		s, _ := m["content"].(string)
		return s
	}

	w := doReq(t, h, "POST", "/api/ai/generate",
		map[string]any{"prompt": "back up my home dir", "kind": "script"}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("script: %d %s", w.Code, w.Body.String())
	}
	if sys := systemContent(); !strings.Contains(sys, "a multi-line program saved as one snippet") {
		t.Errorf("script kind did not reach the provider prompt:\n%s", sys)
	}

	// An omitted kind is the command default.
	w = doReq(t, h, "POST", "/api/ai/generate", map[string]any{"prompt": "list files"}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("default kind: %d %s", w.Code, w.Body.String())
	}
	if sys := systemContent(); !strings.Contains(sys, "exactly ONE executable command") {
		t.Errorf("default kind is not the command prompt:\n%s", sys)
	}

	// Case/whitespace are normalized, not rejected.
	w = doReq(t, h, "POST", "/api/ai/generate",
		map[string]any{"prompt": "a helper", "kind": " Function "}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("function: %d %s", w.Code, w.Body.String())
	}
	if sys := systemContent(); !strings.Contains(sys, "a single named function definition") {
		t.Errorf("function kind did not reach the provider prompt:\n%s", sys)
	}

	// An unknown kind is a 400, not a silent command.
	w = doReq(t, h, "POST", "/api/ai/generate",
		map[string]any{"prompt": "p", "kind": "snippet"}, jsonCT)
	if w.Code != http.StatusBadRequest {
		t.Errorf("unknown kind: %d %s", w.Code, w.Body.String())
	}
}

func TestAIGenerateNotConfigured(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	w := doReq(t, h, "POST", "/api/ai/generate", map[string]any{"prompt": "hello"}, jsonCT)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured generate: %d %s", w.Code, w.Body.String())
	}
}

func TestAIGenerateUpstreamError(t *testing.T) {
	srv, _ := newTestServerWithAI(t, "", http.StatusBadGateway)
	h := srv.Handler()
	w := doReq(t, h, "POST", "/api/ai/generate", map[string]any{"prompt": "hello"}, jsonCT)
	if w.Code != http.StatusBadGateway {
		t.Errorf("upstream 502: %d %s", w.Code, w.Body.String())
	}
}

func TestTags(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	createSnippet(t, h, map[string]any{"title": "a", "body": "x", "tags": []string{"ops", "db"}})
	createSnippet(t, h, map[string]any{"title": "b", "body": "y", "tags": []string{"ops"}})

	w := doReq(t, h, "GET", "/api/tags", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("tags: got %d", w.Code)
	}
	var tags []store.TagCount
	if err := json.Unmarshal(w.Body.Bytes(), &tags); err != nil {
		t.Fatalf("unmarshal tags: %v", err)
	}
	byName := map[string]int{}
	for _, tc := range tags {
		byName[tc.Name] = tc.Count
	}
	if byName["ops"] != 2 || byName["db"] != 1 {
		t.Errorf("tags = %v, want ops=2 db=1", byName)
	}
}

func TestSync(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	w := doReq(t, h, "POST", "/api/folders", map[string]any{"name": "F"}, jsonCT)
	if w.Code != http.StatusCreated {
		t.Fatalf("create folder: %d", w.Code)
	}
	id := createSnippet(t, h, map[string]any{"title": "s", "body": "b"})

	// Full sync: shape + server_time.
	w = doReq(t, h, "GET", "/api/sync", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("sync: got %d", w.Code)
	}
	var syncResp struct {
		ServerTime string           `json:"server_time"`
		Folders    []map[string]any `json:"folders"`
		Snippets   []map[string]any `json:"snippets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &syncResp); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	if syncResp.ServerTime == "" {
		t.Error("server_time is empty")
	}
	if len(syncResp.Folders) != 1 {
		t.Errorf("folders = %d, want 1", len(syncResp.Folders))
	}
	if len(syncResp.Snippets) != 1 {
		t.Errorf("snippets = %d, want 1", len(syncResp.Snippets))
	}
	if _, ok := syncResp.Snippets[0]["title"]; !ok {
		t.Error("live snippet should carry a title")
	}

	// After deletion, a full sync includes the row as a tombstone.
	w = doReq(t, h, "DELETE", "/api/snippets/"+id, nil, jsonCT)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: got %d", w.Code)
	}
	w = doReq(t, h, "GET", "/api/sync", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("sync after delete: got %d", w.Code)
	}
	// Unmarshal into a fresh variable: reusing syncResp would let
	// encoding/json merge into the live snippet's map from the first
	// sync, leaving a stale "title" key on the tombstone.
	var after struct {
		Snippets []map[string]any `json:"snippets"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &after); err != nil {
		t.Fatalf("unmarshal sync: %v", err)
	}
	found := false
	for _, s := range after.Snippets {
		if v, ok := s["id"].(string); ok && v == id {
			found = true
			if _, hasDel := s["deleted_at"]; !hasDel {
				t.Error("deleted snippet should be a tombstone with deleted_at")
			}
			if _, hasTitle := s["title"]; hasTitle {
				t.Error("deleted snippet should be a tombstone without a title")
			}
		}
	}
	if !found {
		t.Error("expected a tombstone for the deleted snippet")
	}
}

func TestSeedStarterPack(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	doc, err := starter.Pack()
	if err != nil {
		t.Fatal(err)
	}

	// The first apply creates the whole pack.
	w := doReq(t, h, "POST", "/api/seed", nil, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("seed: %d %s", w.Code, w.Body.String())
	}
	var out store.ImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Created != len(doc.Snippets) || out.Updated != 0 {
		t.Errorf("first seed = %+v, want %d created", out, len(doc.Snippets))
	}

	// The snippets are really there, and folder_path made the folder.
	w = doReq(t, h, "GET", "/api/snippets", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list snippets: %d", w.Code)
	}
	if n := strings.Count(w.Body.String(), `"body"`); n < len(doc.Snippets) {
		t.Errorf("listed %d bodies, want at least %d", n, len(doc.Snippets))
	}
	w = doReq(t, h, "GET", "/api/folders", nil, "")
	if !strings.Contains(w.Body.String(), `"name":"Starter"`) {
		t.Errorf("Starter folder missing: %s", w.Body.String())
	}

	// Pressing it again updates in place rather than duplicating.
	w = doReq(t, h, "POST", "/api/seed", nil, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("second seed: %d %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Created != 0 || out.Updated != len(doc.Snippets) {
		t.Errorf("second seed = %+v, want 0 created / %d updated", out, len(doc.Snippets))
	}
}

func TestImportExport(t *testing.T) {
	h := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()

	a := createSnippet(t, h, map[string]any{"title": "orig", "body": "original body", "tags": []string{"a"}})
	b := createSnippet(t, h, map[string]any{"title": "other", "body": "other body"})

	// Export: version 1, bodies decrypted.
	w := doReq(t, h, "GET", "/api/export", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("export: got %d", w.Code)
	}
	var doc store.ExportDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if doc.Version != 1 {
		t.Errorf("version = %d, want 1", doc.Version)
	}
	if len(doc.Snippets) != 2 {
		t.Fatalf("snippets = %d, want 2", len(doc.Snippets))
	}
	var exportedA *store.SnippetOut
	for i := range doc.Snippets {
		if doc.Snippets[i].ID == a {
			exportedA = &doc.Snippets[i]
		}
	}
	if exportedA == nil || exportedA.Body == nil || *exportedA.Body != "original body" {
		t.Fatalf("export did not decrypt body: %+v", exportedA)
	}

	// Replace import keeping only snippet a: b is soft-deleted.
	imp := store.ImportDoc{
		Version:  1,
		Snippets: []store.ImportSnippet{{ID: a, Title: "A kept", Body: "a body"}},
	}
	w = doReq(t, h, "POST", "/api/import?mode=replace", imp, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("import replace: got %d, body %s", w.Code, w.Body.String())
	}
	var res store.ImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal import result: %v", err)
	}
	if res.Updated != 1 || res.Created != 0 {
		t.Errorf("import result = %+v, want updated=1 created=0", res)
	}

	// a is updated, b is gone.
	w = doReq(t, h, "GET", "/api/snippets/"+a, nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("get a after import: got %d", w.Code)
	}
	var gotA store.SnippetOut
	if err := json.Unmarshal(w.Body.Bytes(), &gotA); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if gotA.Title != "A kept" {
		t.Errorf("a title = %q, want %q", gotA.Title, "A kept")
	}
	w = doReq(t, h, "GET", "/api/snippets/"+b, nil, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("get b after replace: got %d, want 404", w.Code)
	}
}

func TestStaticFallback(t *testing.T) {
	s := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}})
	s.staticFS = http.FS(fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>index</html>")},
		"sw.js":         &fstest.MapFile{Data: []byte("self.skipWaiting = true")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	})
	h := s.Handler()

	// Root serves index.html, revalidated on every load (spec §6 PWA: a
	// new build must not wait out heuristic caching).
	w := doReq(t, h, "GET", "/", nil, "")
	if w.Code != http.StatusOK || w.Body.String() != "<html>index</html>" {
		t.Errorf("root: got %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("root Cache-Control = %q, want %q", got, "no-cache")
	}

	// An unknown extensionless path falls back to index.html (SPA route).
	w = doReq(t, h, "GET", "/some/spa/route", nil, "")
	if w.Code != http.StatusOK || w.Body.String() != "<html>index</html>" {
		t.Errorf("spa route: got %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("spa route Cache-Control = %q, want %q", got, "no-cache")
	}

	// The service worker script is revalidated too, so the browser's
	// update check sees a new build promptly.
	w = doReq(t, h, "GET", "/sw.js", nil, "")
	if w.Code != http.StatusOK || w.Body.String() != "self.skipWaiting = true" {
		t.Errorf("sw.js: got %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("sw.js Cache-Control = %q, want %q", got, "no-cache")
	}

	// A known asset is served verbatim, cached immutably (content-hashed).
	w = doReq(t, h, "GET", "/assets/app.js", nil, "")
	if w.Code != http.StatusOK || w.Body.String() != "console.log(1)" {
		t.Errorf("asset: got %d %q", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("asset Cache-Control = %q, want immutable", got)
	}

	// An unknown path with an extension is a 404.
	w = doReq(t, h, "GET", "/nope.js", nil, "")
	if w.Code != http.StatusNotFound {
		t.Errorf("missing asset: got %d, want 404", w.Code)
	}
}

func TestAISuggestTags(t *testing.T) {
	srv, _ := newTestServerWithAI(t, `["python","network","Bad Tag!"]`, http.StatusOK)
	h := srv.Handler()

	// Happy path: valid suggestions come through, invalid ones (space in
	// name) are filtered before the client ever sees them.
	w := doReq(t, h, "POST", "/api/ai/tags", map[string]any{"body": "python -m http.server"}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("suggest: %d %s", w.Code, w.Body.String())
	}
	var out aiTagsOut
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Tags) != 2 || out.Tags[0] != "python" || out.Tags[1] != "network" {
		t.Errorf("tags = %v", out.Tags)
	}

	// Missing body is a 400.
	w = doReq(t, h, "POST", "/api/ai/tags", map[string]any{"body": "  "}, jsonCT)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty body: %d", w.Code)
	}

	// Unconfigured is a 503.
	h2 := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	w = doReq(t, h2, "POST", "/api/ai/tags", map[string]any{"body": "x"}, jsonCT)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured: %d", w.Code)
	}
}

func TestAIExplain(t *testing.T) {
	srv, _ := newTestServerWithAI(t, "It copies files recursively and overwrites.", http.StatusOK)
	h := srv.Handler()

	w := doReq(t, h, "POST", "/api/ai/explain", map[string]any{"body": "cp -rf src dst"}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("explain: %d %s", w.Code, w.Body.String())
	}
	var out aiExplainOut
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Notes != "It copies files recursively and overwrites." {
		t.Errorf("notes = %q", out.Notes)
	}

	w = doReq(t, h, "POST", "/api/ai/explain", map[string]any{"body": "  "}, jsonCT)
	if w.Code != http.StatusBadRequest {
		t.Errorf("empty body: %d", w.Code)
	}

	h2 := newTestServer(t, "", &fakeResolver{id: tsauth.Identity{Login: "dev@local"}}).Handler()
	w = doReq(t, h2, "POST", "/api/ai/explain", map[string]any{"body": "x"}, jsonCT)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("unconfigured: %d", w.Code)
	}
}

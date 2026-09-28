package pick

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/jstevewhite/snp/internal/store"
)

func TestCommandText(t *testing.T) {
	plain := Snippet{Body: "echo hi"}
	if got := CommandText(plain, "echo hi"); got != "echo hi" {
		t.Errorf("plain = %q", got)
	}
	secret := Snippet{Sensitive: true}
	if got := CommandText(secret, "echo hi"); got != " echo hi" {
		t.Errorf("secret = %q", got)
	}
	if got := CommandText(secret, " echo hi"); got != " echo hi" {
		t.Errorf("already spaced = %q", got)
	}
	if got := CommandText(secret, "echo\nhi"); got != " echo\nhi" {
		t.Errorf("multiline = %q", got)
	}
	if got := CommandText(secret, ""); got != "" {
		t.Errorf("empty secret = %q", got)
	}
}

func TestLocalSearchAndReveal(t *testing.T) {
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
	if _, err := st.CreateSnippet(store.SnippetInput{
		Title: "restart caddy", Body: "sudo systemctl restart caddy",
		Language: "bash", Notes: "after the Caddyfile", Tags: []string{"ops"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateSnippet(store.SnippetInput{
		Title: "deploy", Body: "ssh {{host|gx10}}",
		Language: "bash", UsesVariables: true,
		VarDefaults: map[string]string{"host": "nas"},
	}); err != nil {
		t.Fatal(err)
	}
	secret, err := st.CreateSnippet(store.SnippetInput{
		Title: "token", Body: "hunter2", IsSensitive: true, Notes: "careful",
	})
	if err != nil {
		t.Fatal(err)
	}

	lib := Local{Store: st}
	hits, err := lib.Search(context.Background(), "caddy")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Body != "sudo systemctl restart caddy" || hits[0].IsTemplate() {
		t.Fatalf("search = %+v", hits)
	}
	all, err := lib.Search(context.Background(), "")
	if err != nil || len(all) != 3 {
		t.Fatalf("all = %+v, %v", all, err)
	}
	var hidden Snippet
	for _, h := range all {
		if h.ID == secret.ID {
			hidden = h
		}
	}
	if hidden.Body != "" || !hidden.Sensitive {
		t.Fatalf("list leaked sensitive body: %+v", hidden)
	}
	full, err := lib.Reveal(context.Background(), secret.ID)
	if err != nil {
		t.Fatal(err)
	}
	if full.Body != "hunter2" || full.Notes != "careful" {
		t.Fatalf("reveal = %+v", full)
	}
}

func TestHTTPSearchAndReveal(t *testing.T) {
	plainBody := "echo hi"
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/snippets", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("q") != "tag:ops hi" {
			t.Errorf("q = %q", r.URL.Query().Get("q"))
		}
		if r.URL.Query().Get("limit") != "200" {
			t.Errorf("limit = %q", r.URL.Query().Get("limit"))
		}
		json.NewEncoder(w).Encode([]store.SnippetOut{{
			ID: "a", Title: "hi", Body: &plainBody, Language: "bash",
			Tags: []string{"ops"}, UsesVariables: false,
		}})
	})
	mux.HandleFunc("GET /api/snippets/{id}", func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != "secret" {
			http.NotFound(w, r)
			return
		}
		body := "hunter2"
		json.NewEncoder(w).Encode(store.SnippetOut{
			ID: "secret", Title: "token", Body: &body, IsSensitive: true,
			VarDefaults: map[string]string{"tok": "hunter2"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	lib, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	hits, err := lib.Search(context.Background(), "tag:ops hi")
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Body != "echo hi" || hits[0].Tags[0] != "ops" {
		t.Fatalf("hits = %+v", hits)
	}
	full, err := lib.Reveal(context.Background(), "secret")
	if err != nil {
		t.Fatal(err)
	}
	if full.Body != "hunter2" || full.VarDefaults["tok"] != "hunter2" || !full.Sensitive {
		t.Fatalf("reveal = %+v", full)
	}
}

func TestHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":"forbidden"}`))
	}))
	defer srv.Close()
	lib, err := NewHTTP(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, err = lib.Search(context.Background(), "")
	if err == nil || err.Error() != "library: forbidden" {
		t.Fatalf("err = %v", err)
	}
}

func TestNewHTTPRejects(t *testing.T) {
	for _, raw := range []string{"", "ftp://example.com", "snp.tailnet.ts.net", "http://"} {
		if _, err := NewHTTP(raw); err == nil {
			t.Errorf("NewHTTP(%q) succeeded", raw)
		}
	}
}

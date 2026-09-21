package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"testing"

	"github.com/jstevewhite/snp/internal/store"
)

func TestDataPreviewAndBackupRoutes(t *testing.T) {
	srv := newTestServer(t, "", &fakeResolver{})
	h := srv.Handler()
	id := createSnippet(t, h, map[string]any{"title": "keep", "body": "before"})
	body := map[string]any{"version": 1, "snippets": []any{map[string]any{"id": id, "title": "after", "body": "after"}}}
	w := doReq(t, h, "POST", "/api/import?preview=1", body, jsonCT)
	if w.Code != 200 {
		t.Fatalf("preview: %d %s", w.Code, w.Body.String())
	}
	var result store.ImportResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || result.Updated != 1 {
		t.Fatalf("result: %+v %v", result, err)
	}
	got, err := srv.store.GetSnippet(id)
	if err != nil || got.Title != "keep" {
		t.Fatal("preview mutated snippet")
	}
	w = doReq(t, h, "POST", "/api/import?mode=replace", map[string]any{"version": 1}, jsonCT)
	if w.Code != 400 {
		t.Fatalf("missing snippets allowed: %d", w.Code)
	}
	w = doReq(t, h, "GET", "/api/export", nil, "")
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("export cacheable")
	}
	w = doReq(t, h, "POST", "/api/backup", nil, "")
	if w.Code != 415 {
		t.Fatalf("CSRF: %d", w.Code)
	}
	w = doReq(t, h, "POST", "/api/backup", nil, jsonCT)
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/zip" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("backup: %d %s", w.Code, w.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil || len(zr.File) != 3 {
		t.Fatalf("archive: %v", err)
	}
	blocked := newTestServer(t, "owner", &fakeResolver{}).Handler()
	for _, path := range []string{"/api/backup", "/api/import?preview=1"} {
		w = doReq(t, blocked, "POST", path, body, jsonCT)
		if w.Code != 403 {
			t.Fatalf("unauthorized %s: %d", path, w.Code)
		}
	}
}

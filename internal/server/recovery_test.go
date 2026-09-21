package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/store"
)

func TestRecoveryEndpoints(t *testing.T) {
	srv := newTestServer(t, "", &fakeResolver{})
	h := srv.Handler()
	id := createSnippet(t, h, map[string]any{"title": "secret", "body": "original-secret", "is_sensitive": true})
	w := doReq(t, h, "PUT", "/api/snippets/"+id, map[string]any{"title": "secret", "body": "updated-secret", "is_sensitive": true}, jsonCT)
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	base := "/api/snippets/" + id
	w = doReq(t, h, "GET", base+"/revisions", nil, "")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("summaries: %d %s", w.Code, w.Body.String())
	}
	var revisions []store.Revision
	if err := json.Unmarshal(w.Body.Bytes(), &revisions); err != nil || len(revisions) != 1 {
		t.Fatalf("revisions: %+v %v", revisions, err)
	}
	path := fmt.Sprintf("%s/revisions/%d", base, revisions[0].ID)
	w = doReq(t, h, "GET", path, nil, "")
	if w.Code != 403 || strings.Contains(w.Body.String(), "original-secret") {
		t.Fatal("protected preview without reveal")
	}
	w = doReq(t, h, "GET", path+"?reveal=1", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "original-secret") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("reveal: %d %s", w.Code, w.Body.String())
	}
	w = doReq(t, h, "POST", path+"/restore", nil, "")
	if w.Code != 415 {
		t.Fatalf("CSRF: %d", w.Code)
	}
	w = doReq(t, h, "POST", path+"/restore", nil, jsonCT)
	if w.Code != 200 || strings.Contains(w.Body.String(), "original-secret") {
		t.Fatalf("restore: %d %s", w.Code, w.Body.String())
	}
	w = doReq(t, h, "DELETE", base, nil, jsonCT)
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
	w = doReq(t, h, "GET", "/api/trash", nil, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), id) || strings.Contains(w.Body.String(), "original-secret") {
		t.Fatal(w.Body.String())
	}
	w = doReq(t, h, "GET", path+"?reveal=1", nil, "")
	if w.Code != 404 {
		t.Fatal("trashed history should require restoration")
	}
	w = doReq(t, h, "POST", base+"/restore", nil, "")
	if w.Code != 415 {
		t.Fatal(w.Code)
	}
	w = doReq(t, h, "POST", base+"/restore", nil, jsonCT)
	if w.Code != 200 || strings.Contains(w.Body.String(), "original-secret") {
		t.Fatal(w.Body.String())
	}
	w = doReq(t, h, "GET", base+"/revisions/invalid", nil, "")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	// Every recovery route remains behind owner authentication.
	other := newTestServer(t, "owner", &fakeResolver{}).Handler()
	for _, p := range []string{"/api/trash", base + "/revisions", path + "?reveal=1"} {
		w = doReq(t, other, "GET", p, nil, "")
		if w.Code != 403 {
			t.Fatalf("unprotected route %s: %d", p, w.Code)
		}
	}
	for _, p := range []string{base + "/restore", path + "/restore"} {
		w = doReq(t, other, "POST", p, nil, jsonCT)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
}

package server

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/portable"
	"github.com/jstevewhite/snp/internal/store"
)

func TestPasswordProtectedDownloadsAndImport(t *testing.T) {
	srv := newTestServer(t, "", &fakeResolver{})
	h := srv.Handler()
	const password = "my protected download password"
	createSnippet(t, h, map[string]any{"title": "Private", "body": "secret-not-in-file", "is_sensitive": true})
	options := map[string]any{"encrypt": true, "password": password}
	for _, kind := range []string{"export", "backup"} {
		t.Run(kind, func(t *testing.T) {
			w := doReq(t, h, "POST", "/api/"+kind, options, jsonCT)
			if w.Code != 200 {
				t.Fatalf("download: %d %s", w.Code, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Header().Get("Content-Disposition"), ".age") {
				t.Fatal("missing protected download headers")
			}
			if strings.Contains(w.Body.String(), "secret-not-in-file") || !strings.HasPrefix(w.Body.String(), portable.ArmorHeader) {
				t.Fatal("unprotected content")
			}
			var plain bytes.Buffer
			if err := portable.Decrypt(&plain, bytes.NewReader(w.Body.Bytes()), password); err != nil {
				t.Fatal(err)
			}
			if kind == "backup" {
				zr, err := zip.NewReader(bytes.NewReader(plain.Bytes()), int64(plain.Len()))
				if err != nil || len(zr.File) != 3 {
					t.Fatalf("backup round trip: %v", err)
				}
				return
			}
			var doc store.ExportDoc
			if err := json.Unmarshal(plain.Bytes(), &doc); err != nil || len(doc.Snippets) != 1 || (doc.Snippets[0].Body == nil || *doc.Snippets[0].Body != "secret-not-in-file") {
				t.Fatalf("export round trip: %v", err)
			}
			for _, pw := range []string{"wrong", password} {
				unlocked := doReq(t, h, "POST", "/api/decrypt-import", map[string]any{"data": w.Body.String(), "password": pw}, jsonCT)
				if pw == "wrong" {
					if unlocked.Code != 400 || strings.Contains(unlocked.Body.String(), "secret-not-in-file") {
						t.Fatal("wrong password revealed data")
					}
				} else {
					if unlocked.Code != 200 || !strings.Contains(unlocked.Body.String(), "secret-not-in-file") || unlocked.Header().Get("Cache-Control") != "no-store" {
						t.Fatalf("unlock: %d", unlocked.Code)
					}
				}
			}
		})
	}
	// Passwords must be intentional: never silently return plaintext on a typo.
	for _, options := range []map[string]any{{"encrypt": true}, {"encrypt": true, "password": "short"}, {"password": password}} {
		if w := doReq(t, h, "POST", "/api/export", options, jsonCT); w.Code != 400 {
			t.Fatalf("bad options: %d", w.Code)
		}
	}
	if w := doReq(t, h, "POST", "/api/export", options, ""); w.Code != 415 {
		t.Fatal("CSRF bypass")
	}
	denied := newTestServer(t, "owner", &fakeResolver{}).Handler()
	if w := doReq(t, denied, "POST", "/api/decrypt-import", options, jsonCT); w.Code != 403 {
		t.Fatal("owner bypass")
	}
	srv.cryptoMu.Lock()
	if w := doReq(t, h, "POST", "/api/export", options, jsonCT); w.Code != 409 {
		t.Fatal("concurrent KDF allowed")
	}
	srv.cryptoMu.Unlock()
}

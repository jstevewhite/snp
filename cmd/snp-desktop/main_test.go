//go:build darwin || linux

package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopShellMarker(t *testing.T) {
	handler := injectDesktopMarker()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	for _, path := range []string{"/", "/index.html"} {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "window.__SNP_DESKTOP__=true") {
			t.Fatalf("%s: desktop shell missing marker (status %d)", path, rr.Code)
		}
		if strings.Contains(rr.Body.String(), `src="/registerSW.js"`) {
			t.Fatal("desktop shell must not register a service worker")
		}
	}
}

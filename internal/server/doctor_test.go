package server

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/store"
	"github.com/jstevewhite/snp/internal/tsauth"
)

// newDoctorServer builds a Server over a temp state dir and returns it
// with the database path, so a test can damage the index directly. The
// state dir is chmodded the way snp creates it, so the key check sees
// production permissions.
func newDoctorServer(t *testing.T, owner string, withKeyPath bool) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod state dir: %v", err)
	}
	dbPath := filepath.Join(dir, "test.db")
	keyPath := filepath.Join(dir, "key")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	k, err := store.LoadOrCreateKey(keyPath)
	if err != nil {
		t.Fatalf("LoadOrCreateKey: %v", err)
	}
	st.SetKey(k)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(st, &fakeResolver{id: tsauth.Identity{Login: "alice@example.com"}}, owner, log)
	if withKeyPath {
		srv.SetKeyPath(keyPath)
	}
	return srv, dbPath
}

// dropIndexRow removes one snippet's FTS row, leaving the content table
// alone.
func dropIndexRow(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`DELETE FROM snippets_fts WHERE rowid = (SELECT MIN(rowid) FROM snippets)`); err != nil {
		t.Fatalf("delete index row: %v", err)
	}
}

// decodeDoctor reads the endpoint's response body.
func decodeDoctor(t *testing.T, w interface{ Bytes() []byte }) doctorOut {
	t.Helper()
	var out doctorOut
	if err := json.Unmarshal(w.Bytes(), &out); err != nil {
		t.Fatalf("decode doctor response: %v (%s)", err, w.Bytes())
	}
	return out
}

// doctorCheck finds one check in a report.
func doctorCheck(t *testing.T, rep *store.DoctorReport, name string) store.DoctorCheck {
	t.Helper()
	if rep == nil {
		t.Fatalf("no report; wanted check %q", name)
	}
	for _, c := range rep.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q missing from report", name)
	return store.DoctorCheck{}
}

func TestDoctorEndpointReport(t *testing.T) {
	srv, _ := newDoctorServer(t, "", true)
	h := srv.Handler()
	createSnippet(t, h, map[string]any{"title": "one", "body": "restart caddy"})

	w := doReq(t, h, "GET", "/api/doctor", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/doctor = %d, want 200: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	out := decodeDoctor(t, w.Body)
	if out.Report == nil {
		t.Fatal("report missing")
	}
	if !out.Report.Healthy {
		t.Errorf("healthy store reported unhealthy: %+v", out.Report.Checks)
	}
	if out.Repair != nil || out.After != nil {
		t.Error("a read-only check must not include repair or after")
	}
	if len(out.Report.Checks) != len(store.DoctorCheckNames()) {
		t.Errorf("got %d checks, want %d", len(out.Report.Checks), len(store.DoctorCheckNames()))
	}
	// With a key path set, the key check inspects a real file.
	if c := doctorCheck(t, out.Report, store.CheckKey); c.Status != store.StatusOK {
		t.Errorf("key check = %s (%s), want ok", c.Status, c.Detail)
	}
}

func TestDoctorEndpointKeyCheckSkippedWithoutPath(t *testing.T) {
	srv, _ := newDoctorServer(t, "", false)
	w := doReq(t, srv.Handler(), "GET", "/api/doctor", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", w.Code, w.Body.String())
	}
	out := decodeDoctor(t, w.Body)
	c := doctorCheck(t, out.Report, store.CheckKey)
	if c.Status != store.StatusWarn || !strings.Contains(c.Detail, "skipped") {
		t.Errorf("key check = %s (%s), want a skipped warning", c.Status, c.Detail)
	}
}

func TestDoctorEndpointRepairs(t *testing.T) {
	srv, dbPath := newDoctorServer(t, "", true)
	h := srv.Handler()
	createSnippet(t, h, map[string]any{"title": "one", "body": "restart caddy"})
	dropIndexRow(t, dbPath)

	w := doReq(t, h, "GET", "/api/doctor?only=fts_count", nil, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET = %d: %s", w.Code, w.Body.String())
	}
	if before := decodeDoctor(t, w.Body); before.Report.Healthy {
		t.Fatal("damaged index reported healthy")
	}

	w = doReq(t, h, "POST", "/api/doctor/repair", map[string]any{"only": []string{"fts_count"}}, jsonCT)
	if w.Code != http.StatusOK {
		t.Fatalf("POST repair = %d, want 200: %s", w.Code, w.Body.String())
	}
	out := decodeDoctor(t, w.Body)
	if out.Repair == nil || !out.Repair.RebuiltFTS {
		t.Errorf("repair = %+v, want a rebuild", out.Repair)
	}
	if out.After == nil || !out.After.Healthy {
		t.Errorf("after = %+v, want healthy", out.After)
	}
}

// A second repair is refused rather than queued.
func TestDoctorEndpointRepairConflict(t *testing.T) {
	srv, _ := newDoctorServer(t, "", true)
	srv.doctorMu.Lock()
	defer srv.doctorMu.Unlock()

	w := doReq(t, srv.Handler(), "POST", "/api/doctor/repair", map[string]any{}, jsonCT)
	if w.Code != http.StatusConflict {
		t.Errorf("POST repair while busy = %d, want 409: %s", w.Code, w.Body.String())
	}
}

func TestDoctorEndpointRepairRequiresJSON(t *testing.T) {
	srv, _ := newDoctorServer(t, "", true)
	w := doReq(t, srv.Handler(), "POST", "/api/doctor/repair", map[string]any{}, "")
	if w.Code != http.StatusUnsupportedMediaType {
		t.Errorf("POST without a content type = %d, want 415", w.Code)
	}
}

func TestDoctorEndpointUnknownCheck(t *testing.T) {
	srv, _ := newDoctorServer(t, "", true)
	w := doReq(t, srv.Handler(), "GET", "/api/doctor?only=nonsense", nil, "")
	if w.Code != http.StatusBadRequest {
		t.Errorf("GET with an unknown check = %d, want 400: %s", w.Code, w.Body.String())
	}
}

func TestDoctorEndpointRequiresOwner(t *testing.T) {
	srv, _ := newDoctorServer(t, "bob@example.com", true)
	h := srv.Handler()
	if w := doReq(t, h, "GET", "/api/doctor", nil, ""); w.Code != http.StatusForbidden {
		t.Errorf("GET as a non-owner = %d, want 403", w.Code)
	}
	if w := doReq(t, h, "POST", "/api/doctor/repair", map[string]any{}, jsonCT); w.Code != http.StatusForbidden {
		t.Errorf("POST as a non-owner = %d, want 403", w.Code)
	}
}

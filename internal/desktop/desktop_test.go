package desktop

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/config"
)

func discardLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// testApp returns an App whose handler is backed by a real temp-file
// store and key (the same NewHandler the desktop command uses).
func testApp(t *testing.T) *App {
	t.Helper()
	h, st, err := NewHandler(config.Config{StateDir: t.TempDir()}, discardLog())
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewApp(h, discardLog())
}

func TestCallAPIMe(t *testing.T) {
	a := testApp(t)
	res, err := a.CallAPI(http.MethodGet, "/api/me", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusOK {
		t.Fatalf("status = %d, body %s", res.Status, res.Body)
	}
	if !strings.Contains(res.Body, "dev@local") {
		t.Errorf("body = %s", res.Body)
	}
}

func TestCallAPICRUD(t *testing.T) {
	a := testApp(t)
	// Create (POST carries a JSON body).
	created, err := a.CallAPI(http.MethodPost, "/api/snippets",
		`{"title":"restart caddy","body":"sudo systemctl restart caddy","language":"bash","tags":["ops"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Status, created.Body)
	}
	var sn struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(created.Body), &sn); err != nil || sn.ID == "" {
		t.Fatalf("create body: %q err %v", created.Body, err)
	}

	// Body-less DELETE must pass the CSRF guard (the bridge sets the
	// JSON content type automatically, like the frontend api.ts).
	del, err := a.CallAPI(http.MethodDelete, "/api/snippets/"+sn.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if del.Status != http.StatusNoContent {
		t.Fatalf("delete: %d %s", del.Status, del.Body)
	}
	// The tombstone is gone from normal reads.
	get, err := a.CallAPI(http.MethodGet, "/api/snippets/"+sn.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if get.Status != http.StatusNotFound {
		t.Fatalf("get after delete: %d", get.Status)
	}
	// Search with a query string passes through intact.
	list, err := a.CallAPI(http.MethodGet, "/api/snippets?q=restart", "")
	if err != nil {
		t.Fatal(err)
	}
	if list.Status != http.StatusOK {
		t.Fatalf("list: %d", list.Status)
	}
	if strings.Contains(list.Body, sn.ID) {
		t.Error("soft-deleted snippet still searchable")
	}
}

// TestCallAPISensitiveRoundTrip: NewHandler loads the key, so a
// sensitive snippet's body must encrypt at rest and decrypt over the
// bridge's single-snippet read, while staying out of list/search
// responses.
func TestCallAPISensitiveRoundTrip(t *testing.T) {
	a := testApp(t)
	created, err := a.CallAPI(http.MethodPost, "/api/snippets",
		`{"title":"token","body":"super-secret","is_sensitive":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if created.Status != http.StatusCreated {
		t.Fatalf("create: %d %s", created.Status, created.Body)
	}
	var sn struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(created.Body), &sn); err != nil {
		t.Fatal(err)
	}
	// Raw body endpoint decrypts for the desktop user.
	raw, err := a.CallAPI(http.MethodGet, "/api/snippets/"+sn.ID+"/raw", "")
	if err != nil {
		t.Fatal(err)
	}
	if raw.Status != http.StatusOK || raw.Body != "super-secret" {
		t.Fatalf("raw: %d %q", raw.Status, raw.Body)
	}
	// List responses keep the body hidden.
	list, err := a.CallAPI(http.MethodGet, "/api/snippets", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list.Body, `"body":null`) {
		t.Errorf("sensitive body leaked in list: %s", list.Body)
	}
}

func TestCallAPIGuards(t *testing.T) {
	a := testApp(t)
	// Unsupported methods are refused before touching the handler.
	if _, err := a.CallAPI("PATCH", "/api/snippets", ""); err == nil {
		t.Error("PATCH should be refused")
	}
	// Non-API paths are refused (the SPA is served by the wails asset
	// server, not by the bridge).
	if _, err := a.CallAPI(http.MethodGet, "/index.html", ""); err == nil {
		t.Error("non-API path should be refused")
	}
	// An API path that exists but 404s surfaces its status.
	res, err := a.CallAPI(http.MethodGet, "/api/nope", "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusNotFound {
		t.Errorf("unknown api path: %d", res.Status)
	}
	// Invalid JSON in a POST body maps to 400 through the handler.
	res, err = a.CallAPI(http.MethodPost, "/api/snippets", "{not json")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != http.StatusBadRequest {
		t.Errorf("bad json: %d %s", res.Status, res.Body)
	}
}

// TestFirstCallLogsOnce: the bridge logs an end-to-end "frontend
// loaded" marker (time since App construction) on exactly the first
// API call, so --debug output shows how long the window/page took to
// boot.
func TestFirstCallLogsOnce(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	h, st, err := NewHandler(config.Config{StateDir: t.TempDir()}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	a := NewApp(h, log)
	if _, err := a.CallAPI(http.MethodGet, "/api/me", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CallAPI(http.MethodGet, "/api/me", ""); err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	if n := strings.Count(got, "frontend loaded"); n != 1 {
		t.Errorf("frontend loaded logged %d times, want 1:\n%s", n, got)
	}
	if !strings.Contains(got, "elapsed_ms") || !strings.Contains(got, "/api/me") {
		t.Errorf("marker missing fields:\n%s", got)
	}
}

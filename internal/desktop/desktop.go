// Package desktop implements the Wails desktop variant of snp: a local
// instance of the same store and SPA, in a native window, with no
// tailnet and no HTTP port (see docs/snp-design.md §12).
//
// The SPA normally talks to the Go API over HTTP. In the desktop window
// there is no HTTP listener, so App.CallAPI runs each request against
// the same in-process http.Handler the serve subcommand uses
// (httptest-backed). The frontend routes its API calls through this
// bridge when it detects the desktop binding (web/src/lib/desktop.ts),
// which keeps the content-type guards and error semantics identical to
// the server variant.
//
// This package deliberately does NOT import wails: it stays
// platform-neutral and testable everywhere. The Wails window itself is
// wired in cmd/snp-desktop (darwin).
package desktop

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/server"
	"github.com/jstevewhite/snp/internal/store"
	"github.com/jstevewhite/snp/internal/tsauth"
)

// stateChanging mirrors the server's guardMW rule (spec §3): these
// methods require a JSON content type, and the frontend (api.ts) sends
// it for them even without a body. The bridge reproduces that so UI
// deletes keep working over the in-process transport.
func stateChanging(method string) bool {
	return method == http.MethodPost || method == http.MethodPut || method == http.MethodDelete
}

// CallResult is the JSON shape returned to the frontend for every API
// call made over the desktop bridge.
type CallResult struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
}

// App is the wails-bound object. Bind it with
// options.App{Bind: []interface{}{app}}; it surfaces to the SPA as
// window.go.desktop.App.
type App struct {
	handler   http.Handler
	log       *slog.Logger
	startedAt time.Time
	firstCall sync.Once
}

// NewApp returns the desktop bridge over an existing handler. startedAt
// marks bridge construction so the first frontend API call can be timed
// (how long the window/page took to boot).
func NewApp(handler http.Handler, log *slog.Logger) *App {
	return &App{handler: handler, log: log, startedAt: time.Now()}
}

// NewHandler opens the store under cfg (creating the state dir, the
// database, and the encryption key as needed) and returns the same
// API+SPA handler the serve subcommand serves, with the dev identity:
// the desktop app is single-user and local, with no network surface.
func NewHandler(cfg config.Config, log *slog.Logger) (http.Handler, *store.Store, error) {
	started := time.Now()
	dbPath := filepath.Join(cfg.StateDir, "snp.db")
	keyPath := filepath.Join(cfg.StateDir, "key")
	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("state dir %s: %w", cfg.StateDir, err)
	}
	st, err := store.Open(dbPath)
	if err != nil {
		return nil, nil, err
	}
	k, err := store.LoadOrCreateKey(keyPath)
	if err != nil {
		st.Close()
		return nil, nil, err
	}
	st.SetKey(k)
	log.Info("desktop: store ready", "db", dbPath,
		"elapsed_ms", time.Since(started).Milliseconds())
	// The desktop window is a local instance, so the AI feature is
	// configured from the same config file (spec §13) and served over
	// the in-process bridge like every other /api route.
	aiClient := ai.FromConfig(cfg, log)
	if aiClient == nil {
		log.Debug("desktop: AI not configured (set ai_key to enable)")
	} else {
		log.Debug("desktop: AI configured", "model", aiClient.Model, "endpoint", aiClient.Endpoint)
	}
	return server.NewWithAI(st, tsauth.NewDev(), "", log, aiClient).Handler(), st, nil
}

// Log forwards a page-side diagnostic line to the app log. The wails
// asset-server runtime (window.runtime) is not injected when the shell
// is served by our own middleware, so the frontend calls this bound
// method instead — it lands in the slog stream (visible under --debug)
// and lets blank-window diagnostics escape a window that never paints.
func (a *App) Log(level, message string) error {
	switch level {
	case "debug":
		a.log.Debug("page: " + message)
	case "warn", "warning":
		a.log.Warn("page: " + message)
	case "error":
		a.log.Error("page: " + message)
	default:
		a.log.Info("page: " + message)
	}
	return nil
}

// CallAPI runs one API request against the in-process handler and
// returns the response as plain data for the frontend bridge. path is
// the absolute request path including any query string (e.g.
// "/api/snippets?q=ops"); body is the raw JSON body, or "" when there
// is none. The JSON content-type rule of the HTTP API (spec §3) is
// applied automatically for state-changing methods, so a body-less
// DELETE still carries application/json and passes the CSRF guard.
func (a *App) CallAPI(method, path, body string) (CallResult, error) {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
	default:
		return CallResult{}, fmt.Errorf("desktop: unsupported method %q", method)
	}
	if !strings.HasPrefix(path, "/api/") {
		return CallResult{}, fmt.Errorf("desktop: refusing non-API path %q", path)
	}
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if stateChanging(method) || body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	// The first API call only happens after the SPA has booted in the
	// webview (its startup sync goes through this bridge), so logging it
	// measures end-to-end load time: store ready -> window -> page boot.
	a.firstCall.Do(func() {
		a.log.Info("desktop: frontend loaded (first API call)",
			"elapsed_ms", time.Since(a.startedAt).Milliseconds(),
			"method", method, "path", path)
	})
	return CallResult{
		Status:      rec.Code,
		ContentType: rec.Header().Get("Content-Type"),
		Body:        rec.Body.String(),
	}, nil
}

//go:build darwin || linux

// Command snp-desktop runs snp as a local Wails desktop app (spec §12):
// the same SQLite store and embedded SPA as the server binary, in a
// native window, with no tailnet and no HTTP listener. The SPA's API
// calls are served by the in-process bridge (internal/desktop) instead
// of over HTTP.
//
// Data lives in the same state dir as the CLI and server variants
// (config default ~/.local/share/snp), so the desktop app and the snp
// CLI/backup tooling share one store.
//
// Built on macOS and Linux. This file is deliberately platform-neutral:
// the per-OS Wails options live in platform_darwin.go and
// platform_linux.go, and the platform-specific link flags in the
// Makefile's `desktop` target. Windows is a follow-on (WebView2 needs
// its own options file).
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/logger"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/jstevewhite/snp/internal/buildinfo"
	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/desktop"
	webembed "github.com/jstevewhite/snp/web"
)

// wailsLogger adapts snp's slog logger to the wails logger interface so
// wails internals land in the same structured stream.
type wailsLogger struct {
	log *slog.Logger
}

func (w *wailsLogger) Print(message string) { w.log.Info(message) }
func (w *wailsLogger) Trace(message string) { w.log.Debug(message) }
func (w *wailsLogger) Debug(message string) { w.log.Debug(message) }
func (w *wailsLogger) Info(message string)  { w.log.Info(message) }
func (w *wailsLogger) Warning(message string) {
	w.log.Warn(message)
}
func (w *wailsLogger) Error(message string) { w.log.Error(message) }
func (w *wailsLogger) Fatal(message string) { w.log.Error(message) }

var _ logger.Logger = (*wailsLogger)(nil)

func usage() {
	fmt.Fprint(os.Stderr, `snp-desktop — snp desktop app (Wails)

usage: snp-desktop [flags]

The desktop app is a local instance of snp: same store, same UI, no
tailnet and no HTTP server. State lives under the config state-dir
(default: ~/.local/share/snp), shared with the snp CLI.

flags:
  -config     path to config file
  -hostname   ignored (no tailnet node)
  -owner      ignored (no HTTP surface to authenticate)
  -state-dir  state directory (default: ~/.local/share/snp)
  -log-level  debug, info, warn, error (default: info)
  -debug      debug logging + startup phase timing
  -version    print the snp-desktop version and exit
`)
}

func main() {
	fs := flag.NewFlagSet("snp-desktop", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.String("config", "", "path to config file")
	fs.String("hostname", "", "ignored in desktop mode")
	fs.String("owner", "", "ignored in desktop mode")
	fs.String("state-dir", "", "state directory (default: ~/.local/share/snp)")
	fs.String("log-level", "", "debug, info, warn, error (default: info)")
	fs.String("ai-endpoint", "", "OpenAI-compatible base URL (default: https://api.openai.com/v1)")
	fs.String("ai-model", "", "model name (default: gpt-4o-mini)")
	fs.String("ai-key", "", "API key; setting it enables the AI feature")
	debug := fs.Bool("debug", false, "debug logging + startup phase timing")
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(os.Args[1:]); err != nil {
		usage()
		os.Exit(2)
	}
	if *showVersion {
		fmt.Println("snp-desktop", buildinfo.String())
		return
	}
	if fs.NArg() != 0 {
		usage()
		os.Exit(2)
	}
	cfg, err := config.LoadDesktop(fs)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snp-desktop:", err)
		os.Exit(1)
	}
	level := cfg.LogLevel
	if *debug {
		level = "debug"
	}
	if err := run(cfg, newLogger(level)); err != nil {
		fmt.Fprintln(os.Stderr, "snp-desktop:", err)
		os.Exit(1)
	}
}

func newLogger(level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}

// logLevel maps snp's log-level strings to wails levels.
func logLevel(level string) logger.LogLevel {
	switch level {
	case "debug":
		return logger.DEBUG
	case "warn":
		return logger.WARNING
	case "error":
		return logger.ERROR
	default:
		return logger.INFO
	}
}

func run(cfg config.Config, log *slog.Logger) error {
	t0 := time.Now()
	log.Info("snp-desktop starting",
		"state_dir", cfg.StateDir,
		"db", filepath.Join(cfg.StateDir, "snp.db"),
		"ai_enabled", cfg.AIEnabled(),
		"pid", os.Getpid())
	handler, st, err := desktop.NewHandler(cfg, log)
	if err != nil {
		return err
	}
	defer st.Close()

	// The purger mirrors the serve subcommand: run at startup and daily,
	// stopped before the store closes.
	purgerCtx, cancelPurger := context.WithCancel(context.Background())
	purgerDone := st.StartPurger(purgerCtx, log)

	app := desktop.NewApp(handler, log)
	opts := &options.App{
		Title:  "snp",
		Width:  1150,
		Height: 760,
		// Below the SPA's 720px compact breakpoint (web/src/lib/layout.ts),
		// so a narrowed window gets the compact layout under Auto (spec §6).
		MinWidth:  400,
		MinHeight: 560,
		// The SPA theme background (#0f172a) behind the first paint.
		BackgroundColour: options.NewRGB(15, 23, 42),
		// Serve the embedded SPA from the wails asset server; the SPA's
		// /api calls are intercepted by the desktop bridge, so no proxy
		// or HTTP listener is involved. The middleware stamps the shell
		// with a window.__SNP_DESKTOP__ marker so the frontend routes
		// through the bridge instead of fetch.
		AssetServer: &assetserver.Options{
			Assets:     webembed.FS,
			Middleware: injectDesktopMarker(),
		},
		Logger:   &wailsLogger{log},
		LogLevel: logLevel(cfg.LogLevel),
		Bind:     []interface{}{app},
		OnShutdown: func(ctx context.Context) {
			cancelPurger()
		},
	}
	log.Info("snp-desktop: creating window",
		"elapsed_ms", time.Since(t0).Milliseconds())
	addPlatformOptions(opts)
	err = wails.Run(opts)
	log.Info("snp-desktop: window closed",
		"total_ms", time.Since(t0).Milliseconds(), "err", err)
	cancelPurger()
	<-purgerDone
	return err
}

// injectDesktopMarker returns asset-server middleware that stamps the
// app shell with window.__SNP_DESKTOP__ = true, so the frontend knows a
// desktop bridge exists before any of it is needed. Everything that is
// not the shell is served untouched by the default handler.
func injectDesktopMarker() assetserver.Middleware {
	return func(next http.Handler) http.Handler {
		marker := "<script>window.__SNP_DESKTOP__=true</script>"
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || (r.URL.Path != "/" && r.URL.Path != "/index.html") {
				next.ServeHTTP(w, r)
				return
			}
			raw, err := fs.ReadFile(webembed.FS, "index.html")
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}
			html := string(raw)
			// Drop the PWA service-worker registration in the desktop
			// shell: there is no offline/PWA role here, and registering a
			// service worker against the wails:// scheme rejects (an
			// unhandled promise rejection in the page).
			html = strings.ReplaceAll(
				html,
				`<script id="vite-plugin-pwa:register-sw" src="/registerSW.js"></script>`,
				"",
			)
			if !strings.Contains(html, "__SNP_DESKTOP__") {
				if i := strings.Index(html, "</head>"); i >= 0 {
					html = html[:i] + marker + html[i:]
				} else {
					html = marker + html
				}
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		})
	}
}

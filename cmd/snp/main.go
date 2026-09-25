// Command snp is the single binary for the snippet manager: the
// tailnet node (serve) plus the backup/export/import/key utilities.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/buildinfo"
	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/server"
	"github.com/jstevewhite/snp/internal/starter"
	"github.com/jstevewhite/snp/internal/store"
	"github.com/jstevewhite/snp/internal/tsauth"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "serve":
		runServe(args)
	case "backup":
		runBackup(args)
	case "export":
		runExport(args)
	case "import":
		runImport(args)
	case "decrypt":
		runDecrypt(args)
	case "seed":
		runSeed(args)
	case "doctor":
		runDoctor(args)
	case "key":
		runKey(args)
	case "version", "-v", "--version":
		fmt.Println("snp", buildinfo.String())
	case "help", "-h", "--help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `snp — snippet manager

usage: snp <command> [flags]

commands:
  serve       run the tailnet node and HTTP server
  backup      write a consistent database copy (VACUUM INTO + quick_check)
  export      write a full JSON export (plaintext bodies)
  import      import a JSON export document
  decrypt     unlock a password-protected JSON export or backup ZIP
  seed        add the bundled starter snippets (nothing seeds by itself)
  doctor      check library health and repair the search index
  key         key management (show-path)
  version     print the snp version

run "snp <command> -h" for command flags.
`)
}

// addConfigFlags registers the standard config flags (spec §2).
func addConfigFlags(fs *flag.FlagSet) {
	fs.String("config", "", "path to config file (default: $SNP_CONFIG, $XDG_CONFIG_HOME/snp/config.toml, ~/.config/snp/config.toml)")
	fs.String("hostname", "", "tailnet node name (default: snp)")
	fs.String("owner", "", "tailscale login allowed in")
	fs.String("state-dir", "", "state directory (default: ~/.local/share/snp)")
	fs.String("log-level", "", "log level: debug, info, warn, error (default: info)")
	fs.String("dev-listen", "", "dev mode: plain HTTP on addr, no tsnet, no auth")
	fs.String("ai-endpoint", "", "OpenAI-compatible base URL (default: https://api.openai.com/v1)")
	fs.String("ai-model", "", "model name (default: gpt-4o-mini)")
	fs.String("ai-key", "", "API key; setting it enables the AI feature")
}

// newSubFlags builds a FlagSet with the standard config flags.
func newSubFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	addConfigFlags(fs)
	return fs
}

// mustConfig loads the configuration or exits with status 1.
func mustConfig(fs *flag.FlagSet) config.Config {
	cfg, err := config.Load(fs)
	if err != nil {
		fatal(err)
	}
	return cfg
}

// fatal prints the error and exits with status 1.
func fatal(err error) {
	fmt.Fprintln(os.Stderr, "snp:", err)
	os.Exit(1)
}

// usageError prints a usage line and exits with status 2.
func usageError(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(2)
}

// dbPath returns the database path under the state directory.
func dbPath(cfg config.Config) string {
	return filepath.Join(cfg.StateDir, "snp.db")
}

// keyPath returns the key file path under the state directory.
func keyPath(cfg config.Config) string {
	return filepath.Join(cfg.StateDir, "key")
}

// openStore opens the database; when withKey it also loads (creating
// if needed) the encryption key and attaches it.
func openStore(cfg config.Config, withKey bool) (*store.Store, error) {
	st, err := store.Open(dbPath(cfg))
	if err != nil {
		return nil, err
	}
	if !withKey {
		return st, nil
	}
	k, err := store.LoadOrCreateKey(keyPath(cfg))
	if err != nil {
		st.Close()
		return nil, err
	}
	st.SetKey(k)
	return st, nil
}

// newLogger builds the structured stdout logger at the configured level
// (spec §7).
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

// devListenAddr resolves a --dev-listen address to a loopback-only
// address. Dev mode is plain HTTP with no tsnet and no auth (spec §1),
// so binding to anything but loopback would expose every snippet to the
// LAN; Go binds an empty host (":8080") to every interface, which is the
// footgun this prevents.
func devListenAddr(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("dev-listen: invalid address %q (want host:port): %w", addr, err)
	}
	switch host {
	case "", "localhost":
		// Bind the IPv4 loopback explicitly.
		return net.JoinHostPort("127.0.0.1", port), nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return addr, nil
	}
	return "", fmt.Errorf("dev-listen: refusing %q: dev mode is unauthenticated and must stay on loopback (use 127.0.0.1:%s)", addr, port)
}

func runServe(args []string) {
	fs := newSubFlags("serve")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	cfg, err := config.Load(fs)
	if err != nil {
		fatal(err)
	}

	log := newLogger(cfg.LogLevel)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := os.MkdirAll(cfg.StateDir, 0o700); err != nil {
		fatal(err)
	}

	st, err := openStore(cfg, true)
	if err != nil {
		fatal(err)
	}
	defer st.Close()

	var (
		ln     net.Listener
		resolv tsauth.IdentityResolver
		owner  = cfg.Owner
	)
	if cfg.DevListen != "" {
		// Dev mode: plain HTTP on loopback only, no tsnet, no auth
		// (spec §1).
		devAddr, err := devListenAddr(cfg.DevListen)
		if err != nil {
			fatal(err)
		}
		ln, err = net.Listen("tcp", devAddr)
		if err != nil {
			fatal(err)
		}
		resolv = tsauth.NewDev()
		owner = ""
	} else {
		ts, err := tsauth.New(cfg.StateDir, cfg.Hostname, os.Getenv("TS_AUTHKEY"))
		if err != nil {
			fatal(err)
		}
		ln, err = ts.Listen(ctx)
		if err != nil {
			fatal(err)
		}
		defer ts.Close()
		resolv = ts
	}

	srv := server.NewWithAI(st, resolv, owner, log, ai.FromConfig(cfg, log))
	// The doctor endpoint inspects the key file's permissions; without the
	// path it would report that check as skipped.
	srv.SetKeyPath(keyPath(cfg))
	purgerDone := st.StartPurger(ctx, log)

	// Any tailnet peer can reach the listener, so header reads and idle
	// connections are bounded (slow-loris hygiene); the largest responses
	// (export) are still written well within these limits.
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpSrv.Serve(ln)
	}()
	log.Info("listening", "addr", ln.Addr().String())

	select {
	case <-ctx.Done():
		log.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpSrv.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown failed", "err", err)
		}
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			fatal(err)
		}
	}
	// Stop the purger and wait for it to finish before the deferred
	// st.Close(): a purge running across the close would log
	// "database is closed" and could leave a half-applied day's purge.
	stop()
	<-purgerDone
}

// runBackup writes a consistent database copy to <dest> and verifies it.
// Quiet on success (cron-friendly); errors go to stderr with exit 1.
func runBackup(args []string) {
	fs := newSubFlags("backup")
	fullCheck := fs.Bool("full-check", false, "run the full integrity_check instead of quick_check")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		usageError("usage: snp backup [flags] <dest>")
	}
	cfg := mustConfig(fs)
	if err := backupCmd(cfg, fs.Arg(0), *fullCheck); err != nil {
		fatal(err)
	}
}

// backupCmd performs the backup; the testable core of runBackup.
// It refuses to run when no database exists yet: store.Open would
// create an empty one, and a cron job silently "backing up" an empty
// database after a state-dir typo is a footgun.
func backupCmd(cfg config.Config, dest string, fullCheck bool) error {
	db := dbPath(cfg)
	if _, err := os.Stat(db); err != nil {
		return fmt.Errorf("backup: no database at %s (has snp serve been run?)", db)
	}
	st, err := openStore(cfg, false)
	if err != nil {
		return err
	}
	defer st.Close()
	return st.Backup(dest, fullCheck)
}

// runExport writes a full JSON export (plaintext bodies), pretty-
// printed, to stdout or -o file.
func runExport(args []string) {
	fs := newSubFlags("export")
	outPath := fs.String("o", "", "write to file instead of stdout")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		usageError("usage: snp export [flags] [-o file]")
	}
	cfg := mustConfig(fs)
	var out io.Writer = os.Stdout
	if *outPath != "" {
		f, err := os.Create(*outPath)
		if err != nil {
			fatal(err)
		}
		defer f.Close()
		out = f
	}
	if err := exportCmd(cfg, out); err != nil {
		fatal(err)
	}
}

// exportCmd writes the export document to out; the testable core of
// runExport.
func exportCmd(cfg config.Config, out io.Writer) error {
	st, err := openStore(cfg, true)
	if err != nil {
		return err
	}
	defer st.Close()
	doc, err := st.Export()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	_, err = io.WriteString(out, buf.String())
	return err
}

// runImport applies a JSON export document from <file>.
func runImport(args []string) {
	fs := newSubFlags("import")
	mode := fs.String("mode", "merge", "import mode: merge or replace")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		usageError("usage: snp import [flags] <file>")
	}
	cfg := mustConfig(fs)
	res, err := importCmd(cfg, fs.Arg(0), *mode)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("imported: %d created, %d updated\n", res.Created, res.Updated)
}

// importCmd reads and applies the document; the testable core of
// runImport.
func importCmd(cfg config.Config, path, mode string) (store.ImportResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return store.ImportResult{}, err
	}
	var doc store.ImportDoc
	if err := json.Unmarshal(data, &doc); err != nil {
		return store.ImportResult{}, fmt.Errorf("parse %s: %w", path, err)
	}
	st, err := openStore(cfg, true)
	if err != nil {
		return store.ImportResult{}, err
	}
	defer st.Close()
	return st.Import(doc, mode)
}

// runSeed applies the bundled starter pack (spec §5). Explicit only: the
// pack is never applied automatically, because import's merge mode clears
// deleted_at and would resurrect snippets the user had deleted.
func runSeed(args []string) {
	fs := newSubFlags("seed")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		usageError("usage: snp seed [flags]")
	}
	res, err := seedCmd(mustConfig(fs))
	if err != nil {
		fatal(err)
	}
	fmt.Printf("starter pack: %d created, %d updated\n", res.Created, res.Updated)
}

// seedCmd applies the starter pack; the testable core of runSeed.
func seedCmd(cfg config.Config) (store.ImportResult, error) {
	st, err := openStore(cfg, true)
	if err != nil {
		return store.ImportResult{}, err
	}
	defer st.Close()
	doc, err := starter.Pack()
	if err != nil {
		return store.ImportResult{}, err
	}
	return st.Import(doc, "merge")
}

// runKey implements "snp key show-path": print the key file path for
// backup scripts.
func runKey(args []string) {
	if len(args) == 0 || args[0] != "show-path" {
		usageError("usage: snp key show-path [flags]")
	}
	fs := newSubFlags("key")
	if err := fs.Parse(args[1:]); err != nil {
		os.Exit(2)
	}
	cfg := mustConfig(fs)
	fmt.Println(keyPath(cfg))
}

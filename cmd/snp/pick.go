package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/pick"
	"github.com/jstevewhite/snp/internal/store"
)

// runPick is `snp pick`. It draws on the terminal and prints the accepted
// command to stdout. Cancel and usage problems exit non-zero and print
// nothing on stdout, so a shell widget can tell them from a command.
func runPick(args []string) {
	fs := newSubFlags("pick")
	fs.String("url", "", "remote library (http or https); overrides the url config key")
	local := fs.Bool("local", false, "read the local database, ignoring url")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		usageError("usage: snp pick [--url URL | --local]")
	}
	cfg, err := config.LoadDesktop(fs)
	if err != nil {
		fatal(err)
	}
	lib, err := openPickLibrary(cfg, *local)
	if err != nil {
		fatal(err)
	}
	err = pick.Run(context.Background(), lib, os.Stdout)
	closePickLibrary(lib)
	if errors.Is(err, pick.ErrCanceled) {
		os.Exit(1)
	}
	if err != nil {
		fatal(err)
	}
}

// openPickLibrary follows --local, then url, then the local database.
func openPickLibrary(cfg config.Config, forceLocal bool) (pick.Library, error) {
	if !forceLocal && cfg.URL != "" {
		return pick.NewHTTP(cfg.URL)
	}
	path := dbPath(cfg)
	if _, err := os.Stat(path); err != nil {
		if forceLocal {
			return nil, fmt.Errorf("no local database at %s", path)
		}
		return nil, fmt.Errorf("no library: set url, or create a database at %s", path)
	}
	st, err := openStore(cfg, false)
	if err != nil {
		return nil, err
	}
	if key, err := store.LoadKey(keyPath(cfg)); err == nil {
		st.SetKey(key)
	} else if !os.IsNotExist(err) {
		st.Close()
		return nil, err
	}
	return pick.Local{Store: st}, nil
}

func closePickLibrary(lib pick.Library) {
	if local, ok := lib.(pick.Local); ok && local.Store != nil {
		local.Store.Close()
	}
}

// runWidget prints the zsh binding for snp pick.
func runWidget(args []string) {
	fs := newSubFlags("widget")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		usageError("usage: snp widget")
	}
	fmt.Print(pick.Widget)
}

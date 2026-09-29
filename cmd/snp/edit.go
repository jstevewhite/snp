package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/ask"
	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/edit"
	"github.com/jstevewhite/snp/internal/pick"
	"github.com/jstevewhite/snp/internal/store"
)

// openAskService follows --local, then url, then the local AI config —
// the same transport rule as the editor. st is the local database for
// the tag vocabulary (Local.SuggestTags); nil when there is no library
// (`snp ask` standalone). The provider client gets no logger: slog
// writes to stdout, which would break `out=$(snp ask ...)`, and the AI
// rule permits only status and duration anyway.
func openAskService(cfg config.Config, forceLocal bool, st *store.Store) (ask.Service, error) {
	if !forceLocal && cfg.URL != "" {
		return ask.NewHTTP(cfg.URL)
	}
	return ask.Local{Client: ai.FromConfig(cfg, nil), Store: st}, nil
}

// editorStore returns the local database behind the editor's library,
// nil in --url mode.
func editorStore(ed pick.Editor) *store.Store {
	if l, ok := ed.(*lazyKeyEditor); ok {
		return l.st
	}
	return nil
}

// runAdd is `snp add`: the editor panel prefilled from flags, a create.
// On save it exits 0 and prints nothing, like pick.
func runAdd(args []string) {
	fs := newSubFlags("add")
	fs.String("url", "", "remote library (http or https); overrides the url config key")
	local := fs.Bool("local", false, "write the local database, ignoring url")
	title := fs.String("title", "", "snippet title")
	language := fs.String("language", "", "language label, e.g. bash")
	folder := fs.String("folder", "", "folder id or parent/child path")
	tags := fs.String("tags", "", "comma-separated tags")
	sensitive := fs.Bool("sensitive", false, "encrypt the body")
	pin := fs.Bool("pin", false, "favorite the snippet")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		usageError("usage: snp add [--url URL | --local] [flags]")
	}
	cfg, err := config.LoadDesktop(fs)
	if err != nil {
		fatal(err)
	}
	ed, done, err := openEditorLibrary(cfg, *local)
	if err != nil {
		fatal(err)
	}
	prefill := edit.Prefill{
		Title:     *title,
		Language:  *language,
		Tags:      splitTags(*tags),
		Sensitive: *sensitive,
		Pinned:    *pin,
	}
	if prefill.FolderID, err = resolveFolderID(context.Background(), ed, *folder); err != nil {
		done()
		fatal(err)
	}
	svc, err := openAskService(cfg, *local, editorStore(ed))
	if err != nil {
		done()
		fatal(err)
	}
	_, err = edit.RunCreate(context.Background(), ed, svc, prefill)
	done()
	if errors.Is(err, edit.ErrCanceled) {
		os.Exit(1)
	}
	if err != nil {
		fatal(err)
	}
}

// runEdit is `snp edit`: the panel on one row. With no argument the
// chooser lists the library; an argument is an id or a search query —
// one hit edits it, several open the chooser.
func runEdit(args []string) {
	fs := newSubFlags("edit")
	fs.String("url", "", "remote library (http or https); overrides the url config key")
	local := fs.Bool("local", false, "write the local database, ignoring url")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 1 {
		usageError("usage: snp edit [--url URL | --local] [id | query]")
	}
	cfg, err := config.LoadDesktop(fs)
	if err != nil {
		fatal(err)
	}
	ed, done, err := openEditorLibrary(cfg, *local)
	if err != nil {
		fatal(err)
	}
	row, err := resolveTarget(context.Background(), ed, fs.Arg(0))
	if err != nil {
		done()
		fatal(err)
	}
	svc, err := openAskService(cfg, *local, editorStore(ed))
	if err != nil {
		done()
		fatal(err)
	}
	_, err = edit.Run(context.Background(), ed, svc, &row)
	done()
	if errors.Is(err, edit.ErrCanceled) {
		os.Exit(1)
	}
	if err != nil {
		fatal(err)
	}
}

// openEditorLibrary follows --local, then url, then the local database.
// Unlike the picker it may create the database (a first `snp add` on a
// fresh machine), and it attaches the encryption key lazily, so a plain
// create or a metadata-only edit never writes a key file.
func openEditorLibrary(cfg config.Config, forceLocal bool) (pick.Editor, func(), error) {
	if !forceLocal && cfg.URL != "" {
		h, err := pick.NewHTTP(cfg.URL)
		if err != nil {
			return nil, nil, err
		}
		return h, func() {}, nil
	}
	st, err := openStore(cfg, false)
	if err != nil {
		return nil, nil, err
	}
	return &lazyKeyEditor{Editor: pick.Local{Store: st}, st: st, path: keyPath(cfg)}, func() { st.Close() }, nil
}

// lazyKeyEditor attaches the encryption key the first time a call needs
// it (store.ErrNoKey) and retries once. This is the one place the
// picker's "never create" rule does not carry over: a sensitive body to
// read or write creates the key file on demand.
type lazyKeyEditor struct {
	pick.Editor
	st   *store.Store
	path string
	have bool
}

func (l *lazyKeyEditor) ensureKey() error {
	if l.have {
		return nil
	}
	k, err := store.LoadOrCreateKey(l.path)
	if err != nil {
		return err
	}
	l.st.SetKey(k)
	l.have = true
	return nil
}

func (l *lazyKeyEditor) Get(ctx context.Context, id string) (pick.Snippet, error) {
	s, err := l.Editor.Get(ctx, id)
	if err == nil || !errors.Is(err, store.ErrNoKey) {
		return s, err
	}
	if err := l.ensureKey(); err != nil {
		return pick.Snippet{}, err
	}
	return l.Editor.Get(ctx, id)
}

func (l *lazyKeyEditor) Create(ctx context.Context, in pick.Input) (pick.Snippet, error) {
	s, err := l.Editor.Create(ctx, in)
	if err == nil || !errors.Is(err, store.ErrNoKey) {
		return s, err
	}
	if err := l.ensureKey(); err != nil {
		return pick.Snippet{}, err
	}
	return l.Editor.Create(ctx, in)
}

func (l *lazyKeyEditor) Update(ctx context.Context, id string, in pick.Input) (pick.Snippet, error) {
	s, err := l.Editor.Update(ctx, id, in)
	if err == nil || !errors.Is(err, store.ErrNoKey) {
		return s, err
	}
	if err := l.ensureKey(); err != nil {
		return pick.Snippet{}, err
	}
	return l.Editor.Update(ctx, id, in)
}

// resolveTarget turns the edit argument into the full row to edit: an
// empty argument runs the chooser; an id that resolves is edited
// directly; anything else is a search — one hit edits it, several open
// the chooser. Search results omit sensitive bodies, so a sensitive row
// is fetched whole (which is what attaches the key).
func resolveTarget(ctx context.Context, ed pick.Editor, arg string) (pick.Snippet, error) {
	if arg == "" {
		s, err := pick.Choose(ctx, ed)
		if err != nil || s.Sensitive {
			return fullRow(ctx, ed, s, err)
		}
		return s, nil
	}
	if s, err := ed.Get(ctx, arg); err == nil {
		return s, nil
	}
	hits, err := ed.Search(ctx, arg)
	if err != nil {
		return pick.Snippet{}, err
	}
	switch len(hits) {
	case 0:
		return pick.Snippet{}, fmt.Errorf("no snippet matches %q", arg)
	case 1:
		return fullRow(ctx, ed, hits[0], nil)
	}
	s, err := pick.Choose(ctx, ed)
	if err != nil || s.Sensitive {
		return fullRow(ctx, ed, s, err)
	}
	return s, nil
}

// fullRow fetches the whole row when the search hit omits its body
// (sensitive rows); a plain hit is already complete.
func fullRow(ctx context.Context, ed pick.Editor, s pick.Snippet, err error) (pick.Snippet, error) {
	if err != nil {
		return pick.Snippet{}, err
	}
	if !s.Sensitive {
		return s, nil
	}
	return ed.Get(ctx, s.ID)
}

// resolveFolderID resolves the --folder flag against the live tree: a
// folder id, else a parent/child path. Folder creation is out of scope,
// so a spec that matches nothing is an error.
func resolveFolderID(ctx context.Context, ed pick.Editor, spec string) (*string, error) {
	if spec == "" {
		return nil, nil
	}
	folders, err := ed.Folders(ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range folders {
		if f.ID == spec {
			id := f.ID
			return &id, nil
		}
	}
	for _, f := range folders {
		if edit.FolderPath(folders, f.ID) == spec {
			id := f.ID
			return &id, nil
		}
	}
	return nil, fmt.Errorf("no folder %q (by id or parent/child path)", spec)
}

// splitTags splits a comma-separated flag value, dropping empties.
func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

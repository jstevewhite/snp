package edit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/pick"
)

// fakeEditor is the write-path fake: the picker's memLib pattern extended
// to the full Editor surface. err set makes every write fail.
type fakeEditor struct {
	snippet pick.Snippet
	folders []pick.Folder
	tags    []pick.TagCount
	creates []pick.Input
	updates []updateCall
	err     error
}

type updateCall struct {
	id string
	in pick.Input
}

func (f *fakeEditor) Search(ctx context.Context, q string) ([]pick.Snippet, error) {
	return nil, nil
}
func (f *fakeEditor) Reveal(ctx context.Context, id string) (pick.Snippet, error) {
	return f.Get(ctx, id)
}
func (f *fakeEditor) Get(ctx context.Context, id string) (pick.Snippet, error) {
	if id != f.snippet.ID {
		return pick.Snippet{}, errors.New("missing")
	}
	return f.snippet, nil
}
func (f *fakeEditor) Folders(ctx context.Context) ([]pick.Folder, error) {
	return f.folders, nil
}
func (f *fakeEditor) Tags(ctx context.Context) ([]pick.TagCount, error) {
	return f.tags, nil
}
func (f *fakeEditor) Create(ctx context.Context, in pick.Input) (pick.Snippet, error) {
	if f.err != nil {
		return pick.Snippet{}, f.err
	}
	f.creates = append(f.creates, in)
	return pick.Snippet{ID: "n1", Title: in.Title, Body: in.Body}, nil
}
func (f *fakeEditor) Update(ctx context.Context, id string, in pick.Input) (pick.Snippet, error) {
	if f.err != nil {
		return pick.Snippet{}, f.err
	}
	f.updates = append(f.updates, updateCall{id: id, in: in})
	return f.snippet, nil
}

func apply(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func press(code rune, text string, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text, Mod: mod}
}

func enter() tea.KeyPressMsg { return press(tea.KeyEnter, "", 0) }

// call runs cmd but waits only briefly: the cursor-blink commands that
// Focus() emits sleep their whole blink interval before yielding a msg,
// and the suite must not wait on them. Their state effect happens inside
// Focus() itself; the cmd only starts blinking.
func call(cmd tea.Cmd) tea.Msg {
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(50 * time.Millisecond):
		return nil
	}
}

// drain runs a cmd (including tea.BatchMsg) against the model. Each leaf
// msg is applied once; chasing the blink tick chain would wait on real
// time.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	msg := call(cmd)
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, c := range batch {
			m = drain(t, m, c)
		}
		return m
	}
	if msg == nil {
		return m
	}
	m, _ = apply(m, msg)
	return m
}

func start(t *testing.T, ed pick.Editor, seed *pick.Snippet) Model {
	t.Helper()
	m := New(context.Background(), ed, seed)
	return drain(t, m, m.Init())
}

func ctrlS() tea.KeyPressMsg { return press('s', "s", tea.ModCtrl) }

func seedSnippet() *pick.Snippet {
	folder := "f1"
	return &pick.Snippet{
		ID: "s1", Title: "restart", Body: "sudo systemctl restart caddy",
		Language: "bash", Notes: "after the Caddyfile", Tags: []string{"ops"},
		FolderID: &folder, Pinned: true,
		VarDefaults: map[string]string{"gone": "1"},
	}
}

func TestTabRingAndToggles(t *testing.T) {
	m := start(t, &fakeEditor{}, nil)
	if m.focus != stopTitle {
		t.Fatalf("focus = %d", m.focus)
	}
	stops := []stop{stopBody, stopNotes, stopLanguage, stopTags, stopSensitive, stopPinned}
	for _, want := range stops {
		m, _ = apply(m, press(tea.KeyTab, "", 0))
		if m.focus != want {
			t.Fatalf("tab: focus = %d, want %d", m.focus, want)
		}
	}
	// Forward wrap: tab from pinned lands on title, then shift-tab wraps
	// back to pinned.
	m, _ = apply(m, press(tea.KeyTab, "", 0))
	if m.focus != stopTitle {
		t.Fatalf("tab wrap: focus = %d", m.focus)
	}
	m, _ = apply(m, press(tea.KeyTab, "", tea.ModShift))
	if m.focus != stopPinned {
		t.Fatalf("shift-tab wrap: focus = %d", m.focus)
	}
	// Space toggles the boolean stops.
	for _, s := range []stop{stopSensitive, stopPinned} {
		for m.focus != s {
			m, _ = apply(m, press(tea.KeyTab, "", 0))
		}
		m, _ = apply(m, press(' ', " ", 0))
	}
	if !m.sensitive || !m.pinned {
		t.Fatalf("toggles: sensitive=%v pinned=%v", m.sensitive, m.pinned)
	}
}

func TestEnterAdvancesSingleLineOnly(t *testing.T) {
	m := start(t, &fakeEditor{}, nil)
	m, _ = apply(m, enter())
	if m.focus != stopBody {
		t.Fatalf("enter: focus = %d", m.focus)
	}
	// In the body textarea, Enter is a newline, not a save or a jump.
	m, _ = apply(m, press('x', "x", 0))
	m, _ = apply(m, enter())
	if !strings.Contains(m.body.Value(), "x\n") {
		t.Fatalf("body = %q", m.body.Value())
	}
	if m.quitting {
		t.Fatal("enter in body quit")
	}
}

func TestSaveCreateAppliesGUIRules(t *testing.T) {
	ed := &fakeEditor{}
	m := start(t, ed, nil)
	m.title.SetValue("  spaced  ")
	m.language.SetValue("  bash ")
	m.notes.SetValue("  a note  ")
	m.tags.SetValue(" ops , bash ,")
	m.pinned = true
	m, cmd := apply(m, ctrlS())
	if cmd == nil {
		t.Fatal("no save cmd")
	}
	m, _ = apply(m, cmd())
	if len(ed.creates) != 1 {
		t.Fatalf("creates = %d", len(ed.creates))
	}
	in := ed.creates[0]
	if in.Title != "spaced" || in.Language != "bash" || in.Notes != "a note" {
		t.Fatalf("trims = %+v", in)
	}
	if len(in.Tags) != 2 || in.Tags[0] != "ops" || in.Tags[1] != "bash" {
		t.Fatalf("tags = %+v", in.Tags)
	}
	if !in.Pinned {
		t.Fatalf("flags = %+v", in)
	}
	if in.FolderID != nil {
		t.Fatalf("create folder = %v", *in.FolderID)
	}
	s, ok := m.Result()
	if !ok || s.ID != "n1" || !m.quitting {
		t.Fatalf("result = %+v ok=%v quit=%v", s, ok, m.quitting)
	}
}

func TestSaveUpdateCarriesUntouchedFields(t *testing.T) {
	ed := &fakeEditor{snippet: *seedSnippet(), folders: []pick.Folder{
		{ID: "p1", Name: "ops"},
		{ID: "f1", ParentID: strPtr("p1"), Name: "deploy"},
	}}
	m := start(t, ed, seedSnippet())
	if m.folderName != "ops/deploy" {
		t.Fatalf("folderName = %q", m.folderName)
	}
	// Only the title is edited; every other field must still be sent.
	m.title.SetValue("restart caddy")
	m, cmd := apply(m, ctrlS())
	m, _ = apply(m, cmd())
	if len(ed.updates) != 1 || ed.updates[0].id != "s1" {
		t.Fatalf("updates = %+v", ed.updates)
	}
	in := ed.updates[0].in
	seed := seedSnippet()
	if in.Body != seed.Body || in.Notes != seed.Notes || in.Language != seed.Language {
		t.Fatalf("carried text = %+v", in)
	}
	if in.FolderID == nil || *in.FolderID != "f1" || !in.Pinned {
		t.Fatalf("carried folder/pin = %+v", in)
	}
	if len(in.Tags) != 1 || in.Tags[0] != "ops" {
		t.Fatalf("carried tags = %+v", in.Tags)
	}
	// var_defaults pruned to the variables the body still uses: "gone"
	// is not in the body, so the map is empty.
	if len(in.VarDefaults) != 0 {
		t.Fatalf("defaults = %+v", in.VarDefaults)
	}
}

func TestDerivedTemplateFlagAndPruning(t *testing.T) {
	seed := seedSnippet()
	seed.VarDefaults = map[string]string{"host": "nas", "gone": "x", "blank": ""}
	ed := &fakeEditor{snippet: *seed}
	m := start(t, ed, seed)
	m.body.SetValue("ssh {{host}}")
	if !strings.Contains(m.View().Content, "Template: host") {
		t.Fatalf("view lacks var list: %s", m.View().Content)
	}
	m, cmd := apply(m, ctrlS())
	m, _ = apply(m, cmd())
	in := ed.updates[0].in
	if !in.UsesVariables {
		t.Fatal("uses_variables not derived")
	}
	// Kept: used and non-empty. Dropped: unused, and blank.
	if in.VarDefaults["host"] != "nas" || len(in.VarDefaults) != 1 {
		t.Fatalf("defaults = %+v", in.VarDefaults)
	}
}

func TestFailedWriteKeepsDraft(t *testing.T) {
	ed := &fakeEditor{err: errors.New("library: invalid tag name \"bad tag\"")}
	m := start(t, ed, nil)
	m.title.SetValue("t")
	m, cmd := apply(m, ctrlS())
	m, _ = apply(m, cmd())
	if m.quitting || m.saving {
		t.Fatalf("quit=%v saving=%v", m.quitting, m.saving)
	}
	if m.err != `library: invalid tag name "bad tag"` {
		t.Fatalf("err = %q", m.err)
	}
	if !strings.Contains(m.View().Content, "invalid tag name") {
		t.Fatal("view lacks the error")
	}
	if m.title.Value() != "t" {
		t.Fatal("draft lost")
	}
	// Retry succeeds once the error clears.
	ed.err = nil
	m, cmd = apply(m, ctrlS())
	m, _ = apply(m, cmd())
	if _, ok := m.Result(); !ok || !m.quitting {
		t.Fatalf("retry: ok=%v quit=%v", ok, m.quitting)
	}
}

func TestTitleRequiredBlocksWrite(t *testing.T) {
	ed := &fakeEditor{}
	m := start(t, ed, nil)
	m.body.SetValue("body")
	m, cmd := apply(m, ctrlS())
	if cmd != nil {
		t.Fatal("write attempted with blank title")
	}
	if m.err != "title required" || len(ed.creates) != 0 {
		t.Fatalf("err = %q creates = %d", m.err, len(ed.creates))
	}
	if m.quitting {
		t.Fatal("quit on validation")
	}
}

func TestSensitiveMaskedUntilRevealed(t *testing.T) {
	seed := seedSnippet()
	seed.Sensitive = true
	seed.Body = "hunter2"
	ed := &fakeEditor{snippet: *seed}
	m := start(t, ed, seed)
	if strings.Contains(m.View().Content, "hunter2") {
		t.Fatal("view showed the secret")
	}
	// Save without reveal carries the seed body, never the empty box.
	m, cmd := apply(m, ctrlS())
	m, _ = apply(m, cmd())
	if in := ed.updates[0].in; in.Body != "hunter2" || !in.IsSensitive {
		t.Fatalf("masked save = %+v", in)
	}
	// Ctrl+R reveals; then the box edits for real.
	m2 := start(t, ed, seed)
	m2, _ = apply(m2, press('r', "r", tea.ModCtrl))
	if m2.body.Value() != "hunter2" {
		t.Fatalf("revealed body = %q", m2.body.Value())
	}
	m2.body.SetValue("hunter3")
	m2, cmd = apply(m2, ctrlS())
	m2, _ = apply(m2, cmd())
	if in := ed.updates[1].in; in.Body != "hunter3" {
		t.Fatalf("edited save = %+v", in)
	}
}

func TestDirtyGuardOnEsc(t *testing.T) {
	// Clean draft: Esc quits at once.
	m := start(t, &fakeEditor{}, nil)
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	if !m.quitting || m.asking {
		t.Fatalf("clean esc: quit=%v asking=%v", m.quitting, m.asking)
	}
	// Dirty draft: Esc asks; n keeps editing; y discards.
	m = start(t, &fakeEditor{}, nil)
	m.title.SetValue("draft")
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	if m.quitting || !m.asking {
		t.Fatalf("dirty esc: quit=%v asking=%v", m.quitting, m.asking)
	}
	m, _ = apply(m, press('n', "n", 0))
	if m.asking || m.quitting {
		t.Fatalf("n: asking=%v quit=%v", m.asking, m.quitting)
	}
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	m, _ = apply(m, press('y', "y", 0))
	if !m.quitting {
		t.Fatal("y did not discard")
	}
	if _, ok := m.Result(); ok {
		t.Fatal("discard produced a save")
	}
}

func TestFolderLabel(t *testing.T) {
	folders := []pick.Folder{
		{ID: "p1", Name: "ops"},
		{ID: "c1", ParentID: strPtr("p1"), Name: "deploy"},
	}
	if got := folderLabel(folders, "c1"); got != "ops/deploy" {
		t.Fatalf("label = %q", got)
	}
	if got := folderLabel(folders, "nope"); got != "" {
		t.Fatalf("unknown = %q", got)
	}
}

func strPtr(s string) *string { return &s }

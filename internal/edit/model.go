package edit

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/pick"
	"github.com/jstevewhite/snp/internal/template"
	"github.com/jstevewhite/snp/internal/tui"
)

// stop is one focus stop in the panel's Tab ring. The folder picker (E4)
// joins the ring here; today the folder is displayed read-only and carried
// unchanged into every write.
type stop int

const (
	stopTitle stop = iota
	stopBody
	stopNotes
	stopLanguage
	stopTags
	stopSensitive
	stopPinned
	stopCount
)

// Model is the editor panel: one form mirroring the GUI editor (spec §6),
// the same fields, validation, and template / var_defaults rules.
type Model struct {
	ed  pick.Editor
	ctx context.Context

	// seed is the row being edited; nil means create. A full replace
	// sends every field back, so untouched state is carried from here.
	seed *pick.Snippet

	width  int
	height int
	dark   bool

	title    textinput.Model
	language textinput.Model
	tags     textinput.Model
	body     textarea.Model
	notes    textarea.Model

	focus     stop
	sensitive bool
	pinned    bool

	// A sensitive body is masked until revealed (Ctrl+R). While masked
	// the box stays empty and saves carry the seed body verbatim.
	revealed bool

	// folderID is carried unchanged; folderName is its display label.
	folderID   *string
	folderName string

	err     string
	asking   bool // dirty-quit confirm
	saving   bool // a write is in flight
	saved    *pick.Snippet
	quitting bool
}

// New builds the panel. seed nil opens the create form.
func New(ctx context.Context, ed pick.Editor, seed *pick.Snippet) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	title := textinput.New()
	title.Prompt = "Title      "
	title.CharLimit = 0
	language := textinput.New()
	language.Prompt = "Language   "
	language.Placeholder = "bash"
	tags := textinput.New()
	tags.Prompt = "Tags       "
	tags.Placeholder = "comma-separated"

	m := Model{
		ed:       ed,
		ctx:      ctx,
		seed:     seed,
		width:    80,
		height:   24,
		dark:     true,
		title:    title,
		language: language,
		tags:     tags,
	}
	m.body = textarea.New()
	m.body.Placeholder = "the snippet body"
	m.notes = textarea.New()
	m.notes.Placeholder = "notes (Markdown, rendered in the GUI read view)"

	if seed != nil {
		m.title.SetValue(seed.Title)
		m.language.SetValue(seed.Language)
		m.tags.SetValue(strings.Join(seed.Tags, ", "))
		m.notes.SetValue(seed.Notes)
		m.sensitive = seed.Sensitive
		m.pinned = seed.Pinned
		m.folderID = seed.FolderID
		if seed.Sensitive {
			m.body.Placeholder = "masked — ctrl+r reveals the body for editing"
		} else {
			m.body.SetValue(seed.Body)
			m.revealed = true
		}
	}

	m.applyChrome()
	m.focusStop(stopTitle)
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor, m.title.Focus()}
	if m.seed != nil && m.seed.FolderID != nil {
		ed, ctx, id := m.ed, m.ctx, *m.seed.FolderID
		cmds = append(cmds, func() tea.Msg {
			folders, err := ed.Folders(ctx)
			return foldersMsg{folders: folders, id: id, err: err}
		})
	}
	return tea.Batch(cmds...)
}

// Result is the saved row, once the write was accepted.
func (m Model) Result() (pick.Snippet, bool) {
	if m.saved == nil {
		return pick.Snippet{}, false
	}
	return *m.saved, true
}

type foldersMsg struct {
	folders []pick.Folder
	id      string
	err     error
}

type savedMsg struct {
	snip pick.Snippet
	err  error
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.applyChrome()
		return m, nil
	case tea.WindowSizeMsg:
		if msg.Width > 0 {
			m.width = msg.Width
		}
		if msg.Height > 0 {
			m.height = msg.Height
		}
		m.resize()
		return m, nil
	case foldersMsg:
		if msg.err != nil {
			// The folder is carried by ID regardless; only the label
			// is missing.
			m.folderName = msg.id
			return m, nil
		}
		m.folderName = folderLabel(msg.folders, msg.id)
		return m, nil
	case savedMsg:
		m.saving = false
		if msg.err != nil {
			// A failed write keeps the draft and shows the mapped error.
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.saved = &msg.snip
		m.quitting = true
		return m, tea.Quit
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()
	if k.Code == 'c' && k.Mod&tea.ModCtrl != 0 {
		// Ctrl+C always quits, like the picker; the dirty guard is
		// Esc-only.
		m.quitting = true
		m.saved = nil
		return m, tea.Quit
	}
	if m.saving {
		return m, nil
	}
	if m.asking {
		switch k.Code {
		case tea.KeyEsc, 'n', 'N':
			m.asking = false
			return m, nil
		case tea.KeyEnter, 'y', 'Y':
			m.quitting = true
			m.saved = nil
			return m, tea.Quit
		}
		return m, nil
	}
	if (k.Code == 's' && k.Mod&tea.ModCtrl != 0) ||
		(k.Code == tea.KeyEnter && k.Mod&tea.ModCtrl != 0) {
		return m.save()
	}
	if k.Code == 'r' && k.Mod&tea.ModCtrl != 0 && !m.revealed && m.seed != nil && m.seed.Sensitive {
		m.revealed = true
		m.body.SetValue(m.seed.Body)
		return m, nil
	}
	if k.Code == tea.KeyTab {
		d := 1
		if k.Mod&tea.ModShift != 0 {
			d = -1
		}
		next := (int(m.focus) + d + int(stopCount)) % int(stopCount)
		return m.focusStop(stop(next))
	}
	if k.Code == tea.KeyEsc {
		if !m.dirty() {
			m.quitting = true
			m.saved = nil
			return m, tea.Quit
		}
		m.asking = true
		return m, nil
	}
	if k.Code == tea.KeyEnter {
		switch m.focus {
		case stopTitle, stopLanguage, stopTags:
			// Enter in a single-line field advances, like Tab; the
			// textareas keep Enter for newlines.
			next := (int(m.focus) + 1) % int(stopCount)
			return m.focusStop(stop(next))
		}
	}
	switch m.focus {
	case stopSensitive, stopPinned:
		if k.Code == ' ' || k.Code == tea.KeyEnter {
			if m.focus == stopSensitive {
				m.sensitive = !m.sensitive
			} else {
				m.pinned = !m.pinned
			}
			return m, nil
		}
		return m, nil
	case stopTitle:
		var cmd tea.Cmd
		m.title, cmd = m.title.Update(msg)
		return m, cmd
	case stopLanguage:
		var cmd tea.Cmd
		m.language, cmd = m.language.Update(msg)
		return m, cmd
	case stopTags:
		var cmd tea.Cmd
		m.tags, cmd = m.tags.Update(msg)
		return m, cmd
	case stopBody:
		var cmd tea.Cmd
		m.body, cmd = m.body.Update(msg)
		return m, cmd
	case stopNotes:
		var cmd tea.Cmd
		m.notes, cmd = m.notes.Update(msg)
		return m, cmd
	}
	return m, nil
}

// input assembles the write payload with the GUI's save rules
// (SnippetForm.svelte submit): trims, tag splitting, the derived template
// flag, and var_defaults carried forward pruned to the variables the body
// still uses.
func (m Model) input() pick.Input {
	body := m.body.Value()
	if m.seed != nil && m.seed.Sensitive && !m.revealed {
		// Masked: carry the seed body; the empty box must not empty the
		// row.
		body = m.seed.Body
	}
	defaults := map[string]string{}
	if m.seed != nil {
		names := map[string]bool{}
		for _, v := range template.Extract(body) {
			names[v.Name] = true
		}
		for name, val := range m.seed.VarDefaults {
			if names[name] && val != "" {
				defaults[name] = val
			}
		}
	}
	var tags []string
	for _, t := range strings.Split(m.tags.Value(), ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return pick.Input{
		Title:         strings.TrimSpace(m.title.Value()),
		Body:          body,
		Language:      strings.TrimSpace(m.language.Value()),
		Notes:         strings.TrimSpace(m.notes.Value()),
		FolderID:      m.folderID,
		Tags:          tags,
		IsSensitive:   m.sensitive,
		UsesVariables: template.HasVars(body),
		Pinned:        m.pinned,
		VarDefaults:   defaults,
	}
}

// dirty reports whether the draft differs from the seed, mirroring the
// GUI's dirty check: a masked sensitive body is its seed body.
func (m Model) dirty() bool {
	in := m.input()
	if m.seed == nil {
		return in.Title != "" || in.Body != "" || in.Notes != "" ||
			in.Language != "" || len(in.Tags) > 0 || in.Pinned
	}
	s := m.seed
	return in.Title != s.Title || in.Body != s.Body || in.Notes != s.Notes ||
		in.Language != s.Language || m.folderID != s.FolderID ||
		in.IsSensitive != s.Sensitive || in.UsesVariables != s.UsesVariables ||
		in.Pinned != s.Pinned || !sameTags(in.Tags, s.Tags) ||
		!sameDefaults(in.VarDefaults, s.VarDefaults)
}

func (m Model) save() (tea.Model, tea.Cmd) {
	if m.saving {
		return m, nil
	}
	if strings.TrimSpace(m.title.Value()) == "" {
		m.err = "title required"
		return m, nil
	}
	in := m.input()
	ed, ctx := m.ed, m.ctx
	seed := m.seed
	m.saving = true
	m.err = ""
	return m, func() tea.Msg {
		if seed == nil {
			snip, err := ed.Create(ctx, in)
			return savedMsg{snip: snip, err: err}
		}
		snip, err := ed.Update(ctx, seed.ID, in)
		return savedMsg{snip: snip, err: err}
	}
}

func (m Model) focusStop(s stop) (tea.Model, tea.Cmd) {
	m.focus = s
	m.title.Blur()
	m.language.Blur()
	m.tags.Blur()
	m.body.Blur()
	m.notes.Blur()
	switch s {
	case stopTitle:
		return m, m.title.Focus()
	case stopLanguage:
		return m, m.language.Focus()
	case stopTags:
		return m, m.tags.Focus()
	case stopBody:
		return m, m.body.Focus()
	case stopNotes:
		return m, m.notes.Focus()
	}
	return m, nil
}

func (m *Model) applyChrome() {
	tui.StyleInput(&m.title, m.dark)
	tui.StyleInput(&m.language, m.dark)
	tui.StyleInput(&m.tags, m.dark)
	m.resize()
}

func (m *Model) resize() {
	w := m.contentWidth()
	m.title.SetWidth(w)
	m.language.SetWidth(w)
	m.tags.SetWidth(w)
	m.body.SetWidth(w)
	m.notes.SetWidth(w)
	bodyH := 6
	notesH := 3
	avail := m.height - 14
	if avail > 0 && avail < bodyH+notesH {
		if avail < 4 {
			bodyH, notesH = 2, 1
		} else {
			bodyH = avail * 2 / 3
			notesH = avail - bodyH
		}
	}
	m.body.SetHeight(bodyH)
	m.notes.SetHeight(notesH)
}

func (m Model) contentWidth() int {
	// frame padding (2) + prompt column (11)
	w := m.width - 13
	if w < 12 {
		w = 12
	}
	return w
}

// folderLabel builds a parent/child path from the flat folder list. The
// store forbids cycles; the visited set is cheap insurance anyway.
func folderLabel(folders []pick.Folder, id string) string {
	byID := make(map[string]pick.Folder, len(folders))
	for _, f := range folders {
		byID[f.ID] = f
	}
	var parts []string
	seen := map[string]bool{}
	for {
		f, ok := byID[id]
		if !ok || seen[id] {
			break
		}
		seen[id] = true
		parts = append([]string{f.Name}, parts...)
		if f.ParentID == nil {
			break
		}
		id = *f.ParentID
	}
	return strings.Join(parts, "/")
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := map[string]int{}
	for _, t := range a {
		counts[t]++
	}
	for _, t := range b {
		counts[t]--
		if counts[t] < 0 {
			return false
		}
	}
	return true
}

func sameDefaults(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || v != w {
			return false
		}
	}
	return true
}

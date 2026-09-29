package edit

import (
	"context"
	"sort"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/ask"
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
	stopFolder
	stopTags
	stopSensitive
	stopPinned
	stopCount
)

// languages mirrors the user-facing spellings the read view can highlight
// (web/src/lib/highlight.ts ALIASES keys). Suggestions only — free text is
// still accepted — so drift is cosmetic, but keep the two in step.
var languages = []string{
	"bash", "c", "c++", "cpp", "css", "diff", "dockerfile", "go", "golang",
	"html", "ini", "java", "javascript", "js", "json", "make", "makefile",
	"markdown", "md", "nginx", "node", "php", "py", "python", "rb", "rs",
	"ruby", "rust", "sh", "shell", "sql", "toml", "ts", "typescript",
	"xml", "yaml", "yml", "zsh",
}

// folderChoice is one row of the folder picker: nil id is Unfiled.
type folderChoice struct {
	id   *string
	path string
}

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

	// revealed is set for non-sensitive seeds: the body box is live.
	// A sensitive body stays masked until revealed (Ctrl+R); while
	// masked the box is empty and saves carry the seed body verbatim.
	revealed bool

	// folderID is the current selection, carried into every write;
	// folderName is its display label until the picker list loads.
	folderID   *string
	folderName string

	// folder picker state. choices[0] is always Unfiled (nil id); the
	// paths are parent/child labels sorted for a stable list.
	folderChoices []folderChoice
	folderCursor  int

	// tagNames backs the tag suggestions; suggActive is the highlighted
	// suggestion for the focused suggestible field (-1 = none). The
	// focused field decides which list it indexes; it resets on text
	// change and on focus change.
	tagNames   []string
	suggActive int

	// askMode is the Ask-AI box (Ctrl+A): a sub-view over the panel
	// that generates into the draft. It is only reachable when the AI
	// status reports the feature configured and enabled.
	ai       ask.Service // nil = no AI; controls hidden
	aiOn     bool
	askMode  bool
	askPrompt textinput.Model
	askKind  ai.Kind

	// Busy gating is the web form's aiPending: any AI action in flight
	// blocks saves and new AI actions. aiDone is the transient
	// "Generated — review and save." line.
	aiBusy      bool
	tagsBusy    bool
	explainBusy bool
	aiDone      string

	// explainUndo snapshots Notes before an Explain replaces it, the
	// web form's rule: taken only on success, dropped by hand-edits,
	// restored once by Ctrl+Z.
	explainUndo *string

	err     string
	asking   bool // dirty-quit confirm
	saving   bool // a write is in flight
	saved    *pick.Snippet
	quitting bool
}

// Prefill carries the `snp add` flags — or an Ask-AI generation — into
// a create panel. Empty strings prefill nothing; the body is the one
// field set unconditionally.
type Prefill struct {
	Title    string
	Language string
	Body     string
	Notes    string
	FolderID *string
	Tags     []string
	Sensitive bool
	Pinned    bool
}

// NewCreate builds the panel for `snp add`, prefilled from flags.
func NewCreate(ctx context.Context, ed pick.Editor, prefill Prefill) Model {
	m := New(ctx, ed, nil)
	m.title.SetValue(prefill.Title)
	m.language.SetValue(prefill.Language)
	m.body.SetValue(prefill.Body)
	m.notes.SetValue(prefill.Notes)
	if len(prefill.Tags) > 0 {
		m.tags.SetValue(strings.Join(prefill.Tags, ", "))
	}
	m.sensitive = prefill.Sensitive
	m.pinned = prefill.Pinned
	m.folderID = prefill.FolderID
	return m
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

	askPrompt := textinput.New()
	askPrompt.Prompt = "> "
	askPrompt.Placeholder = "what should the snippet do?"

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
		askPrompt: askPrompt,
		askKind:  ai.KindCommand,
		// no suggestion is highlighted until Down picks one
		suggActive: -1,
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
	ed, ctx := m.ed, m.ctx
	cmds := []tea.Cmd{tea.RequestBackgroundColor, m.title.Focus()}
	cmds = append(cmds,
		func() tea.Msg {
			folders, err := ed.Folders(ctx)
			return foldersMsg{folders: folders, err: err}
		},
		func() tea.Msg {
			tags, err := ed.Tags(ctx)
			return tagsMsg{tags: tags, err: err}
		},
	)
	if m.ai != nil {
		svc := m.ai
		cmds = append(cmds, func() tea.Msg {
			st, err := svc.Status(ctx)
			return aiStatusMsg{status: st, err: err}
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
	err     error
}

type tagsMsg struct {
	tags []pick.TagCount
	err  error
}

type aiStatusMsg struct {
	status ask.Status
	err    error
}

type aiGeneratedMsg struct {
	gen ask.Generation
	err error
}

type aiTagsMsg struct {
	tags []string
	err  error
}

type aiExplainedMsg struct {
	notes string
	err   error
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
			return m, nil
		}
		m.setFolders(msg.folders)
		return m, nil
	case tagsMsg:
		if msg.err != nil {
			// Suggestions are optional decoration; a failed load must
			// not break the editor.
			return m, nil
		}
		m.tagNames = make([]string, len(msg.tags))
		for i, t := range msg.tags {
			m.tagNames[i] = t.Name
		}
		return m, nil
	case aiStatusMsg:
		// A failed status check hides the controls, the web form's
		// aiKnown/aiEnabled outcome.
		m.aiOn = msg.err == nil && msg.status.Enabled
		return m, nil
	case aiGeneratedMsg:
		return m.onGenerated(msg)
	case aiTagsMsg:
		m.tagsBusy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.mergeTags(msg.tags)
		return m, nil
	case aiExplainedMsg:
		m.explainBusy = false
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		if strings.TrimSpace(msg.notes) == "" {
			// The web form's empty-explanation error: nothing replaced,
			// no snapshot taken.
			m.err = "AI returned an empty explanation"
			return m, nil
		}
		m.err = ""
		snapshot := m.notes.Value()
		m.explainUndo = &snapshot
		m.notes.SetValue(msg.notes)
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
	if m.askMode {
		return m.onAskKey(msg)
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
	// The AI controls (Ctrl+A ask, Ctrl+T suggest tags, Ctrl+E explain,
	// Ctrl+Z undo explain) are panel-level keys; each no-ops silently
	// when its web-form button would be disabled — unconfigured, busy,
	// sensitive (for the two that send the body), or an empty body.
	if k.Code == 'a' && k.Mod&tea.ModCtrl != 0 {
		return m.openAsk()
	}
	if k.Code == 't' && k.Mod&tea.ModCtrl != 0 {
		return m.suggestTagsCmd()
	}
	if k.Code == 'e' && k.Mod&tea.ModCtrl != 0 {
		return m.explainCmd()
	}
	if k.Code == 'z' && k.Mod&tea.ModCtrl != 0 && m.explainUndo != nil {
		m.notes.SetValue(*m.explainUndo)
		m.explainUndo = nil
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
		// A highlighted suggestion clears first, like the picker's
		// "esc clears, then cancels".
		if m.suggActive >= 0 {
			m.suggActive = -1
			return m, nil
		}
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
		case stopTitle:
			next := (int(m.focus) + 1) % int(stopCount)
			return m.focusStop(stop(next))
		case stopLanguage, stopTags:
			// Enter accepts a highlighted suggestion; without one it
			// advances like the other single-line fields.
			if m.suggActive >= 0 {
				m.acceptSuggestion()
				return m, nil
			}
			next := (int(m.focus) + 1) % int(stopCount)
			return m.focusStop(stop(next))
		}
	}
	switch m.focus {
	case stopFolder:
		switch k.Code {
		case tea.KeyUp:
			m.folderCursor = (m.folderCursor - 1 + len(m.folderChoices)) % len(m.folderChoices)
		case tea.KeyDown:
			m.folderCursor = (m.folderCursor + 1) % len(m.folderChoices)
		case tea.KeyEnter:
			m.folderID = m.folderChoices[m.folderCursor].id
			m.folderName = m.folderChoices[m.folderCursor].path
			next := (int(m.focus) + 1) % int(stopCount)
			return m.focusStop(stop(next))
		}
		return m, nil
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
		return m.updateSuggestible(&m.language, msg)
	case stopTags:
		return m.updateSuggestible(&m.tags, msg)
	case stopBody:
		var cmd tea.Cmd
		m.body, cmd = m.body.Update(msg)
		return m, cmd
	case stopNotes:
		prev := m.notes.Value()
		var cmd tea.Cmd
		m.notes, cmd = m.notes.Update(msg)
		// Hand-editing Notes drops the Explain undo snapshot (the web
		// form's oninput rule), so Undo can never discard something
		// written after the overwrite.
		if m.notes.Value() != prev {
			m.explainUndo = nil
		}
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

// suggestions lists the prefix matches for the focused suggestible field:
// the current tag word (text after the last comma) against the library's
// tags, or the whole language field against the known list. An exact
// match of the word itself is not suggested; five at most.
func (m Model) suggestions() []string {
	var word string
	var pool []string
	switch m.focus {
	case stopTags:
		parts := strings.Split(m.tags.Value(), ",")
		word = strings.TrimSpace(parts[len(parts)-1])
		pool = m.tagNames
	case stopLanguage:
		word = strings.TrimSpace(m.language.Value())
		pool = languages
	default:
		return nil
	}
	if word == "" {
		return nil
	}
	lower := strings.ToLower(word)
	var out []string
	for _, name := range pool {
		if name != word && strings.HasPrefix(strings.ToLower(name), lower) {
			out = append(out, name)
			if len(out) == 5 {
				break
			}
		}
	}
	return out
}

// acceptSuggestion replaces the focused field's word with the highlighted
// suggestion and clears the highlight.
func (m *Model) acceptSuggestion() {
	suggs := m.suggestions()
	if m.suggActive < 0 || m.suggActive >= len(suggs) {
		return
	}
	picked := suggs[m.suggActive]
	switch m.focus {
	case stopTags:
		parts := strings.Split(m.tags.Value(), ",")
		parts[len(parts)-1] = picked
		m.tags.SetValue(strings.Join(parts, ","))
	case stopLanguage:
		m.language.SetValue(picked)
	}
	m.suggActive = -1
}

// dirty reports whether the draft differs from the seed, mirroring the
// GUI's dirty check: a masked sensitive body is its seed body.
func (m Model) dirty() bool {
	in := m.input()
	if m.seed == nil {
		return in.Title != "" || in.Body != "" || in.Notes != "" ||
			in.Language != "" || len(in.Tags) > 0 || in.Pinned ||
			in.IsSensitive || in.FolderID != nil
	}
	s := m.seed
	// var_defaults is deliberately not compared: the panel has no UI for
	// it and pruning happens at save (the GUI's dirty check skips it
	// too), so a seed carrying defaults its body no longer uses must
	// still open as clean.
	return in.Title != s.Title || in.Body != s.Body || in.Notes != s.Notes ||
		in.Language != s.Language || !sameFolderID(m.folderID, s.FolderID) ||
		in.IsSensitive != s.Sensitive || in.UsesVariables != s.UsesVariables ||
		in.Pinned != s.Pinned || !sameTags(in.Tags, s.Tags)
}

func (m Model) save() (tea.Model, tea.Cmd) {
	if m.saving || m.aiBusy || m.tagsBusy || m.explainBusy {
		// aiPending parity: the web form disables Save while an AI
		// action is in flight.
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

// updateSuggestible routes a key for the language or tags field: Up/Down
// cycle the suggestion highlight while matches exist; any text change
// drops the highlight; everything else goes to the input.
func (m *Model) updateSuggestible(in *textinput.Model, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()
	if k.Code == tea.KeyUp || k.Code == tea.KeyDown {
		n := len(m.suggestions())
		if n > 0 {
			if k.Code == tea.KeyDown {
				m.suggActive++
			} else {
				m.suggActive--
			}
			if m.suggActive < 0 {
				m.suggActive = n - 1
			}
			if m.suggActive >= n {
				m.suggActive = 0
			}
			return *m, nil
		}
	}
	prev := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if in.Value() != prev {
		m.suggActive = -1
	}
	return *m, cmd
}

func (m Model) focusStop(s stop) (tea.Model, tea.Cmd) {
	m.focus = s
	m.suggActive = -1
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
	tui.StyleInput(&m.askPrompt, m.dark)
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

// setFolders builds the picker list — Unfiled first, then every live
// folder as a parent/child path, sorted — and points the cursor at the
// current selection.
func (m *Model) setFolders(folders []pick.Folder) {
	choices := make([]folderChoice, 0, len(folders)+1)
	choices = append(choices, folderChoice{path: "(none)"})
	rest := make([]folderChoice, 0, len(folders))
	for _, f := range folders {
		fid := f.ID
		rest = append(rest, folderChoice{id: &fid, path: FolderPath(folders, f.ID)})
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i].path < rest[j].path })
	choices = append(choices, rest...)
	m.folderChoices = choices
	m.folderCursor = 0
	if m.folderID != nil {
		for i, c := range choices {
			if c.id != nil && *c.id == *m.folderID {
				m.folderCursor = i
				break
			}
		}
	}
	m.folderName = choices[m.folderCursor].path
}

// FolderPath builds a parent/child path from the flat folder list. The
// store forbids cycles; the visited set is cheap insurance anyway.
func FolderPath(folders []pick.Folder, id string) string {
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

// aiAvailable reports whether the AI controls may be offered: a
// service is wired and its status reported the feature enabled.
func (m Model) aiAvailable() bool {
	return m.ai != nil && m.aiOn && !m.aiBusy && !m.tagsBusy && !m.explainBusy
}

// aiBody is the body the body-sending actions would use. They are
// never offered for a sensitive draft, so the masked box is not a
// concern here.
func (m Model) aiBody() string {
	return m.body.Value()
}

// openAsk opens the Ask-AI box (Ctrl+A). Ask-AI sends only the typed
// prompt, so — unlike suggest-tags and explain — it is available even
// on a sensitive draft, exactly like the web form's button.
func (m Model) openAsk() (tea.Model, tea.Cmd) {
	if !m.aiAvailable() {
		return m, nil
	}
	m.askMode = true
	m.err = ""
	return m, m.askPrompt.Focus()
}

// onAskKey drives the box: Tab cycles the kind, Enter generates, Esc
// returns without touching the draft.
func (m Model) onAskKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()
	switch k.Code {
	case tea.KeyEsc:
		m.askMode = false
		m.err = ""
		return m, nil
	case tea.KeyTab:
		switch m.askKind {
		case ai.KindCommand:
			m.askKind = ai.KindScript
		case ai.KindScript:
			m.askKind = ai.KindFunction
		default:
			m.askKind = ai.KindCommand
		}
		return m, nil
	case tea.KeyEnter:
		prompt := strings.TrimSpace(m.askPrompt.Value())
		if prompt == "" || m.aiBusy {
			return m, nil
		}
		m.aiBusy = true
		m.err = ""
		svc, ctx, kind := m.ai, m.ctx, m.askKind
		// The web form sends the draft's language when set.
		lang := strings.TrimSpace(m.language.Value())
		return m, func() tea.Msg {
			gen, err := svc.Generate(ctx, prompt, kind, lang)
			return aiGeneratedMsg{gen: gen, err: err}
		}
	}
	var cmd tea.Cmd
	m.askPrompt, cmd = m.askPrompt.Update(msg)
	return m, cmd
}

// onGenerated applies the web form's fill rules (askAI): body always;
// title, language, notes only when the model produced them; an empty
// body is the web client's empty-snippet error and leaves the draft
// (and the box) alone.
func (m Model) onGenerated(msg aiGeneratedMsg) (tea.Model, tea.Cmd) {
	m.aiBusy = false
	if msg.err != nil {
		m.err = msg.err.Error()
		return m, nil
	}
	if msg.gen.Body == "" {
		m.err = "AI returned an empty snippet"
		return m, nil
	}
	m.err = ""
	if msg.gen.Title != "" {
		m.title.SetValue(msg.gen.Title)
	}
	if msg.gen.Language != "" {
		m.language.SetValue(msg.gen.Language)
	}
	m.body.SetValue(msg.gen.Body)
	if msg.gen.Notes != "" {
		m.notes.SetValue(msg.gen.Notes)
	}
	// A generation into a masked sensitive draft is an explicit
	// replacement of a body the user has now seen: save must send the
	// generated body, not the masked seed body it would otherwise
	// carry.
	if m.seed != nil && m.seed.Sensitive && !m.revealed {
		m.revealed = true
	}
	m.askMode = false
	m.aiDone = "Generated — review and save."
	return m, nil
}

// suggestTagsCmd runs the tag suggestion (Ctrl+T): the web form's
// guards — not for a sensitive draft, needs a body — and the merge
// rule: lowercase, nothing duplicated.
func (m Model) suggestTagsCmd() (tea.Model, tea.Cmd) {
	if !m.aiAvailable() || m.sensitive || strings.TrimSpace(m.aiBody()) == "" {
		return m, nil
	}
	m.tagsBusy = true
	m.err = ""
	svc, ctx := m.ai, m.ctx
	body := m.aiBody()
	title := strings.TrimSpace(m.title.Value())
	language := strings.TrimSpace(m.language.Value())
	return m, func() tea.Msg {
		tags, err := svc.SuggestTags(ctx, ask.TagInput{
			Body: body, Title: title, Language: language,
		})
		return aiTagsMsg{tags: tags, err: err}
	}
}

// explainCmd runs the explanation (Ctrl+E): the same guards as
// suggest-tags; the result replaces Notes, snapshot kept (the web
// form's Explain).
func (m Model) explainCmd() (tea.Model, tea.Cmd) {
	if !m.aiAvailable() || m.sensitive || strings.TrimSpace(m.aiBody()) == "" {
		return m, nil
	}
	m.explainBusy = true
	m.err = ""
	svc, ctx := m.ai, m.ctx
	body := m.aiBody()
	return m, func() tea.Msg {
		notes, err := svc.Explain(ctx, body, false)
		return aiExplainedMsg{notes: notes, err: err}
	}
}

// mergeTags merges suggested names into the tags field without
// clobbering what is already typed — the web form's suggestTags merge.
func (m *Model) mergeTags(suggested []string) {
	current := []string{}
	for _, t := range strings.Split(m.tags.Value(), ",") {
		if t = strings.TrimSpace(strings.ToLower(t)); t != "" {
			current = append(current, t)
		}
	}
	for _, t := range suggested {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		dup := false
		for _, c := range current {
			if c == t {
				dup = true
				break
			}
		}
		if !dup {
			current = append(current, t)
		}
	}
	m.tags.SetValue(strings.Join(current, ", "))
}

func sameFolderID(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
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


package pick

import (
	"context"
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/template"
)

const searchDelay = 100 * time.Millisecond

type mode int

const (
	modeList mode = iota
	modeForm
)

// Model is the list screen and the variable form.
type Model struct {
	lib Library
	ctx context.Context

	width  int
	height int
	dark   bool

	filter  textinput.Model
	gen     int
	settled bool
	hits    []Snippet
	cursor  int
	err     string

	mode   mode
	form   Snippet
	vars   []template.Var
	fields []textinput.Model
	focus  int

	revealGen int

	result   *string
	quitting bool
}

func newModel(ctx context.Context, lib Library) Model {
	if ctx == nil {
		ctx = context.Background()
	}
	f := textinput.New()
	f.Prompt = "› "
	f.Placeholder = "search"
	f.Focus()
	m := Model{
		lib:    lib,
		ctx:    ctx,
		width:  80,
		height: 24,
		dark:   true,
		filter: f,
		gen:    1,
	}
	m.applyChrome()
	return m
}

func (m Model) Init() tea.Cmd {
	// The first search starts immediately. Later keystrokes are debounced
	// by arm, so the opening frame does not sit on an empty list.
	return tea.Batch(m.searchCmd(m.gen, ""), m.filter.Focus(), tea.RequestBackgroundColor)
}

func (m Model) Result() (string, bool) {
	if m.result == nil {
		return "", false
	}
	return *m.result, true
}

type arm struct{ gen int }

type searched struct {
	gen  int
	hits []Snippet
	err  error
}

type revealed struct {
	gen  int
	snip Snippet
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
	case tea.KeyPressMsg:
		return m.onKey(msg)
	case arm:
		if msg.gen != m.gen {
			return m, nil
		}
		return m, m.searchCmd(msg.gen, m.filter.Value())
	case searched:
		if msg.gen != m.gen {
			return m, nil
		}
		m.settled = true
		if msg.err != nil {
			m.hits = nil
			m.cursor = 0
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		m.hits = msg.hits
		if m.cursor >= len(m.hits) {
			m.cursor = 0
		}
		return m, nil
	case revealed:
		if msg.gen != m.revealGen || m.mode != modeList {
			return m, nil
		}
		if msg.err != nil {
			m.err = msg.err.Error()
			return m, nil
		}
		m.err = ""
		if msg.snip.IsTemplate() {
			return m.openForm(msg.snip)
		}
		return m.accept(msg.snip, msg.snip.Body)
	}
	return m, nil
}

func (m Model) onKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.Key()
	if k.Code == 'c' && k.Mod&tea.ModCtrl != 0 {
		return m.cancel()
	}
	if m.mode == modeForm {
		return m.onFormKey(k)
	}
	return m.onListKey(msg, k)
}

func (m Model) onListKey(msg tea.KeyPressMsg, k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyUp:
		m.move(-1)
		return m, nil
	case tea.KeyDown:
		m.move(1)
		return m, nil
	case tea.KeyEnter:
		return m.choose()
	case tea.KeyEsc:
		if m.filter.Value() != "" {
			m.filter.SetValue("")
			return m.rearm()
		}
		return m.cancel()
	}
	prev := m.filter.Value()
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if m.filter.Value() == prev {
		return m, cmd
	}
	next, armCmd := m.rearm()
	return next, tea.Batch(cmd, armCmd)
}

func (m Model) onFormKey(k tea.Key) (tea.Model, tea.Cmd) {
	switch k.Code {
	case tea.KeyEsc:
		m.mode = modeList
		m.err = ""
		return m, nil
	case tea.KeyTab:
		if len(m.fields) == 0 {
			return m, nil
		}
		m.fields[m.focus].Blur()
		if k.Mod&tea.ModShift != 0 {
			m.focus--
			if m.focus < 0 {
				m.focus = len(m.fields) - 1
			}
		} else {
			m.focus = (m.focus + 1) % len(m.fields)
		}
		return m, m.fields[m.focus].Focus()
	case tea.KeyEnter:
		return m.accept(m.form, m.rendered())
	}
	if len(m.fields) == 0 {
		return m, nil
	}
	var cmd tea.Cmd
	m.fields[m.focus], cmd = m.fields[m.focus].Update(tea.KeyPressMsg(k))
	return m, cmd
}

func (m Model) choose() (tea.Model, tea.Cmd) {
	if len(m.hits) == 0 || m.cursor >= len(m.hits) {
		return m, nil
	}
	s := m.hits[m.cursor]
	if s.Sensitive {
		return m.startReveal(s.ID)
	}
	if s.IsTemplate() {
		return m.openForm(s)
	}
	return m.accept(s, s.Body)
}

func (m Model) startReveal(id string) (tea.Model, tea.Cmd) {
	m.revealGen++
	gen := m.revealGen
	lib := m.lib
	ctx := m.ctx
	m.err = ""
	return m, func() tea.Msg {
		snip, err := lib.Reveal(ctx, id)
		return revealed{gen: gen, snip: snip, err: err}
	}
}

func (m Model) openForm(s Snippet) (tea.Model, tea.Cmd) {
	vars := template.Extract(s.Body)
	if len(vars) == 0 {
		return m.accept(s, s.Body)
	}
	labelW := 0
	for _, v := range vars {
		if len(v.Name) > labelW {
			labelW = len(v.Name)
		}
	}
	fields := make([]textinput.Model, len(vars))
	boxW := m.fieldWidth(labelW)
	for i, v := range vars {
		ti := textinput.New()
		ti.Prompt = fmt.Sprintf("%-*s  ", labelW, v.Name)
		ti.SetValue(template.Initial(v, s.VarDefaults))
		ti.SetWidth(boxW)
		styleInput(&ti, m.dark)
		if i > 0 {
			ti.Blur()
		}
		fields[i] = ti
	}
	m.mode = modeForm
	m.form = s
	m.vars = vars
	m.fields = fields
	m.focus = 0
	m.err = ""
	return m, m.fields[0].Focus()
}

func (m Model) rendered() string {
	vals := make(map[string]string, len(m.vars))
	for i, v := range m.vars {
		vals[v.Name] = m.fields[i].Value()
	}
	return template.Fill(m.form.Body, vals)
}

func (m Model) accept(s Snippet, body string) (tea.Model, tea.Cmd) {
	text := CommandText(s, body)
	m.result = &text
	m.quitting = true
	return m, tea.Quit
}

func (m Model) cancel() (tea.Model, tea.Cmd) {
	m.quitting = true
	m.result = nil
	return m, tea.Quit
}

func (m Model) rearm() (tea.Model, tea.Cmd) {
	m.gen++
	m.hits = nil
	m.cursor = 0
	m.err = ""
	m.settled = false
	return m, m.arm()
}

func (m Model) arm() tea.Cmd {
	gen := m.gen
	return tea.Tick(searchDelay, func(time.Time) tea.Msg {
		return arm{gen: gen}
	})
}

func (m Model) searchCmd(gen int, q string) tea.Cmd {
	lib := m.lib
	ctx := m.ctx
	return func() tea.Msg {
		hits, err := lib.Search(ctx, q)
		return searched{gen: gen, hits: hits, err: err}
	}
}

func (m *Model) move(d int) {
	n := len(m.hits)
	if n == 0 {
		return
	}
	m.cursor = (m.cursor + d + n) % n
}

func (m *Model) applyChrome() {
	styleInput(&m.filter, m.dark)
	m.resize()
}

func (m Model) fieldWidth(labelW int) int {
	boxW := m.contentWidth() - labelW - 2
	if boxW < 12 {
		boxW = 12
	}
	return boxW
}

func (m *Model) resize() {
	w := m.contentWidth() - 2
	if w < 12 {
		w = 12
	}
	m.filter.SetWidth(w)
	if len(m.fields) == 0 {
		return
	}
	labelW := len(m.vars[0].Name)
	for _, v := range m.vars {
		if len(v.Name) > labelW {
			labelW = len(v.Name)
		}
	}
	boxW := m.fieldWidth(labelW)
	for i := range m.fields {
		m.fields[i].SetWidth(boxW)
	}
}

func (m Model) View() tea.View {
	var b strings.Builder
	if m.mode == modeForm {
		m.writeForm(&b)
	} else {
		m.writeList(&b)
	}
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

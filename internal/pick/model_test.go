package pick

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

type memLib struct {
	hits    []Snippet
	full    map[string]Snippet
	err     error
	queries []string
	reveals int
}

func (m *memLib) Search(ctx context.Context, q string) ([]Snippet, error) {
	m.queries = append(m.queries, q)
	if m.err != nil {
		return nil, m.err
	}
	return m.hits, nil
}

func (m *memLib) Reveal(ctx context.Context, id string) (Snippet, error) {
	m.reveals++
	if m.err != nil {
		return Snippet{}, m.err
	}
	s, ok := m.full[id]
	if !ok {
		return Snippet{}, errors.New("missing")
	}
	return s, nil
}

func apply(m Model, msg tea.Msg) (Model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func press(code rune, text string, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: code, Text: text, Mod: mod}
}

func loaded(t *testing.T, lib Library) Model {
	t.Helper()
	m := newModel(context.Background(), lib)
	m, cmd := apply(m, arm{gen: m.gen})
	if cmd == nil {
		t.Fatal("search command missing")
	}
	m, _ = apply(m, cmd())
	return m
}

func TestListEnterInsertsBody(t *testing.T) {
	lib := &memLib{hits: []Snippet{
		{ID: "1", Title: "restart caddy", Body: "sudo systemctl restart caddy", Language: "bash", Notes: "after the Caddyfile", Tags: []string{"ops"}},
		{ID: "2", Title: "other", Body: "echo other"},
	}}
	m := loaded(t, lib)
	if lib.queries[0] != "" {
		t.Fatalf("query = %q", lib.queries[0])
	}
	view := m.View().Content
	if !strings.Contains(view, "restart caddy") || !strings.Contains(view, "sudo systemctl restart caddy") {
		t.Fatalf("view = %s", view)
	}
	m, _ = apply(m, press(tea.KeyDown, "", 0))
	m, _ = apply(m, press(tea.KeyDown, "", 0))
	if m.cursor != 0 {
		t.Fatalf("cursor wrapped to %d", m.cursor)
	}
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	got, ok := m.Result()
	if !ok || got != "sudo systemctl restart caddy" || !m.quitting {
		t.Fatalf("result %q ok=%v quit=%v", got, ok, m.quitting)
	}
}

func TestTemplateForm(t *testing.T) {
	lib := &memLib{hits: []Snippet{{
		ID: "1", Title: "deploy", Body: "ssh {{user|root}}@{{host|gx10}}",
		Language: "bash", UsesVariables: true,
		VarDefaults: map[string]string{"host": "nas"},
	}}}
	m := loaded(t, lib)
	if !strings.Contains(m.View().Content, "template") {
		t.Fatalf("view = %s", m.View().Content)
	}
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	if m.mode != modeForm || m.focus != 0 {
		t.Fatalf("mode %v focus %d", m.mode, m.focus)
	}
	if m.fields[0].Value() != "root" || m.fields[1].Value() != "nas" {
		t.Fatalf("boxes = %q %q", m.fields[0].Value(), m.fields[1].Value())
	}
	m, _ = apply(m, press(tea.KeyTab, "", 0))
	if m.focus != 1 {
		t.Fatalf("focus = %d", m.focus)
	}
	m, _ = apply(m, press(tea.KeyTab, "", tea.ModShift))
	if m.focus != 0 {
		t.Fatalf("focus after shift-tab = %d", m.focus)
	}
	m.fields[0].SetValue("")
	if !strings.Contains(m.rendered(), "ssh @nas") {
		t.Fatalf("preview = %q", m.rendered())
	}
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	got, ok := m.Result()
	if !ok || got != "ssh @nas" {
		t.Fatalf("result = %q", got)
	}
}

func TestFormEscReturnsToList(t *testing.T) {
	lib := &memLib{hits: []Snippet{{ID: "1", Title: "t", Body: "echo {{name}}"}}}
	m := loaded(t, lib)
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	if m.mode != modeList || m.result != nil {
		t.Fatalf("mode %v result %v", m.mode, m.result)
	}
}

func TestEscClearsThenCancels(t *testing.T) {
	lib := &memLib{hits: []Snippet{{ID: "1", Title: "t", Body: "echo"}}}
	m := loaded(t, lib)
	m, cmd := apply(m, press('x', "x", 0))
	if m.filter.Value() != "x" || cmd == nil {
		t.Fatalf("filter %q cmd %v", m.filter.Value(), cmd)
	}
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	if m.filter.Value() != "" || m.quitting {
		t.Fatalf("filter %q quit %v", m.filter.Value(), m.quitting)
	}
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	if !m.quitting || m.result != nil {
		t.Fatalf("quit %v result %v", m.quitting, m.result)
	}
}

func TestSensitiveReveal(t *testing.T) {
	lib := &memLib{
		hits: []Snippet{{ID: "s", Title: "token", Sensitive: true, Notes: "careful"}},
		full: map[string]Snippet{"s": {ID: "s", Title: "token", Body: "hunter2", Sensitive: true}},
	}
	m := loaded(t, lib)
	if strings.Contains(m.View().Content, "hunter2") {
		t.Fatal("list showed the secret")
	}
	m, cmd := apply(m, press(tea.KeyEnter, "", 0))
	if cmd == nil {
		t.Fatal("no reveal")
	}
	m, _ = apply(m, cmd())
	got, ok := m.Result()
	if !ok || got != " hunter2" {
		t.Fatalf("result = %q", got)
	}
}

func TestSensitiveTemplateForm(t *testing.T) {
	lib := &memLib{
		hits: []Snippet{{ID: "s", Title: "login", Sensitive: true, UsesVariables: true}},
		full: map[string]Snippet{"s": {
			ID: "s", Title: "login", Body: "echo {{user|root}}", Sensitive: true, UsesVariables: true,
		}},
	}
	m := loaded(t, lib)
	m, cmd := apply(m, press(tea.KeyEnter, "", 0))
	m, _ = apply(m, cmd())
	if m.mode != modeForm {
		t.Fatalf("mode %v", m.mode)
	}
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	got, ok := m.Result()
	if !ok || got != " echo root" {
		t.Fatalf("result = %q", got)
	}
}

func TestStaleSearchIgnored(t *testing.T) {
	lib := &memLib{hits: []Snippet{{ID: "1", Title: "new", Body: "new"}}}
	m := loaded(t, lib)
	m, _ = apply(m, searched{gen: m.gen - 1, hits: []Snippet{{ID: "old", Title: "old", Body: "old"}}})
	if m.hits[0].ID != "1" {
		t.Fatalf("hits = %+v", m.hits)
	}
}

func TestSearchErrorStaysOnScreen(t *testing.T) {
	lib := &memLib{err: errors.New("library: offline")}
	m := loaded(t, lib)
	if m.err != "library: offline" || len(m.hits) != 0 {
		t.Fatalf("err %q hits %d", m.err, len(m.hits))
	}
	if !strings.Contains(m.View().Content, "library: offline") {
		t.Fatalf("view = %s", m.View().Content)
	}
}

func TestChooseSelectOnly(t *testing.T) {
	lib := &memLib{hits: []Snippet{
		{ID: "s", Title: "token", Sensitive: true, Notes: "careful"},
		{ID: "2", Title: "plain", Body: "echo hi"},
	}}
	m := loaded(t, lib)
	m.selectOnly = true
	// A sensitive row needs no reveal in the chooser: Enter returns it
	// as-is — the editor loads the body itself.
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	got, ok := m.Chosen()
	if !ok || got.ID != "s" || !got.Sensitive || !m.quitting {
		t.Fatalf("chosen = %+v ok=%v quit=%v", got, ok, m.quitting)
	}
	if lib.reveals != 0 {
		t.Fatalf("reveal called %d times in select mode", lib.reveals)
	}
	if m.result != nil {
		t.Fatal("select mode wrote a command result")
	}
	if !strings.Contains(m.View().Content, "enter edit") {
		t.Fatalf("hint: %s", m.View().Content)
	}
}

func TestChooseSelectOnlyMovesFirst(t *testing.T) {
	lib := &memLib{hits: []Snippet{
		{ID: "1", Title: "a", Body: "x"},
		{ID: "2", Title: "b", Body: "y"},
	}}
	m := loaded(t, lib)
	m.selectOnly = true
	m, _ = apply(m, press(tea.KeyDown, "", 0))
	m, _ = apply(m, press(tea.KeyEnter, "", 0))
	got, ok := m.Chosen()
	if !ok || got.ID != "2" {
		t.Fatalf("chosen = %+v ok=%v", got, ok)
	}
}

func TestWidgetMatchesDeploy(t *testing.T) {
	deployed, err := os.ReadFile("../../deploy/snp.zsh")
	if err != nil {
		t.Fatal(err)
	}
	if string(deployed) != Widget {
		t.Fatal("deploy/snp.zsh drifted from the embedded widget")
	}
}

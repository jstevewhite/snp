package edit

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/ask"
	"github.com/jstevewhite/snp/internal/pick"
)

// fakeAsk is the Service fake: fixed replies, recorded calls.
type fakeAsk struct {
	status ask.Status
	gen    ask.Generation
	genErr error
	tags   []string
	tagErr error
	notes  string
	expErr error

	genCalls     []genCall
	tagsCalls    []ask.TagInput
	explainCalls []string
}

type genCall struct {
	prompt, language string
	kind             ai.Kind
}

func (f *fakeAsk) Status(ctx context.Context) (ask.Status, error) {
	return f.status, nil
}

func (f *fakeAsk) Generate(ctx context.Context, prompt string, kind ai.Kind, language string) (ask.Generation, error) {
	f.genCalls = append(f.genCalls, genCall{prompt: prompt, language: language, kind: kind})
	if f.genErr != nil {
		return ask.Generation{}, f.genErr
	}
	return f.gen, nil
}

func (f *fakeAsk) SuggestTags(ctx context.Context, in ask.TagInput) ([]string, error) {
	f.tagsCalls = append(f.tagsCalls, in)
	if f.tagErr != nil {
		return nil, f.tagErr
	}
	return f.tags, nil
}

func (f *fakeAsk) Explain(ctx context.Context, body string, sensitive bool) (string, error) {
	f.explainCalls = append(f.explainCalls, body)
	if f.expErr != nil {
		return "", f.expErr
	}
	return f.notes, nil
}

// aiPanel builds a panel with the AI fake attached and the status
// already resolved (the Init drain would do it; setting directly keeps
// each test's setup obvious).
func aiPanel(t *testing.T, ed pick.Editor, seed *pick.Snippet, fa *fakeAsk) Model {
	t.Helper()
	m := start(t, ed, seed)
	m.ai = fa
	m.aiOn = fa.status.Enabled
	return m
}

func ctrl(key rune) tea.KeyPressMsg { return press(key, string(key), tea.ModCtrl) }

func enabledAsk() *fakeAsk {
	return &fakeAsk{status: ask.Status{Enabled: true, Model: "m"}}
}

func TestAIStatusHiddenWhenDisabled(t *testing.T) {
	m := New(context.Background(), &fakeEditor{}, nil)
	fa := &fakeAsk{status: ask.Status{Enabled: false}}
	m.ai = fa
	m, cmd := apply(m, m.Init())
	m = drain(t, m, cmd)
	if m.aiOn {
		t.Fatal("disabled status enabled the controls")
	}
	// Every AI key no-ops: the box never opens, no calls are made.
	m, _ = apply(m, ctrl('a'))
	if m.askMode {
		t.Fatal("ask box opened while disabled")
	}
	m, _ = apply(m, ctrl('t'))
	m, _ = apply(m, ctrl('e'))
	if len(fa.genCalls)+len(fa.tagsCalls)+len(fa.explainCalls) != 0 {
		t.Fatal("service called while disabled")
	}
	// The help line offers nothing.
	if strings.Contains(m.View().Content, "ctrl+a") {
		t.Fatalf("help offers AI while disabled: %s", m.View().Content)
	}
}

func TestAskAIFillRules(t *testing.T) {
	fa := enabledAsk()
	fa.gen = ask.Generation{Title: "new title", Language: "bash", Body: "echo hi", Notes: "a note"}
	m := aiPanel(t, &fakeEditor{}, nil, fa)
	m.title.SetValue("old title")
	m, _ = apply(m, ctrl('a'))
	if !m.askMode || !strings.Contains(m.View().Content, "ask ai") {
		t.Fatalf("box = %v view = %s", m.askMode, m.View().Content)
	}
	for _, r := range "a prompt" {
		m, _ = apply(m, press(r, string(r), 0))
	}
	m, _ = apply(m, press(tea.KeyTab, "", 0)) // command → script
	m, _ = apply(m, press(tea.KeyTab, "", 0)) // script → function
	m, cmd := apply(m, enter())
	if cmd == nil {
		t.Fatal("no generate cmd")
	}
	m, _ = apply(m, cmd())
	if m.askMode || m.aiBusy {
		t.Fatalf("mode = %v busy = %v", m.askMode, m.aiBusy)
	}
	if m.title.Value() != "new title" || m.language.Value() != "bash" ||
		m.body.Value() != "echo hi" || m.notes.Value() != "a note" {
		t.Fatalf("fill: %q %q %q %q", m.title.Value(), m.language.Value(), m.body.Value(), m.notes.Value())
	}
	if m.aiDone != "Generated — review and save." {
		t.Fatalf("done = %q", m.aiDone)
	}
	// The kind cycled to function and the draft language rode along.
	if fa.genCalls[0].kind != ai.KindFunction || fa.genCalls[0].prompt != "a prompt" {
		t.Fatalf("call = %+v", fa.genCalls[0])
	}
	// Esc before Enter leaves the draft untouched.
	m2 := aiPanel(t, &fakeEditor{}, nil, enabledAsk())
	m2.title.SetValue("keep")
	m2, _ = apply(m2, ctrl('a'))
	m2, _ = apply(m2, press(tea.KeyEsc, "", 0))
	if m2.askMode || m2.title.Value() != "keep" {
		t.Fatalf("esc: mode=%v title=%q", m2.askMode, m2.title.Value())
	}
}

func TestAskAIFillOmitsEmptyFields(t *testing.T) {
	// The web rules: title/language/notes only when the model produced
	// them; body always.
	fa := enabledAsk()
	fa.gen = ask.Generation{Title: "", Language: "", Body: "echo hi", Notes: ""}
	m := aiPanel(t, &fakeEditor{}, nil, fa)
	m.title.SetValue("kept")
	m.language.SetValue("bash")
	m.notes.SetValue("kept notes")
	m, _ = apply(m, ctrl('a'))
	for _, r := range "p" {
		m, _ = apply(m, press(r, string(r), 0))
	}
	m, cmd := apply(m, enter())
	m, _ = apply(m, cmd())
	if m.title.Value() != "kept" || m.language.Value() != "bash" ||
		m.notes.Value() != "kept notes" || m.body.Value() != "echo hi" {
		t.Fatalf("fill = %q %q %q %q", m.title.Value(), m.language.Value(), m.notes.Value(), m.body.Value())
	}
}

func TestAskAIEmptyGenerationKeepsDraft(t *testing.T) {
	fa := enabledAsk()
	fa.gen = ask.Generation{Title: "t"}
	m := aiPanel(t, &fakeEditor{}, nil, fa)
	m, _ = apply(m, ctrl('a'))
	for _, r := range "p" {
		m, _ = apply(m, press(r, string(r), 0))
	}
	m, cmd := apply(m, enter())
	m, _ = apply(m, cmd())
	if !m.askMode || m.err != "AI returned an empty snippet" || m.body.Value() != "" {
		t.Fatalf("empty gen: mode=%v err=%q body=%q", m.askMode, m.err, m.body.Value())
	}
	// The box is still up: Esc backs out cleanly.
	m, _ = apply(m, press(tea.KeyEsc, "", 0))
	if m.askMode {
		t.Fatal("esc did not close the box")
	}
}

func TestAskAIRevealsMaskedSensitiveDraft(t *testing.T) {
	seed := seedSnippet()
	seed.Sensitive = true
	seed.Body = "hunter2"
	fa := enabledAsk()
	fa.gen = ask.Generation{Title: "gen", Body: "new body"}
	m := aiPanel(t, &fakeEditor{snippet: *seed}, seed, fa)
	if m.revealed {
		t.Fatal("seed not masked")
	}
	m, _ = apply(m, ctrl('a'))
	for _, r := range "p" {
		m, _ = apply(m, press(r, string(r), 0))
	}
	m, cmd := apply(m, enter())
	m, _ = apply(m, cmd())
	if !m.revealed || m.body.Value() != "new body" {
		t.Fatalf("reveal: revealed=%v body=%q", m.revealed, m.body.Value())
	}
	// A save sends the generated body, not the masked seed body.
	m, cmd = apply(m, ctrlS())
	m, _ = apply(m, cmd())
	if in := m.ed.(*fakeEditor).updates[0].in; in.Body != "new body" {
		t.Fatalf("saved body = %q", in.Body)
	}
}

func TestSuggestTagsMergeAndGuards(t *testing.T) {
	fa := enabledAsk()
	fa.tags = []string{"bash", "ops", "net"}
	m := aiPanel(t, &fakeEditor{}, nil, fa)
	m.body.SetValue("echo hi")
	m.tags.SetValue("ops, WEB")
	m, cmd := apply(m, ctrl('t'))
	if cmd == nil {
		t.Fatal("no suggest cmd")
	}
	m, _ = apply(m, cmd())
	if m.tags.Value() != "ops, web, bash, net" {
		t.Fatalf("tags = %q", m.tags.Value())
	}
	// The suggestion carried the draft's title/language context.
	if fa.tagsCalls[0].Body != "echo hi" || fa.tagsCalls[0].Sensitive {
		t.Fatalf("call = %+v", fa.tagsCalls[0])
	}
	// Sensitive drafts never offer it, and an empty body never calls.
	seed := seedSnippet()
	seed.Sensitive = true
	fa2 := enabledAsk()
	m2 := aiPanel(t, &fakeEditor{snippet: *seed}, seed, fa2)
	m2.body.SetValue("x")
	m2, _ = apply(m2, ctrl('t'))
	if len(fa2.tagsCalls) != 0 || m2.tagsBusy {
		t.Fatal("sensitive draft suggested tags")
	}
	// The help line offers only the prompt-only control, not the
	// body-sending ones.
	if strings.Contains(m2.View().Content, "ctrl+t") || strings.Contains(m2.View().Content, "ctrl+e") {
		t.Fatalf("help offers body-sending AI for a sensitive draft: %s", m2.View().Content)
	}
	m3 := aiPanel(t, &fakeEditor{}, nil, fa2)
	m3, _ = apply(m3, ctrl('t'))
	if len(fa2.tagsCalls) != 0 {
		t.Fatal("empty body called suggest")
	}
}

func TestExplainUndoAndInvalidation(t *testing.T) {
	fa := enabledAsk()
	fa.notes = "the explanation"
	m := aiPanel(t, &fakeEditor{}, nil, fa)
	m.body.SetValue("echo hi")
	m.notes.SetValue("hand notes")
	m, cmd := apply(m, ctrl('e'))
	if cmd == nil {
		t.Fatal("no explain cmd")
	}
	m, _ = apply(m, cmd())
	if m.notes.Value() != "the explanation" || m.explainUndo == nil || *m.explainUndo != "hand notes" {
		t.Fatalf("explain: notes=%q undo=%v", m.notes.Value(), m.explainUndo)
	}
	// Ctrl+Z puts the snapshot back, once.
	m, _ = apply(m, ctrl('z'))
	if m.notes.Value() != "hand notes" || m.explainUndo != nil {
		t.Fatalf("undo: notes=%q undo=%v", m.notes.Value(), m.explainUndo)
	}
	// Hand-editing Notes drops the snapshot: re-explain, then type.
	m.body.SetValue("echo hi")
	m, cmd = apply(m, ctrl('e'))
	m, _ = apply(m, cmd())
	if m.explainUndo == nil {
		t.Fatal("no second snapshot")
	}
	for m.focus != stopNotes {
		m, _ = apply(m, press(tea.KeyTab, "", 0))
	}
	m, _ = apply(m, press('x', "x", 0))
	if m.explainUndo != nil {
		t.Fatal("hand edit kept the snapshot")
	}
	// An explain error replaces nothing and takes no snapshot.
	fa3 := enabledAsk()
	fa3.expErr = errors.New("AI provider error")
	m3 := aiPanel(t, &fakeEditor{}, nil, fa3)
	m3.body.SetValue("echo hi")
	m3.notes.SetValue("kept")
	m3, cmd = apply(m3, ctrl('e'))
	m3, _ = apply(m3, cmd())
	if m3.notes.Value() != "kept" || m3.explainUndo != nil || m3.err != "AI provider error" {
		t.Fatalf("error path: notes=%q undo=%v err=%q", m3.notes.Value(), m3.explainUndo, m3.err)
	}
}

func TestAIBusyBlocksSavesAndNewActions(t *testing.T) {
	fa := enabledAsk()
	fa.gen = ask.Generation{Title: "g", Body: "gen body"}
	ed := &fakeEditor{}
	m := aiPanel(t, ed, nil, fa)
	m.title.SetValue("t")
	m.body.SetValue("b")
	m, _ = apply(m, ctrl('a'))
	for _, r := range "p" {
		m, _ = apply(m, press(r, string(r), 0))
	}
	m, genCmd := apply(m, enter())
	if !m.aiBusy {
		t.Fatal("not busy")
	}
	// New AI actions no-op while the generation is in flight.
	m, _ = apply(m, ctrl('t'))
	m, _ = apply(m, ctrl('e'))
	if len(fa.tagsCalls)+len(fa.explainCalls) != 0 {
		t.Fatal("AI action started while busy")
	}
	m, _ = apply(m, genCmd())
	if len(ed.creates) != 0 {
		t.Fatal("save ran while the box was open")
	}
	// A tags suggestion in flight blocks saves from the main panel —
	// the aiPending rule.
	m, tagsCmd := apply(m, ctrl('t'))
	if !m.tagsBusy {
		t.Fatal("not tags-busy")
	}
	m, saveCmd := apply(m, ctrlS())
	if saveCmd != nil || len(ed.creates) != 0 {
		t.Fatal("save ran while tags-busy")
	}
	m, _ = apply(m, tagsCmd())
	m, saveCmd = apply(m, ctrlS())
	if saveCmd == nil {
		t.Fatal("save blocked after busy cleared")
	}
	m, _ = apply(m, saveCmd())
	if len(ed.creates) != 1 {
		t.Fatal("no save after the suggestion")
	}
}

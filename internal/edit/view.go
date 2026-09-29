package edit

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/template"
	"github.com/jstevewhite/snp/internal/tui"
)

func (m Model) View() tea.View {
	th := tui.NewTheme(m.dark)
	if m.askMode {
		return m.writeAskView(th)
	}
	return m.writePanelView(th)
}

// writeAskView is the Ask-AI box (Ctrl+A): its own frame over the
// panel, like the picker's variable form.
func (m Model) writeAskView(th tui.Theme) tea.View {
	var b strings.Builder
	fmt.Fprintf(&b, "%s%s\n", th.Brand.Render("snp"), th.Dim.Render("  ask ai"))
	fmt.Fprintf(&b, "%s\n", m.askPrompt.View())
	fmt.Fprintf(&b, "%s\n", th.Dim.Render("kind: "+string(m.askKind)+"   (tab cycles)"))
	if m.err != "" {
		fmt.Fprintf(&b, "%s\n", th.Err.Render(m.err))
	}
	if m.aiBusy {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("asking…"))
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%s\n", th.Help.Render("enter generate   tab kind   esc back"))
	v := tea.NewView(th.Frame.Width(m.frameWidth()).Render(strings.TrimRight(b.String(), "\n")))
	v.AltScreen = true
	return v
}

func (m Model) writePanelView(th tui.Theme) tea.View {
	var b strings.Builder

	head := "add"
	if m.seed != nil {
		head = "edit"
	}
	fmt.Fprintf(&b, "%s%s\n", th.Brand.Render("snp"), th.Dim.Render("  "+head))

	if m.err != "" {
		fmt.Fprintf(&b, "%s\n", th.Err.Render(m.err))
	}

	fmt.Fprintf(&b, "%s\n", m.title.View())
	fmt.Fprintf(&b, "%s\n", th.Dim.Render("Body"))
	fmt.Fprintf(&b, "%s\n", m.body.View())
	if vars := template.Extract(m.body.Value()); len(vars) > 0 {
		names := make([]string, len(vars))
		for i, v := range vars {
			names[i] = v.Name
		}
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("Template: "+strings.Join(names, ", ")))
	}
	fmt.Fprintf(&b, "%s\n", m.notes.View())
	fmt.Fprintf(&b, "%s\n", m.language.View())
	m.writeSuggestions(&b, th)
	fmt.Fprintf(&b, "%s\n", m.folderLine(th))
	m.writeFolderList(&b, th)
	fmt.Fprintf(&b, "%s\n", m.tags.View())
	m.writeSuggestions(&b, th)

	fmt.Fprintf(&b, "%s\n", m.toggleLine(th, "Sensitive", m.sensitive, stopSensitive))
	fmt.Fprintf(&b, "%s\n", m.toggleLine(th, "Pinned", m.pinned, stopPinned))

	if m.sensitive && m.seed != nil && !m.revealed {
		fmt.Fprintf(&b, "%s\n", th.Warn.Render("Body masked. ctrl+r reveals it for editing; saving without revealing keeps it unchanged."))
	}
	if m.aiDone != "" && !m.aiBusy && !m.tagsBusy && !m.explainBusy {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render(m.aiDone))
	}
	if m.aiBusy {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("asking…"))
	}
	if m.tagsBusy {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("suggesting tags…"))
	}
	if m.explainBusy {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("explaining…"))
	}
	if m.saving {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("saving…"))
	}

	b.WriteByte('\n')
	if m.asking {
		fmt.Fprintf(&b, "%s\n", th.Warn.Render("Discard changes? y discard   n keep editing"))
		fmt.Fprintf(&b, "%s\n", th.Help.Render("y discard   n keep editing   ctrl+c quits anyway"))
	} else {
		help := "tab next   shift-tab back   ↑↓ lists   enter pick or advance   ctrl+s save   esc cancel"
		if m.aiAvailable() {
			help += "   ctrl+a ask ai"
			if !m.sensitive {
				help += "   ctrl+t tags   ctrl+e explain"
			}
		}
		if m.explainUndo != nil {
			help += "   ctrl+z undo explain"
		}
		fmt.Fprintf(&b, "%s\n", th.Help.Render(help))
	}

	v := tea.NewView(th.Frame.Width(m.frameWidth()).Render(strings.TrimRight(b.String(), "\n")))
	v.AltScreen = true
	return v
}

// folderLine renders the folder stop with its current selection. The
// full-replace write needs the ID intact; the picker below edits it.
func (m Model) folderLine(th tui.Theme) string {
	label := m.folderName
	if label == "" {
		if m.folderID != nil {
			label = *m.folderID
		} else {
			label = "(none)"
		}
	}
	line := th.Dim.Render("Folder     ") + th.Meta.Render(label)
	if m.focus == stopFolder {
		return th.Selected.Render(line)
	}
	return line
}

// writeFolderList renders the picker's inline list while the folder stop
// is focused: a six-row window over Unfiled + the sorted paths, the
// cursor row highlighted.
func (m Model) writeFolderList(b *strings.Builder, th tui.Theme) {
	if m.focus != stopFolder || len(m.folderChoices) == 0 {
		return
	}
	const rows = 6
	start := m.folderCursor - rows/2
	if start < 0 {
		start = 0
	}
	end := start + rows
	if end > len(m.folderChoices) {
		end = len(m.folderChoices)
		start = end - rows
		if start < 0 {
			start = 0
		}
	}
	w := m.contentWidth()
	for i := start; i < end; i++ {
		if i == m.folderCursor {
			fmt.Fprintf(b, "%s\n", th.Selected.Width(w).Render("  "+m.folderChoices[i].path))
		} else {
			fmt.Fprintf(b, "%s\n", th.Dim.Render("  "+m.folderChoices[i].path))
		}
	}
}

// writeSuggestions renders the focused suggestible field's matches below
// it, the highlighted one first-class. Everything is derived from focus,
// so the wrong field can never show another's suggestions.
func (m Model) writeSuggestions(b *strings.Builder, th tui.Theme) {
	if m.focus != stopTags && m.focus != stopLanguage {
		return
	}
	suggs := m.suggestions()
	if len(suggs) == 0 {
		return
	}
	w := m.contentWidth()
	for i, s := range suggs {
		if i == m.suggActive {
			fmt.Fprintf(b, "%s\n", th.Selected.Width(w).Render("  "+s))
		} else {
			fmt.Fprintf(b, "%s\n", th.Dim.Render("  "+s))
		}
	}
}

// toggleLine renders a boolean stop with its state.
func (m Model) toggleLine(th tui.Theme, label string, on bool, s stop) string {
	state := "no"
	if on {
		state = "yes"
	}
	line := th.Dim.Render(label) + "  " + th.Meta.Render(state)
	if m.focus == s {
		return th.Selected.Render(line)
	}
	return line
}

func (m Model) frameWidth() int {
	return m.width - 2
}

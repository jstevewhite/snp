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
	fmt.Fprintf(&b, "%s\n", m.tags.View())
	fmt.Fprintf(&b, "%s\n", m.folderLine(th))

	fmt.Fprintf(&b, "%s\n", m.toggleLine(th, "Sensitive", m.sensitive, stopSensitive))
	fmt.Fprintf(&b, "%s\n", m.toggleLine(th, "Pinned", m.pinned, stopPinned))

	if m.sensitive && m.seed != nil && !m.revealed {
		fmt.Fprintf(&b, "%s\n", th.Warn.Render("Body masked. ctrl+r reveals it for editing; saving without revealing keeps it unchanged."))
	}
	if m.saving {
		fmt.Fprintf(&b, "%s\n", th.Dim.Render("saving…"))
	}

	b.WriteByte('\n')
	if m.asking {
		fmt.Fprintf(&b, "%s\n", th.Warn.Render("Discard changes? y discard   n keep editing"))
		fmt.Fprintf(&b, "%s\n", th.Help.Render("y discard   n keep editing   ctrl+c quits anyway"))
	} else {
		fmt.Fprintf(&b, "%s\n", th.Help.Render("tab next   shift-tab back   enter advances   ctrl+s save   esc cancel"))
	}

	v := tea.NewView(th.Frame.Width(m.frameWidth()).Render(strings.TrimRight(b.String(), "\n")))
	v.AltScreen = true
	return v
}

// folderLine renders the carried folder. E3 displays it read-only — the
// full-replace write needs the ID intact; E4 replaces this with the picker.
func (m Model) folderLine(th tui.Theme) string {
	label := "Unfiled"
	if m.folderID != nil && m.folderName != "" {
		label = m.folderName
	} else if m.folderID != nil {
		label = *m.folderID
	}
	if m.seed == nil {
		label = "—"
	}
	return th.Dim.Render("Folder     ") + th.Meta.Render(label)
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

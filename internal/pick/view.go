package pick

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jstevewhite/snp/internal/tui"
)

func (m Model) writeList(b *strings.Builder) {
	th := tui.NewTheme(m.dark)
	inner := m.contentWidth()
	var body strings.Builder

	fmt.Fprintf(&body, "%s%s\n", th.Brand.Render("snp"), th.Dim.Render("  pick"))
	fmt.Fprintf(&body, "%s\n", m.filter.View())
	if m.err != "" {
		fmt.Fprintf(&body, "%s\n", th.Err.Render(m.err))
	} else {
		body.WriteByte('\n')
	}

	switch {
	case !m.settled && m.err == "":
		fmt.Fprintf(&body, "%s\n", th.Dim.Render("searching…"))
	case len(m.hits) == 0 && m.filter.Value() == "":
		fmt.Fprintf(&body, "%s\n", th.Dim.Render("No snippets yet"))
	case len(m.hits) == 0:
		fmt.Fprintf(&body, "%s\n", th.Dim.Render("No matches"))
	default:
		start, end := m.window()
		for i := start; i < end; i++ {
			fmt.Fprintf(&body, "%s\n", m.rowLine(th, m.hits[i], i == m.cursor, inner))
		}
	}

	body.WriteByte('\n')
	fmt.Fprintf(&body, "%s\n", th.Rule.Render(strings.Repeat("─", inner)))
	m.writeDetail(&body, th, inner)
	body.WriteByte('\n')
	hint := "↑↓ move   enter insert   esc clears, then cancels"
	if s, ok := m.selected(); ok && s.IsTemplate() {
		hint = "↑↓ move   enter fill   esc clears, then cancels"
	}
	fmt.Fprintf(&body, "%s\n", th.Help.Render(hint))

	b.WriteString(th.Frame.Width(m.frameWidth()).Render(strings.TrimRight(body.String(), "\n")))
	b.WriteByte('\n')
}

func (m Model) rowLine(th tui.Theme, s Snippet, selected bool, inner int) string {
	title := s.Title
	meta := rowMeta(s)
	metaW := utf8.RuneCountInString(meta)
	gap := 2
	if metaW > 0 && metaW+gap >= inner {
		meta = truncate(meta, inner/3)
		metaW = utf8.RuneCountInString(meta)
	}
	titleW := inner - gap
	if metaW > 0 {
		titleW = inner - metaW - gap
	}
	if titleW < 8 {
		titleW = 8
	}
	title = truncate(title, titleW)
	pad := inner - utf8.RuneCountInString(title) - metaW
	if pad < gap {
		pad = gap
	}
	if meta == "" {
		pad = 0
	}
	plain := title + strings.Repeat(" ", pad) + meta
	if selected {
		return th.Selected.Width(inner).Render(truncate(plain, inner))
	}
	line := th.Title.Render(title)
	if meta != "" {
		line += strings.Repeat(" ", pad) + th.Meta.Render(meta)
	}
	return line
}

func (m Model) writeDetail(b *strings.Builder, th tui.Theme, inner int) {
	s, ok := m.selected()
	if !ok {
		fmt.Fprintf(b, "%s\n", th.Dim.Render("Nothing selected"))
		return
	}
	if s.Sensitive {
		fmt.Fprintf(b, "%s\n", th.Warn.Render("Sensitive. Enter fetches it and inserts it with a leading space,"))
		fmt.Fprintf(b, "%s\n", th.Warn.Render("so zsh history skips it when HIST_IGNORE_SPACE is set."))
	} else if s.Body != "" {
		fmt.Fprintf(b, "%s\n", th.Code.Width(inner).Render(clipLines(s.Body, 5)))
	}
	if s.Notes != "" {
		fmt.Fprintf(b, "%s\n", th.Dim.Render(clipLines(s.Notes, 2)))
	}
}

func (m Model) writeForm(b *strings.Builder) {
	th := tui.NewTheme(m.dark)
	inner := m.contentWidth()
	var body strings.Builder

	fmt.Fprintf(&body, "%s\n", th.Brand.Render("snp"))
	fmt.Fprintf(&body, "%s\n", th.Heading.Render(m.form.Title))
	if meta := rowMeta(m.form); meta != "" {
		fmt.Fprintf(&body, "%s\n", th.Meta.Render(meta))
	}
	body.WriteByte('\n')
	for _, field := range m.fields {
		fmt.Fprintf(&body, "%s\n", field.View())
	}
	body.WriteByte('\n')
	fmt.Fprintf(&body, "%s\n", th.Dim.Render("rendered"))
	fmt.Fprintf(&body, "%s\n", th.Code.Width(inner).Render(clipLines(m.rendered(), 8)))
	if m.form.Sensitive {
		body.WriteByte('\n')
		fmt.Fprintf(&body, "%s\n", th.Warn.Render("Inserted with a leading space so zsh history skips it when HIST_IGNORE_SPACE is set."))
	}
	body.WriteByte('\n')
	fmt.Fprintf(&body, "%s\n", th.Help.Render("tab next   shift-tab back   enter insert   esc list"))

	b.WriteString(th.Frame.Width(m.frameWidth()).Render(strings.TrimRight(body.String(), "\n")))
	b.WriteByte('\n')
}

func (m Model) contentWidth() int {
	// Leave the last column empty. A box that fills the terminal exactly
	// makes the bottom-right corner wrap onto the next line.
	w := m.width - 5
	if w < 24 {
		w = 24
	}
	return w
}

func (m Model) frameWidth() int {
	return m.contentWidth() + 4
}

func (m Model) selected() (Snippet, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return Snippet{}, false
	}
	return m.hits[m.cursor], true
}

func (m Model) window() (int, int) {
	// Border, brand, filter, gap, rule, a few preview lines, help.
	rows := m.height - 14
	if rows < 3 {
		rows = 3
	}
	n := len(m.hits)
	if n <= rows {
		return 0, n
	}
	start := m.cursor - rows/2
	if start < 0 {
		start = 0
	}
	end := start + rows
	if end > n {
		end = n
		start = end - rows
	}
	return start, end
}

func rowMeta(s Snippet) string {
	var parts []string
	if s.Language != "" {
		parts = append(parts, s.Language)
	}
	for _, tag := range s.Tags {
		parts = append(parts, "#"+tag)
	}
	if s.IsTemplate() {
		parts = append(parts, "template")
	}
	return strings.Join(parts, "  ")
}

func clipLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[:n], "\n") + "\n…"
}

func truncate(s string, width int) string {
	if width <= 1 || utf8.RuneCountInString(s) <= width {
		return s
	}
	cut := 0
	count := 0
	for i := range s {
		if count == width-1 {
			cut = i
			break
		}
		count++
	}
	return s[:cut] + "…"
}

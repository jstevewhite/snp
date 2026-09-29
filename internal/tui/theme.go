// Package tui holds the terminal chrome shared by snp's Bubble Tea
// clients: the palette (matching the app's light and dark colors) and the
// terminal program setup they run under.
package tui

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// Theme is the chrome shared by the terminal clients. Colors follow the
// app's light and dark palettes (web/src/app.css): purple accent, warm
// paper, cool ink.
type Theme struct {
	Frame    lipgloss.Style
	Brand    lipgloss.Style
	Dim      lipgloss.Style
	Heading  lipgloss.Style
	Title    lipgloss.Style
	Meta     lipgloss.Style
	Selected lipgloss.Style
	Code     lipgloss.Style
	Err      lipgloss.Style
	Warn     lipgloss.Style
	Help     lipgloss.Style
	Rule     lipgloss.Style
}

// NewTheme builds the palette for a light or dark terminal.
func NewTheme(dark bool) Theme {
	pick := lipgloss.LightDark(dark)
	accent := pick(lipgloss.Color("#aa3bff"), lipgloss.Color("#c084fc"))
	text := pick(lipgloss.Color("#17171f"), lipgloss.Color("#f3f4f6"))
	muted := pick(lipgloss.Color("#6b6b76"), lipgloss.Color("#9ca3af"))
	border := pick(lipgloss.Color("#d9d9d4"), lipgloss.Color("#3a3c48"))
	selBg := pick(lipgloss.Color("#f4e8ff"), lipgloss.Color("#2c2438"))
	codeFg := pick(lipgloss.Color("#0a3069"), lipgloss.Color("#e7e5f0"))
	codeBg := pick(lipgloss.Color("#f4f3ec"), lipgloss.Color("#1f2028"))
	danger := pick(lipgloss.Color("#b3372c"), lipgloss.Color("#e06c5f"))

	return Theme{
		Frame: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		Brand:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		Dim:      lipgloss.NewStyle().Foreground(muted),
		Heading:  lipgloss.NewStyle().Foreground(text).Bold(true),
		Title:    lipgloss.NewStyle().Foreground(text),
		Meta:     lipgloss.NewStyle().Foreground(muted),
		Selected: lipgloss.NewStyle().Foreground(text).Background(selBg).Bold(true),
		Code:     lipgloss.NewStyle().Foreground(codeFg).Background(codeBg),
		Err:      lipgloss.NewStyle().Foreground(danger),
		Warn:     lipgloss.NewStyle().Foreground(danger),
		Help:     lipgloss.NewStyle().Foreground(muted),
		Rule:     lipgloss.NewStyle().Foreground(border),
	}
}

// StyleInput styles a text input with the theme's accent and text colors.
func StyleInput(ti *textinput.Model, dark bool) {
	pick := lipgloss.LightDark(dark)
	accent := pick(lipgloss.Color("#aa3bff"), lipgloss.Color("#c084fc"))
	text := pick(lipgloss.Color("#17171f"), lipgloss.Color("#f3f4f6"))
	muted := pick(lipgloss.Color("#6b6b76"), lipgloss.Color("#9ca3af"))

	s := textinput.DefaultStyles(dark)
	s.Focused.Prompt = lipgloss.NewStyle().Foreground(accent).Bold(true)
	s.Focused.Text = lipgloss.NewStyle().Foreground(text)
	s.Focused.Placeholder = lipgloss.NewStyle().Foreground(muted)
	s.Blurred.Prompt = lipgloss.NewStyle().Foreground(muted)
	s.Blurred.Text = lipgloss.NewStyle().Foreground(text)
	s.Blurred.Placeholder = lipgloss.NewStyle().Foreground(muted)
	s.Cursor.Color = accent
	ti.SetStyles(s)
}

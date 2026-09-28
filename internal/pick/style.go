package pick

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

// theme is the picker chrome. Colors follow the app's light and dark
// palettes (web/src/app.css): purple accent, warm paper, cool ink.
type theme struct {
	frame    lipgloss.Style
	brand    lipgloss.Style
	dim      lipgloss.Style
	heading  lipgloss.Style
	title    lipgloss.Style
	meta     lipgloss.Style
	selected lipgloss.Style
	code     lipgloss.Style
	err      lipgloss.Style
	warn     lipgloss.Style
	help     lipgloss.Style
	rule     lipgloss.Style
}

func newTheme(dark bool) theme {
	pick := lipgloss.LightDark(dark)
	accent := pick(lipgloss.Color("#aa3bff"), lipgloss.Color("#c084fc"))
	text := pick(lipgloss.Color("#17171f"), lipgloss.Color("#f3f4f6"))
	muted := pick(lipgloss.Color("#6b6b76"), lipgloss.Color("#9ca3af"))
	border := pick(lipgloss.Color("#d9d9d4"), lipgloss.Color("#3a3c48"))
	selBg := pick(lipgloss.Color("#f4e8ff"), lipgloss.Color("#2c2438"))
	codeFg := pick(lipgloss.Color("#0a3069"), lipgloss.Color("#e7e5f0"))
	codeBg := pick(lipgloss.Color("#f4f3ec"), lipgloss.Color("#1f2028"))
	danger := pick(lipgloss.Color("#b3372c"), lipgloss.Color("#e06c5f"))

	return theme{
		frame: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(0, 1),
		brand:    lipgloss.NewStyle().Foreground(accent).Bold(true),
		dim:      lipgloss.NewStyle().Foreground(muted),
		heading:  lipgloss.NewStyle().Foreground(text).Bold(true),
		title:    lipgloss.NewStyle().Foreground(text),
		meta:     lipgloss.NewStyle().Foreground(muted),
		selected: lipgloss.NewStyle().Foreground(text).Background(selBg).Bold(true),
		code:     lipgloss.NewStyle().Foreground(codeFg).Background(codeBg),
		err:      lipgloss.NewStyle().Foreground(danger),
		warn:     lipgloss.NewStyle().Foreground(danger),
		help:     lipgloss.NewStyle().Foreground(muted),
		rule:     lipgloss.NewStyle().Foreground(border),
	}
}

func styleInput(ti *textinput.Model, dark bool) {
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

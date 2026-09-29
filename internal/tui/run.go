package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// Run draws model on the terminal rather than on stdout: it opens /dev/tty
// for input and rendering, so a program whose stdout is captured (a shell
// widget) still shows its screen. A fresh pty often answers no color query
// and the renderer then drops every color, so truecolor is forced. It
// returns the final model.
func Run(ctx context.Context, model tea.Model) (tea.Model, error) {
	in, tty, err := tea.OpenTTY()
	if err != nil {
		return nil, fmt.Errorf("needs a terminal: %w", err)
	}
	defer in.Close()
	defer tty.Close()

	p := tea.NewProgram(model,
		tea.WithInput(in),
		tea.WithOutput(tty),
		tea.WithContext(ctx),
		tea.WithColorProfile(colorprofile.TrueColor),
	)
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	return final, nil
}

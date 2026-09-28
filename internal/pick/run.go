package pick

import (
	"context"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"golang.org/x/term"
)

// Run draws the picker on the terminal and writes the accepted command to
// out. The screen never uses out, so a shell widget can capture it with
// command substitution. Cancel returns ErrCanceled and writes nothing.
func Run(ctx context.Context, lib Library, out io.Writer) error {
	in, tty, err := tea.OpenTTY()
	if err != nil {
		return fmt.Errorf("pick needs a terminal: %w", err)
	}
	defer in.Close()
	defer tty.Close()

	p := tea.NewProgram(newModel(ctx, lib),
		tea.WithInput(in),
		tea.WithOutput(tty),
		tea.WithContext(ctx),
		// A fresh pty often answers no color query, and the renderer then
		// drops every color. The terminals this runs in can do truecolor.
		tea.WithColorProfile(colorprofile.TrueColor),
	)
	final, err := p.Run()
	if err != nil {
		return err
	}
	m, ok := final.(Model)
	if !ok {
		return fmt.Errorf("pick: unexpected model %T", final)
	}
	text, ok := m.Result()
	if !ok {
		return ErrCanceled
	}
	if terminal(out) {
		// Nothing is capturing stdout, so this process cannot put the
		// command into the line editor. Printing it without a newline
		// makes zsh (and Starship) paint a % and start a fresh prompt.
		fmt.Fprintln(os.Stderr, "snp: source <(snp widget) to leave the command on the prompt")
		_, err = fmt.Fprintln(out, text)
		return err
	}
	_, err = io.WriteString(out, text)
	return err
}

func terminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}

package edit

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/pick"
	"github.com/jstevewhite/snp/internal/tui"
)

// ErrCanceled means the panel closed without saving: Ctrl+C or Esc past
// the dirty guard.
var ErrCanceled = fmt.Errorf("edit: %w", pick.ErrCanceled)

// Run draws the editor panel on the terminal (tui.Run handles /dev/tty)
// and returns the saved row. seed nil opens the create form.
func Run(ctx context.Context, ed pick.Editor, seed *pick.Snippet) (pick.Snippet, error) {
	return panel(tui.Run(ctx, New(ctx, ed, seed)))
}

// RunCreate is Run for `snp add`, prefilled from flags.
func RunCreate(ctx context.Context, ed pick.Editor, prefill Prefill) (pick.Snippet, error) {
	return panel(tui.Run(ctx, NewCreate(ctx, ed, prefill)))
}

func panel(final tea.Model, err error) (pick.Snippet, error) {
	if err != nil {
		return pick.Snippet{}, err
	}
	m, ok := final.(Model)
	if !ok {
		return pick.Snippet{}, fmt.Errorf("edit: unexpected model %T", final)
	}
	if s, ok := m.Result(); ok {
		return s, nil
	}
	return pick.Snippet{}, ErrCanceled
}

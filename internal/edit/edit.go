package edit

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/jstevewhite/snp/internal/ask"
	"github.com/jstevewhite/snp/internal/pick"
	"github.com/jstevewhite/snp/internal/tui"
)

// ErrCanceled means the panel closed without saving: Ctrl+C or Esc past
// the dirty guard.
var ErrCanceled = fmt.Errorf("edit: %w", pick.ErrCanceled)

// Run draws the editor panel on the terminal (tui.Run handles /dev/tty)
// and returns the saved row. seed nil opens the create form. svc is
// the AI surface (spec §13); nil hides the AI controls.
func Run(ctx context.Context, ed pick.Editor, svc ask.Service, seed *pick.Snippet) (pick.Snippet, error) {
	m := New(ctx, ed, seed)
	m.ai = svc
	return panel(tui.Run(ctx, m))
}

// RunCreate is Run for `snp add`, prefilled from flags.
func RunCreate(ctx context.Context, ed pick.Editor, svc ask.Service, prefill Prefill) (pick.Snippet, error) {
	m := NewCreate(ctx, ed, prefill)
	m.ai = svc
	return panel(tui.Run(ctx, m))
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

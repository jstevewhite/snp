package pick

import (
	"context"
	"fmt"

	"github.com/jstevewhite/snp/internal/tui"
)

// Choose runs the list in select-only mode: Enter returns the highlighted
// row instead of inserting its command. It is the chooser behind
// `snp edit` with no argument — no reveal, no template form, nothing on
// stdout. Cancel returns ErrCanceled.
func Choose(ctx context.Context, lib Library) (Snippet, error) {
	m := newModel(ctx, lib)
	m.selectOnly = true
	final, err := tui.Run(ctx, m)
	if err != nil {
		return Snippet{}, err
	}
	fm, ok := final.(Model)
	if !ok {
		return Snippet{}, fmt.Errorf("choose: unexpected model %T", final)
	}
	if s, ok := fm.Chosen(); ok {
		return s, nil
	}
	return Snippet{}, ErrCanceled
}

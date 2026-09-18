//go:build darwin || linux

package main

import (
	"context"
	"sync/atomic"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// CloseGuard asks the mounted frontend to resolve pending edits before a
// native close. It carries no snippet content and lives only in Wails wiring.
type CloseGuard struct {
	ctx     context.Context
	ready   atomic.Bool
	allowed atomic.Bool
}

func (g *CloseGuard) Ready() { g.ready.Store(true) }
func (g *CloseGuard) ConfirmClose() {
	g.allowed.Store(true)
	runtime.Quit(g.ctx)
}
func (g *CloseGuard) beforeClose(ctx context.Context) bool {
	if !g.ready.Load() || g.allowed.Load() {
		return false
	}
	runtime.WindowExecJS(ctx, `window.dispatchEvent(new Event('snp:close-request'))`)
	return true
}

//go:build linux

package main

import (
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	webembed "github.com/jstevewhite/snp/web"
)

// addPlatformOptions applies the Linux-specific Wails options (spec §12).
// Each one replaces a default that is wrong for this app:
//
//   - WebviewGpuPolicy: wails sets WebviewGpuPolicyNever — software
//     rendering — when options.Linux is nil, which makes the embedded
//     WebKit view crawl on a script list. Ask for on-demand instead.
//   - ProgramName: sets GTK's g_set_prgname(), which the desktop entry's
//     StartupWMClass must match (deploy/snp.desktop) or the window shows
//     up as a second, unnamed taskbar entry.
//   - Icon: the GTK window and minimized-window icon. The macOS bundle
//     gets its icon from the .app, so this is Linux-only.
func addPlatformOptions(opts *options.App) {
	opts.Linux = &linux.Options{
		WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		ProgramName:      "snp",
		Icon:             webembed.IconPNG,
	}
}

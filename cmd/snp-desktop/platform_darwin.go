//go:build darwin

package main

import "github.com/wailsapp/wails/v2/pkg/options"

// addPlatformOptions applies macOS-specific Wails options. There are
// none: the wails defaults are right for this app, and the one macOS
// build quirk (`CGO_LDFLAGS=-framework UniformTypeIdentifiers`) belongs
// to the link step, not to the options — it lives in the Makefile's
// `desktop` target. The function exists as an explicit no-op so the
// platform split stays uniform across darwin/linux (and a later
// windows file).
func addPlatformOptions(opts *options.App) {}

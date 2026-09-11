// Package webembed exposes the built SPA to the Go binary.
//
// The Makefile builds the Svelte app into web/dist before the Go build.
// A stub index.html is checked in so a bare `go build` works before the
// web app exists (spec §1).
package webembed

import (
	"embed"
)

//go:embed all:dist
var FS embed.FS

// IconPNG is the app icon for platforms that need the bytes at runtime:
// the Linux GTK window icon (wails linux.Options.Icon). It is the same
// 512px artwork the macOS bundle falls back to and the PWA installs.
//
//go:embed public/pwa-512x512.png
var IconPNG []byte

//go:build darwin || linux

package main

import (
	"context"
	"path/filepath"

	"github.com/jstevewhite/snp/internal/desktop"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func nativeFileDialogs(ctx func() context.Context) desktop.FileDialogs {
	return desktop.FileDialogs{
		OpenImport: func() (string, error) {
			return runtime.OpenFileDialog(ctx(), runtime.OpenDialogOptions{
				Title: "Import snp JSON", Filters: []runtime.FileFilter{{DisplayName: "snp JSON or encrypted export", Pattern: "*.json;*.age"}},
			})
		},
		SaveData: func(filename string) (string, error) {
			return runtime.SaveFileDialog(ctx(), runtime.SaveDialogOptions{
				Title: "Save snp data", DefaultFilename: filename,
				Filters: []runtime.FileFilter{{DisplayName: "snp data", Pattern: "*" + filepath.Ext(filename)}},
			})
		},
	}
}

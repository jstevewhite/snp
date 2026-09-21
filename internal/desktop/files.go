package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"time"
)

const maxImportBytes = 16 << 20

// FileDialogs supplies platform-native selection; cancellation returns an empty path.
type FileDialogs struct {
	OpenImport func() (string, error)
	SaveData   func(filename string) (string, error)
}

type ImportFile struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

func (a *App) OpenImport() (*ImportFile, error) {
	if a.dialogs.OpenImport == nil {
		return nil, errors.New("Native file dialog is unavailable")
	}
	path, err := a.dialogs.OpenImport()
	if err != nil || path == "" {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !stat.Mode().IsRegular() {
		return nil, errors.New("Choose a regular JSON or .json.age file")
	}
	data, err := io.ReadAll(io.LimitReader(f, maxImportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxImportBytes {
		return nil, errors.New("Selected files must be 16 MiB or smaller; use snp decrypt and snp import for larger files")
	}
	return &ImportFile{Name: filepath.Base(path), Text: string(data)}, nil
}

// SaveData only exports server-owned content to a path explicitly chosen by
// the user. Temp+rename prevents failed writes from truncating an existing file.
func (a *App) SaveData(kind string) (bool, error) {
	return a.saveData(kind, false, "")
}

// SaveEncryptedData receives the password in memory only, never in a URL or file.
func (a *App) SaveEncryptedData(kind, password string) (bool, error) {
	return a.saveData(kind, true, password)
}

func (a *App) saveData(kind string, encrypt bool, password string) (bool, error) {
	method, path, extension := "GET", "/api/export", "json"
	if kind == "backup" {
		method, path, extension = "POST", "/api/backup", "zip"
	} else if kind != "export" {
		return false, errors.New("Unknown download type")
	}
	if a.dialogs.SaveData == nil {
		return false, errors.New("Native file dialog is unavailable")
	}
	if encrypt {
		method = "POST"
		extension += ".age"
	}
	suggested := "snp-" + kind + "-" + time.Now().UTC().Format("20060102-150405") + "." + extension
	dest, err := a.dialogs.SaveData(suggested)
	if err != nil || dest == "" {
		return false, err
	}
	body, err := json.Marshal(map[string]any{"encrypt": encrypt, "password": password})
	if err != nil {
		return false, err
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	if method == "POST" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	a.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		var failure struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(rec.Body.Bytes(), &failure) == nil && failure.Error != "" {
			return false, errors.New(failure.Error)
		}
		return false, fmt.Errorf("Download failed (HTTP %d)", rec.Code)
	}
	if err := atomicSave(dest, rec.Body.Bytes()); err != nil {
		return false, err
	}
	return true, nil
}

func atomicSave(dest string, data []byte) (err error) {
	file, err := os.CreateTemp(filepath.Dir(dest), ".snp-save-")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	defer file.Close()
	if _, err = file.Write(data); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, dest)
}

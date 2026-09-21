package desktop

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeExportAndBackup(t *testing.T) {
	app := testApp(t)
	if res, err := app.CallAPI("POST", "/api/snippets", `{"title":"private","body":"native-secret","is_sensitive":true}`); err != nil || res.Status != 201 {
		t.Fatalf("create: %+v %v", res, err)
	}
	for _, kind := range []string{"export", "backup"} {
		t.Run(kind, func(t *testing.T) {
			dest := filepath.Join(t.TempDir(), "download")
			app.dialogs.SaveData = func(name string) (string, error) {
				if !strings.HasPrefix(name, "snp-"+kind+"-") {
					t.Fatal(name)
				}
				return dest, nil
			}
			ok, err := app.SaveData(kind)
			if err != nil || !ok {
				t.Fatalf("save: %v %v", ok, err)
			}
			info, err := os.Stat(dest)
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("permissions: %v %v", info, err)
			}
			data, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "export" && !bytes.Contains(data, []byte("native-secret")) {
				t.Fatal("export missing content")
			}
			if kind == "backup" {
				zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
				if err != nil || len(zr.File) != 3 {
					t.Fatalf("invalid zip: %v", err)
				}
			}
		})
	}
}

func TestNativeCancellationAndFailedSavePreserveFiles(t *testing.T) {
	calls := 0
	app := NewAppWithDialogs(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; http.Error(w, "failure", 500) }), discardLog(), FileDialogs{SaveData: func(string) (string, error) { return "", nil }, OpenImport: func() (string, error) { return "", nil }})
	if ok, err := app.SaveData("backup"); err != nil || ok || calls != 0 {
		t.Fatalf("cancel: %v %v", ok, err)
	}
	if file, err := app.OpenImport(); err != nil || file != nil {
		t.Fatalf("cancel import: %+v %v", file, err)
	}
	if _, err := app.SaveData("arbitrary"); err == nil {
		t.Fatal("unknown type accepted")
	}
	dest := filepath.Join(t.TempDir(), "existing")
	if err := os.WriteFile(dest, []byte("keep me"), 0o600); err != nil {
		t.Fatal(err)
	}
	app.dialogs.SaveData = func(string) (string, error) { return dest, nil }
	if _, err := app.SaveData("export"); err == nil {
		t.Fatal("failed endpoint saved")
	}
	data, _ := os.ReadFile(dest)
	if string(data) != "keep me" {
		t.Fatal("existing file lost")
	}
}

func TestNativeImportLimitsAndReadsChosenFile(t *testing.T) {
	app := testApp(t)
	dest := filepath.Join(t.TempDir(), "import.json")
	data := `{"version":1,"snippets":[]}`
	if err := os.WriteFile(dest, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	app.dialogs.OpenImport = func() (string, error) { return dest, nil }
	file, err := app.OpenImport()
	if err != nil || file.Name != "import.json" || file.Text != data {
		t.Fatalf("read: %+v %v", file, err)
	}
	if err := os.Truncate(dest, maxImportBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := app.OpenImport(); err == nil {
		t.Fatal("oversized file allowed")
	}
}

func TestNativeProtectedDownload(t *testing.T) {
	app := testApp(t)
	dest := filepath.Join(t.TempDir(), "backup.zip.age")
	app.dialogs.SaveData = func(name string) (string, error) {
		if !strings.HasSuffix(name, ".zip.age") {
			t.Fatal(name)
		}
		return dest, nil
	}
	if saved, err := app.SaveEncryptedData("backup", "safe native file password"); err != nil || !saved {
		t.Fatalf("encrypted save: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil || !bytes.HasPrefix(data, []byte("-----BEGIN AGE ENCRYPTED FILE-----")) {
		t.Fatalf("unencrypted native file: %v", err)
	}
	if saved, err := app.SaveEncryptedData("backup", ""); err == nil || saved {
		t.Fatal("empty password accepted")
	}
	after, _ := os.ReadFile(dest)
	if !bytes.Equal(after, data) {
		t.Fatal("failed encryption changed file")
	}
}

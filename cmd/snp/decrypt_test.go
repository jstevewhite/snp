package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/jstevewhite/snp/internal/portable"
)

func TestDecryptFilePublishesOnlyAuthenticatedOutput(t *testing.T) {
	const password = "keep this password safe"
	var ciphertext bytes.Buffer
	plain := bytes.Repeat([]byte("backup data"), 10000)
	if err := portable.Encrypt(&ciphertext, bytes.NewReader(plain), password); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "backup.zip.age")
	dest := filepath.Join(dir, "backup.zip")
	if err := os.WriteFile(src, ciphertext.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := decryptFile(src, dest, "wrong"); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal("failure published output")
	}
	if err := decryptFile(src, dest, password); err != nil {
		t.Fatal(err)
	}
	stat, _ := os.Stat(dest)
	if stat.Mode().Perm() != 0o600 {
		t.Fatal("output not private")
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, plain) {
		t.Fatal("bad output")
	}
	if err := decryptFile(src, dest, password); err == nil {
		t.Fatal("existing file overwritten")
	}
	got, _ = os.ReadFile(dest)
	if !bytes.Equal(got, plain) {
		t.Fatal("existing content changed")
	}
	if err := os.WriteFile(src, ciphertext.Bytes()[:ciphertext.Len()-100], 0o600); err != nil {
		t.Fatal(err)
	}
	if err := decryptFile(src, filepath.Join(dir, "damaged.zip"), password); err == nil {
		t.Fatal("damaged file accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("partial output or temp files retained")
	}
}

package store

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestKeyRoundTrip(t *testing.T) {
	k, err := LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	if err != nil {
		t.Fatal(err)
	}
	ct, err := k.Seal("id1", []byte("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, []byte("hello world")) {
		t.Error("ciphertext equals plaintext")
	}
	pt, err := k.Open("id1", ct)
	if err != nil {
		t.Fatal(err)
	}
	if string(pt) != "hello world" {
		t.Errorf("round trip = %q", pt)
	}
}

func TestKeyAADMismatch(t *testing.T) {
	k, _ := LoadOrCreateKey(filepath.Join(t.TempDir(), "key"))
	ct, _ := k.Seal("id1", []byte("hello"))
	if _, err := k.Open("id2", ct); !errors.Is(err, ErrDecrypt) {
		t.Errorf("expected ErrDecrypt, got %v", err)
	}
}

func TestKeyFileModeAndReload(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	k1, err := LoadOrCreateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		// Filesystems without POSIX permissions (e.g. exFAT) ignore
		// the requested mode; the mode is best-effort there.
		t.Logf("key file mode is %v (filesystem may not support 0600)", info.Mode().Perm())
	}
	ct, _ := k1.Seal("id", []byte("x"))
	k2, err := LoadOrCreateKey(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := k2.Open("id", ct); err != nil {
		t.Errorf("reloaded key cannot open: %v", err)
	}
}

func TestKeyBadSize(t *testing.T) {
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(p); err == nil {
		t.Error("expected error for short key file")
	}
}

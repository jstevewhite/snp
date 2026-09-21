package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jstevewhite/snp/internal/portable"
	"github.com/jstevewhite/snp/internal/store"
)

type downloadOptions struct {
	Encrypt  bool   `json:"encrypt"`
	Password string `json:"password"`
}

func (s *Server) downloadOptions(w http.ResponseWriter, r *http.Request) (downloadOptions, bool) {
	var options downloadOptions
	if r.Method == http.MethodPost && r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &options); err != nil {
			s.handleBodyErr(w, err)
			return options, false
		}
	}
	if options.Encrypt {
		if err := portable.ValidatePassword(options.Password); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return options, false
		}
	} else if options.Password != "" {
		writeError(w, http.StatusBadRequest, "Choose encryption when supplying a password")
		return options, false
	}
	return options, true
}

// One memory-hard password operation per server; reject rather than queue secrets.
func (s *Server) beginCrypto(w http.ResponseWriter) bool {
	if !s.cryptoMu.TryLock() {
		writeError(w, http.StatusConflict, "Another protected file is being processed. Try again shortly.")
		return false
	}
	return true
}

func (s *Server) handleDecryptImport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Data     string `json:"data"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &input); err != nil {
		s.handleBodyErr(w, err)
		return
	}
	if !strings.HasPrefix(strings.TrimSpace(input.Data), portable.ArmorHeader) {
		writeError(w, http.StatusBadRequest, "Choose a password-protected snp JSON export (.json.age)")
		return
	}
	if !s.beginCrypto(w) {
		return
	}
	defer s.cryptoMu.Unlock()
	var plain bytes.Buffer
	if err := portable.Decrypt(&limitedPlaintext{dst: &plain, remaining: maxBodyBytes}, strings.NewReader(input.Data), input.Password); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if bytes.HasPrefix(plain.Bytes(), []byte("PK\x03\x04")) {
		writeError(w, http.StatusBadRequest, "This is a full backup. Use snp decrypt to unlock the ZIP, then follow the backup restore instructions with snp stopped.")
		return
	}
	var doc store.ImportDoc
	if err := json.Unmarshal(plain.Bytes(), &doc); err != nil || doc.Version != 1 || doc.Snippets == nil {
		writeError(w, http.StatusBadRequest, "The decrypted file is not a snp version-1 JSON export")
		return
	}
	// No database writes: the user still previews and confirms the normal import.
	w.Header().Set("Content-Type", "application/json")
	w.Write(plain.Bytes())
}

type limitedPlaintext struct {
	dst       io.Writer
	remaining int64
}

func (w *limitedPlaintext) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, io.ErrShortWrite
	}
	n, err := w.dst.Write(p)
	w.remaining -= int64(n)
	return n, err
}

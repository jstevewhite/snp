package server

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/jstevewhite/snp/internal/portable"
)

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	options, ok := s.downloadOptions(w, r)
	if !ok {
		return
	}
	if options.Encrypt {
		if !s.beginCrypto(w) {
			return
		}
		defer s.cryptoMu.Unlock()
	}
	if !s.backupMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "A backup is already being prepared. Try again shortly."})
		return
	}
	defer s.backupMu.Unlock()
	dir, err := os.MkdirTemp("", "snp-download-")
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "backup.zip")
	if err := s.store.BackupArchive(path); err != nil {
		s.handleStoreErr(w, err)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	defer f.Close()
	extension := ".zip"
	if options.Encrypt {
		encrypted, err := os.OpenFile(filepath.Join(dir, "backup.zip.age"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
		if err != nil {
			s.handleStoreErr(w, err)
			return
		}
		defer encrypted.Close()
		if err := portable.Encrypt(encrypted, f, options.Password); err != nil {
			s.handleStoreErr(w, err)
			return
		}
		if _, err := encrypted.Seek(0, 0); err != nil {
			s.handleStoreErr(w, err)
			return
		}
		f = encrypted
		extension += ".age"
		w.Header().Set("Content-Type", "application/octet-stream")
	} else {
		w.Header().Set("Content-Type", "application/zip")
	}
	w.Header().Set("Content-Disposition", `attachment; filename="snp-backup-`+time.Now().UTC().Format("20060102-150405")+extension+`"`)
	http.ServeContent(w, r, "backup.zip", time.Time{}, f)
}

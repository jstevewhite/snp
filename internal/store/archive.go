package store

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// BackupArchive packages a verified standalone database and its matching key.
// The ZIP is not password-encrypted; its holder can decrypt sensitive content.
// dest must not exist. No live DB/WAL files are copied directly.
func (s *Store) BackupArchive(dest string) (err error) {
	key, err := s.requireKey()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "snp-backup-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	dbPath := filepath.Join(dir, "snp.db")
	if err = s.Backup(dbPath, false); err != nil {
		return err
	}
	dbFile, err := os.Open(dbPath)
	if err != nil {
		return err
	}
	defer dbFile.Close()
	out, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := out.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(dest)
		}
	}()
	zw := zip.NewWriter(out)
	entries := []struct {
		name string
		data io.Reader
	}{
		{"snp.db", dbFile},
		{"key", bytes.NewReader(key.key)},
		{"RESTORE.txt", strings.NewReader(backupRestoreInstructions)},
	}
	for _, entry := range entries {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Deflate}
		header.SetMode(0o600)
		header.SetModTime(s.clock.Now())
		writer, createErr := zw.CreateHeader(header)
		if createErr != nil {
			return fmt.Errorf("backup archive: %w", createErr)
		}
		if _, copyErr := io.Copy(writer, entry.data); copyErr != nil {
			return fmt.Errorf("backup archive: %w", copyErr)
		}
	}
	if err = zw.Close(); err != nil {
		return err
	}
	return out.Sync()
}

const backupRestoreInstructions = `snp database backup

This ZIP contains a consistent SQLite database (snp.db) and the matching
32-byte encryption key (key). It includes live snippets, folders, tags,
Trash and revision history. It does not include config.toml, Tailscale
state, or browser/desktop appearance preferences.

This extracted ZIP is no longer password-protected, even if it came from a
.zip.age download. Anyone with both files can read sensitive snippets and
history. Store it privately. The .zip.age file uses a separate password; snp
cannot reset that password or recover a forgotten one.

Restore:
1. Stop every snp server and desktop app using the destination state directory.
2. Keep a copy of that entire existing state directory as a rollback backup.
3. Extract snp.db and key into a NEW EMPTY directory. Keep directory permissions
   private (0700) and file permissions private (0600) on macOS/Linux.
   Do not leave old snp.db-wal or snp.db-shm files alongside a restored database.
4. Start snp with --state-dir pointing to that directory (and your usual
   configuration). Use the same or a newer snp version than the backup.
   Example: snp-desktop --state-dir /path/to/restored-state
   Or: snp serve --state-dir /path/to/restored-state
5. In every browser/desktop client, choose Settings > Full resync.

JSON Import is for snippet exports, not this archive. Restoring this database
replaces the entire library, including its history and Trash, with this snapshot.
`

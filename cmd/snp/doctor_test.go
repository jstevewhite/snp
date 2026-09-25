package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/store"
)

// rawDB opens a second connection to the same database so a test can
// write around the store's own helpers, which is how the corruption these
// checks look for actually arises.
func rawDB(t *testing.T, cfg config.Config) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+dbPath(cfg))
	if err != nil {
		t.Fatalf("open raw connection: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// corruptIndex deletes one snippet's index row, leaving the content table
// untouched.
func corruptIndex(t *testing.T, cfg config.Config) {
	t.Helper()
	db := rawDB(t, cfg)
	if _, err := db.Exec(`DELETE FROM snippets_fts WHERE rowid = (SELECT MIN(rowid) FROM snippets)`); err != nil {
		t.Fatalf("delete index row: %v", err)
	}
}

func TestDoctorCmdHealthy(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)

	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{}, &out)
	if err != nil {
		t.Fatalf("doctorCmd: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0\n%s", code, out.String())
	}
	text := out.String()
	if !strings.Contains(text, "no problems found") {
		t.Errorf("output missing the healthy line:\n%s", text)
	}
	// Every default check must appear in the report.
	for _, name := range store.DoctorCheckNames() {
		if !strings.Contains(text, name) {
			t.Errorf("check %q missing from output:\n%s", name, text)
		}
	}
}

func TestDoctorCmdNoDatabase(t *testing.T) {
	cfg := testConfig(t)
	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{}, &out)
	if err == nil {
		t.Fatal("want an error for a missing database")
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "no database") {
		t.Errorf("error = %v, want it to mention the missing database", err)
	}
}

func TestDoctorCmdDetectsAndRepairsIndex(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)
	corruptIndex(t, cfg)

	var before bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{Only: []string{store.CheckFTSCount}}, &before)
	if err != nil {
		t.Fatalf("doctorCmd: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for a damaged index\n%s", code, before.String())
	}

	var after bytes.Buffer
	code, err = doctorCmd(cfg, doctorCLIOptions{
		Repair: true, Only: []string{store.CheckFTS, store.CheckFTSCount},
	}, &after)
	if err != nil {
		t.Fatalf("doctorCmd --repair: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code after repair = %d, want 0\n%s", code, after.String())
	}
	if !strings.Contains(after.String(), "rebuilt the FTS index") {
		t.Errorf("output does not report the rebuild:\n%s", after.String())
	}
}

func TestDoctorCmdOnlyValidation(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)

	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{Only: []string{"nonsense"}}, &out)
	if err == nil {
		t.Fatal("want an error for an unknown check")
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "unknown check") {
		t.Errorf("error = %v, want it to name the unknown check", err)
	}
}

// Repairing a selection with nothing repairable is a usage problem, not a
// silent no-op.
func TestDoctorCmdRepairWithUnrepairableSelection(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)

	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{Repair: true, Only: []string{store.CheckSchema}}, &out)
	if err == nil {
		t.Fatal("want an error when the selection has nothing to repair")
	}
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
}

func TestDoctorCmdJSON(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)
	corruptIndex(t, cfg)

	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{
		JSON: true, Repair: true, Only: []string{store.CheckFTS, store.CheckFTSCount},
	}, &out)
	if err != nil {
		t.Fatalf("doctorCmd: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0 after repair\n%s", code, out.String())
	}
	var payload doctorJSON
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v\n%s", err, out.String())
	}
	if payload.Report.Healthy {
		t.Error("report.healthy = true, want the before-state to be unhealthy")
	}
	if payload.After == nil {
		t.Fatal("after report missing after a repair")
	}
	if !payload.After.Healthy {
		t.Errorf("after.healthy = false, want true: %+v", payload.After.Checks)
	}
	if payload.Repair == nil || !payload.Repair.RebuiltFTS {
		t.Errorf("repair result = %+v, want a rebuild", payload.Repair)
	}
}

func TestDoctorCmdFixOrphans(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)
	// The seeded snippet lives in folder "shell". Delete the folder row
	// without going through DeleteFolder, leaving a live snippet pointing
	// at a deleted folder: the state that blocks purge.
	db := rawDB(t, cfg)
	if _, err := db.Exec(`UPDATE folders SET deleted_at = '2026-09-01T12:00:00Z' WHERE name = 'shell'`); err != nil {
		t.Fatalf("orphan the folder: %v", err)
	}

	var before bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{Only: []string{store.CheckOrphans}}, &before)
	if err != nil {
		t.Fatalf("doctorCmd: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1 for an orphan\n%s", code, before.String())
	}

	var out bytes.Buffer
	code, err = doctorCmd(cfg, doctorCLIOptions{
		JSON: true, FixOrphans: true, Only: []string{store.CheckOrphans},
	}, &out)
	if err != nil {
		t.Fatalf("doctorCmd --fix-orphans: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code after fix = %d, want 0\n%s", code, out.String())
	}
	var payload doctorJSON
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if payload.OrphansFixed != 1 {
		t.Errorf("orphans_fixed = %d, want 1", payload.OrphansFixed)
	}
	if payload.After == nil || !payload.After.Healthy {
		t.Errorf("after report = %+v, want healthy", payload.After)
	}
}

// Warnings pass by default and fail under --strict.
func TestDoctorCmdStrictExitCode(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)
	// A timestamp the storage format forbids: reported, never rewritten.
	db := rawDB(t, cfg)
	if _, err := db.Exec(`UPDATE snippets SET updated_at = '2026/09/01 12:00:00' WHERE rowid = (SELECT MIN(rowid) FROM snippets)`); err != nil {
		t.Fatalf("age a timestamp: %v", err)
	}

	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{Only: []string{store.CheckTimestamps}}, &out)
	if err != nil {
		t.Fatalf("doctorCmd: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0 when only warnings are found\n%s", code, out.String())
	}

	out.Reset()
	code, err = doctorCmd(cfg, doctorCLIOptions{Only: []string{store.CheckTimestamps}, Strict: true}, &out)
	if err != nil {
		t.Fatalf("doctorCmd --strict: %v", err)
	}
	if code != 1 {
		t.Errorf("exit code with --strict = %d, want 1\n%s", code, out.String())
	}
}

// --reindex is the narrow form: index checks only, repaired.
func TestDoctorCmdReindex(t *testing.T) {
	cfg := testConfig(t)
	seedStore(t, cfg)
	corruptIndex(t, cfg)

	var out bytes.Buffer
	code, err := doctorCmd(cfg, doctorCLIOptions{JSON: true, Reindex: true}, &out)
	if err != nil {
		t.Fatalf("doctorCmd --reindex: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0 after reindex\n%s", code, out.String())
	}
	var payload doctorJSON
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	for _, c := range payload.Report.Checks {
		if c.Name != store.CheckFTS && c.Name != store.CheckFTSCount {
			t.Errorf("--reindex ran check %q; it must stay narrow", c.Name)
		}
	}
	if payload.Repair == nil || !payload.Repair.RebuiltFTS {
		t.Errorf("repair = %+v, want a rebuild", payload.Repair)
	}
}

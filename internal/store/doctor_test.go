package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// checkByName returns one check from the report, failing when it is absent.
func checkByName(t *testing.T, rep DoctorReport, name string) DoctorCheck {
	t.Helper()
	for _, c := range rep.Checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q missing from report (%d checks)", name, len(rep.Checks))
	return DoctorCheck{}
}

// ftsRowid is the surrogate rowid of a snippet.
func ftsRowid(t *testing.T, s *Store, id string) int64 {
	t.Helper()
	var rowid int64
	if err := s.db.QueryRow(`SELECT rowid FROM snippets WHERE id = ?`, id).Scan(&rowid); err != nil {
		t.Fatalf("rowid for %s: %v", id, err)
	}
	return rowid
}

// mustDoctor runs a full check and fails on an infrastructure error.
func mustDoctor(t *testing.T, s *Store) DoctorReport {
	t.Helper()
	rep, err := s.Doctor(DoctorOptions{})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	return rep
}

func TestDoctorHealthyStore(t *testing.T) {
	s, fc := newTestStore(t)
	folder, err := s.CreateFolder("Work", nil)
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, SnippetInput{
		Title: "restart caddy", Body: "sudo systemctl restart caddy",
		Language: "bash", Notes: "after Caddyfile changes",
		Tags: []string{"ops", "caddy"}, FolderID: strPtr(folder.ID),
	})
	secret := mustCreate(t, s, SnippetInput{
		Title: "api token", Body: "hunter2", IsSensitive: true,
		VarDefaults: map[string]string{"env": "prod"},
	})
	// A revision, a trashed snippet, and a tag-only-pruned row exercise the
	// paths the checks read.
	if _, err := s.ReplaceSnippet(secret.ID, SnippetInput{
		Title: "api token", Body: "hunter3", IsSensitive: true,
		VarDefaults: map[string]string{"env": "prod"},
	}); err != nil {
		t.Fatal(err)
	}
	trashed := mustCreate(t, s, SnippetInput{Title: "throwaway", Body: "x"})
	if err := s.SoftDeleteSnippet(trashed.ID); err != nil {
		t.Fatal(err)
	}
	_ = fc

	rep := mustDoctor(t, s)
	if !rep.Healthy {
		t.Fatalf("healthy store reported unhealthy: %+v", rep.Checks)
	}
	for _, c := range rep.Checks {
		if c.Status == StatusError {
			t.Errorf("check %s: unexpected error: %s", c.Name, c.Detail)
		}
	}
	if rep.SchemaVersion != rep.BinarySchema {
		t.Errorf("schema versions differ: db=%d binary=%d", rep.SchemaVersion, rep.BinarySchema)
	}
	// One live, one trashed, and the sensitive one replaced.
	if rep.Counts.Snippets != 2 || rep.Counts.Trashed != 1 {
		t.Errorf("counts: got %+v, want 2 live and 1 trashed", rep.Counts)
	}
	if rep.Counts.Folders != 1 || rep.Counts.Revisions != 1 {
		t.Errorf("counts: got %+v, want 1 folder and 1 revision", rep.Counts)
	}
}

// The index row count cannot come from the virtual table: it is
// external-content, so COUNT(*) there reads the content table and can
// never disagree. This is the case that proves the shadow-table count.
func TestDoctorDetectsMissingIndexRow(t *testing.T) {
	s, _ := newTestStore(t)
	sn := mustCreate(t, s, SnippetInput{Title: "alpha gateway", Body: "zebra crossing"})

	if _, err := s.db.Exec(`DELETE FROM snippets_fts WHERE rowid = ?`, ftsRowid(t, s, sn.ID)); err != nil {
		t.Fatal(err)
	}
	rep := mustDoctor(t, s)
	if rep.Healthy {
		t.Fatal("missing index row reported healthy")
	}
	if c := checkByName(t, rep, CheckFTSCount); c.Status != StatusError {
		t.Errorf("fts_count: got %s (%s), want error", c.Status, c.Detail)
	}

	res, err := s.Repair(DoctorOptions{Only: []string{CheckFTSCount}})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if !res.RebuiltFTS {
		t.Error("repair did not rebuild the index")
	}
	if rep = mustDoctor(t, s); !rep.Healthy {
		t.Errorf("store still unhealthy after repair: %+v", rep.Checks)
	}
	got, err := s.ListSnippets(ListFilter{Q: "alpha"})
	if err != nil || len(got) != 1 {
		t.Errorf("search after repair: got %d rows err=%v, want 1", len(got), err)
	}
}

// Stale content: the index row exists and the counts agree, so the count
// check passes, but the index holds terms the content no longer has. Only
// the content probe catches this.
func TestDoctorDetectsStaleIndexContent(t *testing.T) {
	s, _ := newTestStore(t)
	sn := mustCreate(t, s, SnippetInput{Title: "alpha gateway", Body: "zebra crossing"})

	// Rewrite the content column without touching the index, the way a
	// bypassed write would.
	if _, err := s.db.Exec(`UPDATE snippets SET title = 'quokka replacement' WHERE id = ?`, sn.ID); err != nil {
		t.Fatal(err)
	}

	rep := mustDoctor(t, s)
	if c := checkByName(t, rep, CheckFTSCount); c.Status != StatusOK {
		t.Errorf("fts_count: got %s (%s), want ok — this case has equal counts", c.Status, c.Detail)
	}
	if c := checkByName(t, rep, CheckFTS); c.Status != StatusError {
		t.Errorf("fts: got %s (%s), want error — stale terms are not detected", c.Status, c.Detail)
	}

	if _, err := s.Repair(DoctorOptions{}); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if rep = mustDoctor(t, s); !rep.Healthy {
		t.Errorf("store still unhealthy after repair: %+v", rep.Checks)
	}
	// The old title term must be gone and the new one present.
	if got, _ := s.ListSnippets(ListFilter{Q: "alpha"}); len(got) != 0 {
		t.Errorf("stale term 'alpha' still matches after repair: %d rows", len(got))
	}
	if got, _ := s.ListSnippets(ListFilter{Q: "quokka"}); len(got) != 1 {
		t.Errorf("term 'quokka' does not match after repair: %d rows", len(got))
	}
}

// A plaintext leak must be cleared before the rebuild, because rebuild
// reads the content table and would otherwise index the leaked body.
func TestDoctorClearsSensitiveMirrorsBeforeRebuild(t *testing.T) {
	s, _ := newTestStore(t)
	secret := mustCreate(t, s, SnippetInput{
		Title: "api token", Body: "hunter2", IsSensitive: true,
		VarDefaults: map[string]string{"env": "prod"},
	})
	if _, err := s.db.Exec(
		`UPDATE snippets SET body_text = 'leakedsecret', var_defaults = '{"env":"prod"}' WHERE id = ?`,
		secret.ID); err != nil {
		t.Fatal(err)
	}

	rep := mustDoctor(t, s)
	if c := checkByName(t, rep, CheckSensitive); c.Status != StatusError || c.Rows != 1 {
		t.Errorf("sensitive: got %s rows=%d (%s), want error with 1 row", c.Status, c.Rows, c.Detail)
	}

	res, err := s.Repair(DoctorOptions{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ClearedSensitiveMirrors != 1 {
		t.Errorf("ClearedSensitiveMirrors = %d, want 1", res.ClearedSensitiveMirrors)
	}
	if rep = mustDoctor(t, s); !rep.Healthy {
		t.Errorf("store still unhealthy after repair: %+v", rep.Checks)
	}
	// The ordering test: had the rebuild run first, the leaked term would
	// now be in the index.
	if got, _ := s.ListSnippets(ListFilter{Q: "leakedsecret"}); len(got) != 0 {
		t.Errorf("leaked body term is searchable after repair: %d rows", len(got))
	}
	// The sealed copy must survive, so the defaults still load.
	got, err := s.GetSnippet(secret.ID)
	if err != nil {
		t.Fatalf("GetSnippet: %v", err)
	}
	if got.VarDefaults["env"] != "prod" {
		t.Errorf("sealed defaults lost: %+v", got.VarDefaults)
	}
}

// A leak that is already in the index must be rebuilt away, not just
// cleared from the mirror: clearing alone leaves the term searchable.
func TestRepairOfSensitiveLeakAlsoRebuildsIndex(t *testing.T) {
	s, _ := newTestStore(t)
	secret := mustCreate(t, s, SnippetInput{
		Title: "api token", Body: "hunter2", IsSensitive: true,
	})
	// Leak the plaintext into the mirror and index it, which is the state
	// a rebuild performed while the leak existed would leave behind.
	if _, err := s.db.Exec(`UPDATE snippets SET body_text = 'leakedsecret' WHERE id = ?`, secret.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO snippets_fts(snippets_fts) VALUES('rebuild')`); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.ListSnippets(ListFilter{Q: "leakedsecret"}); len(got) != 1 {
		t.Fatalf("precondition: leaked term not indexed (%d rows)", len(got))
	}

	// Repair the sensitive check alone; the rebuild is still required.
	res, err := s.Repair(DoctorOptions{Only: []string{CheckSensitive}})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ClearedSensitiveMirrors != 1 {
		t.Errorf("ClearedSensitiveMirrors = %d, want 1", res.ClearedSensitiveMirrors)
	}
	if !res.RebuiltFTS {
		t.Error("clearing a leak must also rebuild the index, or the term stays searchable")
	}
	if got, _ := s.ListSnippets(ListFilter{Q: "leakedsecret"}); len(got) != 0 {
		t.Errorf("leaked term still searchable after repair: %d rows", len(got))
	}
}

func TestDoctorResyncsTagMirror(t *testing.T) {
	s, _ := newTestStore(t)
	sn := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "make deploy", Tags: []string{"ops", "caddy"},
	})
	if _, err := s.db.Exec(`UPDATE snippets SET tags = 'ops' WHERE id = ?`, sn.ID); err != nil {
		t.Fatal(err)
	}

	rep := mustDoctor(t, s)
	if c := checkByName(t, rep, CheckTags); c.Status != StatusWarn || c.Rows != 1 {
		t.Errorf("tags: got %s rows=%d (%s), want warn with 1 row", c.Status, c.Rows, c.Detail)
	}
	res, err := s.Repair(DoctorOptions{})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ResyncedTagMirrors != 1 {
		t.Errorf("ResyncedTagMirrors = %d, want 1", res.ResyncedTagMirrors)
	}
	var mirror string
	if err := s.db.QueryRow(`SELECT tags FROM snippets WHERE id = ?`, sn.ID).Scan(&mirror); err != nil {
		t.Fatal(err)
	}
	if mirror != "caddy ops" {
		t.Errorf("mirror = %q, want %q (sorted by name)", mirror, "caddy ops")
	}
	if rep = mustDoctor(t, s); !rep.Healthy {
		t.Errorf("store unhealthy after repair: %+v", rep.Checks)
	}
}

// Rebuilding the index normalizes the mirror it reads even when only fts
// is selected, or the rebuild would bake the stale mirror in.
func TestRepairOfFTSAloneAlsoNormalizesMirrors(t *testing.T) {
	s, _ := newTestStore(t)
	sn := mustCreate(t, s, SnippetInput{
		Title: "deploy", Body: "make deploy", Tags: []string{"ops", "caddy"},
	})
	if _, err := s.db.Exec(`UPDATE snippets SET tags = 'ops' WHERE id = ?`, sn.ID); err != nil {
		t.Fatal(err)
	}
	res, err := s.Repair(DoctorOptions{Only: []string{CheckFTS}})
	if err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if res.ResyncedTagMirrors != 1 {
		t.Errorf("ResyncedTagMirrors = %d, want 1: a rebuild must normalize the tags mirror it reads", res.ResyncedTagMirrors)
	}
	if rep := mustDoctor(t, s); !rep.Healthy {
		t.Errorf("store unhealthy after repair: %+v", rep.Checks)
	}
}

// The B2 class: a live folder under a deleted parent, which makes Purge's
// foreign keys fail and stops trash draining. Repair must not touch it
// implicitly; only the explicit call may.
func TestDoctorDetectsOrphans(t *testing.T) {
	s, fc := newTestStore(t)
	parent, err := s.CreateFolder("Work", nil)
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.CreateFolder("bash", strPtr(parent.ID))
	if err != nil {
		t.Fatal(err)
	}
	mustCreate(t, s, SnippetInput{Title: "in child", Body: "x", FolderID: strPtr(child.ID)})

	// Soft-delete the parent directly, leaving a live child beneath it.
	if _, err := s.db.Exec(`UPDATE folders SET deleted_at = ? WHERE id = ?`, ts(fc.t), parent.ID); err != nil {
		t.Fatal(err)
	}

	rep := mustDoctor(t, s)
	if c := checkByName(t, rep, CheckOrphans); c.Status != StatusError || c.Rows == 0 {
		t.Errorf("orphans: got %s rows=%d (%s), want error with rows", c.Status, c.Rows, c.Detail)
	}

	// Repair has nothing to offer here: the fix changes real data.
	if _, err := s.Repair(DoctorOptions{Only: []string{CheckOrphans}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Repair(orphans) error = %v, want ErrInvalid", err)
	}
	if _, err := s.Repair(DoctorOptions{}); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	if c := checkByName(t, mustDoctor(t, s), CheckOrphans); c.Status != StatusError {
		t.Errorf("orphans: got %s, want error — Repair must not fix orphans implicitly", c.Status)
	}

	changed, err := s.NullOrphanFolderRefs()
	if err != nil {
		t.Fatalf("NullOrphanFolderRefs: %v", err)
	}
	if changed != 1 {
		t.Errorf("changed = %d, want 1", changed)
	}
	if c := checkByName(t, mustDoctor(t, s), CheckOrphans); c.Status != StatusOK {
		t.Errorf("orphans: got %s (%s), want ok after the explicit fix", c.Status, c.Detail)
	}
}

func TestDoctorTimestampsAreReportedNotRewritten(t *testing.T) {
	s, _ := newTestStore(t)
	sn := mustCreate(t, s, SnippetInput{Title: "bad stamp", Body: "x"})
	if _, err := s.db.Exec(`UPDATE snippets SET updated_at = '2021-01-01 00:00:00' WHERE id = ?`, sn.ID); err != nil {
		t.Fatal(err)
	}
	rep := mustDoctor(t, s)
	if c := checkByName(t, rep, CheckTimestamps); c.Status != StatusWarn || c.Rows != 1 {
		t.Errorf("timestamps: got %s rows=%d (%s), want warn with 1 row", c.Status, c.Rows, c.Detail)
	}
	if _, err := s.Repair(DoctorOptions{}); err != nil {
		t.Fatalf("Repair: %v", err)
	}
	var stamp string
	if err := s.db.QueryRow(`SELECT updated_at FROM snippets WHERE id = ?`, sn.ID).Scan(&stamp); err != nil {
		t.Fatal(err)
	}
	if stamp != "2021-01-01 00:00:00" {
		t.Errorf("Repair rewrote a timestamp to %q; it must only report these", stamp)
	}
}

func TestDoctorSchemaNewerThanBinary(t *testing.T) {
	s, _ := newTestStore(t)
	binary, err := maxMigrationVersion()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (?)`, binary+1); err != nil {
		t.Fatal(err)
	}
	rep := mustDoctor(t, s)
	c := checkByName(t, rep, CheckSchema)
	if c.Status != StatusError {
		t.Errorf("schema: got %s (%s), want error", c.Status, c.Detail)
	}
	if rep.Healthy {
		t.Error("a database from a newer binary must not report healthy")
	}
}

func TestDoctorRunsWithoutKey(t *testing.T) {
	// A store with no key attached at all: doctor must still complete, so
	// it works when the key file is missing or wrong.
	path := filepath.Join(t.TempDir(), "nokey.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.SetClock(&fakeClock{t: baseTime})
	mustCreate(t, s, SnippetInput{Title: "no key needed", Body: "plain"})

	rep, err := s.Doctor(DoctorOptions{KeyPath: filepath.Join(t.TempDir(), "absent-key")})
	if err != nil {
		t.Fatalf("Doctor without a key: %v", err)
	}
	if c := checkByName(t, rep, CheckKey); c.Status != StatusWarn {
		t.Errorf("key: got %s (%s), want warn for a missing file", c.Status, c.Detail)
	}
	if !rep.Healthy {
		t.Errorf("store with no key reported unhealthy: %+v", rep.Checks)
	}
}

func TestDoctorSelection(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "one", Body: "x"})

	rep, err := s.Doctor(DoctorOptions{Only: []string{CheckFTSCount}})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Name != CheckFTSCount {
		t.Errorf("Only selection returned %+v, want just %s", rep.Checks, CheckFTSCount)
	}
	// Comma-separated values are accepted, as the CLI passes one string.
	rep, err = s.Doctor(DoctorOptions{Only: []string{"fts, fts_count"}})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	if len(rep.Checks) != 2 {
		t.Errorf("comma selection returned %d checks, want 2", len(rep.Checks))
	}
	for _, bad := range [][]string{{"nonsense"}, {""}, {"  "}} {
		if _, err := s.Doctor(DoctorOptions{Only: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("Only=%q error = %v, want ErrInvalid", bad, err)
		}
	}
}

func TestDoctorKeyCheck(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name   string
		path   string
		perm   os.FileMode
		status DoctorStatus
	}{
		{"empty path", "", 0, StatusWarn},
		{"missing file", filepath.Join(dir, "absent"), 0, StatusWarn},
		{"short file", filepath.Join(dir, "short"), 0o600, StatusError},
		{"world readable", filepath.Join(dir, "loose"), 0o644, StatusWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if tc.perm != 0 {
				size := 32
				if tc.name == "short file" {
					size = 8
				}
				if err := os.WriteFile(path, make([]byte, size), tc.perm); err != nil {
					t.Fatal(err)
				}
			}
			if got := keyCheck(path); got.Status != tc.status {
				t.Errorf("keyCheck(%s) = %s (%s), want %s", path, got.Status, got.Detail, tc.status)
			}
		})
	}

	// A correct key file in a private directory is the only ok outcome.
	// t.TempDir() is not 0700, so make the directory the way snp does.
	privateDir := filepath.Join(t.TempDir(), "state")
	if err := os.Mkdir(privateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(privateDir, "key")
	if err := os.WriteFile(good, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if c := keyCheck(good); c.Status != StatusOK {
		t.Errorf("keyCheck(good) = %s (%s), want ok", c.Status, c.Detail)
	}
}

func TestRepairIsIdempotent(t *testing.T) {
	s, _ := newTestStore(t)
	mustCreate(t, s, SnippetInput{Title: "alpha gateway", Body: "zebra crossing", Tags: []string{"ops"}})
	if _, err := s.Repair(DoctorOptions{}); err != nil {
		t.Fatalf("first Repair: %v", err)
	}
	res, err := s.Repair(DoctorOptions{})
	if err != nil {
		t.Fatalf("second Repair: %v", err)
	}
	if res.ClearedSensitiveMirrors != 0 || res.ResyncedTagMirrors != 0 {
		t.Errorf("second repair changed rows: %+v", res)
	}
	if rep := mustDoctor(t, s); !rep.Healthy {
		t.Errorf("store unhealthy: %+v", rep.Checks)
	}
}

func TestProbeToken(t *testing.T) {
	cases := []struct{ in, want string }{
		{"alpha gateway", "gateway"},
		{"ab cd", ""},
		{"sudo systemctl restart caddy", "systemctl"},
		{"", ""},
		{"!!!", ""},
		{"café résumé", "caf"},   // accent splits the run; ASCII only
		{"UPPER lower", "upper"}, // lowercased
		{"a1b2c3", "a1b2c3"},
	}
	for _, tc := range cases {
		if got := probeToken(tc.in); got != tc.want {
			t.Errorf("probeToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	// A run longer than 64 characters is skipped, never truncated: a
	// prefix is not a token and would never match.
	long := make([]byte, 80)
	for i := range long {
		long[i] = 'a'
	}
	if got := probeToken(string(long)); got != "" {
		t.Errorf("probeToken(long) = %q, want empty", got)
	}
}

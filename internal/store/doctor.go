package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Health check and index repair (spec "Health check and index repair").
//
// The FTS5 index is the one structure snp can rebuild from its own
// tables, and the write-ordering rules in spec §4 exist because it has
// corrupted before. Doctor is the counterpart to those rules: it names
// what is wrong, and it can repair the parts that are derived rather than
// authoritative.

// DoctorStatus is the outcome of a single check.
type DoctorStatus string

const (
	StatusOK    DoctorStatus = "ok"
	StatusWarn  DoctorStatus = "warn"
	StatusError DoctorStatus = "error"
)

// Check names. Each doubles as a value for --only.
const (
	CheckSQLite     = "sqlite"
	CheckFTS        = "fts"
	CheckFTSCount   = "fts_count"
	CheckTags       = "tags"
	CheckSensitive  = "sensitive"
	CheckOrphans    = "orphans"
	CheckTimestamps = "timestamps"
	CheckSchema     = "schema"
	CheckKey        = "key"
)

// doctorChecks is every check, in report order.
var doctorChecks = []string{
	CheckSQLite, CheckFTS, CheckFTSCount, CheckTags, CheckSensitive,
	CheckOrphans, CheckTimestamps, CheckSchema, CheckKey,
}

// ValidDoctorCheck reports whether name is a check this build knows, so a
// caller can reject a bad --only before opening the database.
func ValidDoctorCheck(name string) bool {
	for _, c := range doctorChecks {
		if c == name {
			return true
		}
	}
	return false
}

// DoctorCheckNames lists every check, in report order, for help output.
func DoctorCheckNames() []string {
	out := make([]string, len(doctorChecks))
	copy(out, doctorChecks)
	return out
}

// DoctorCheck is one named check and its result. Rows carries the
// affected row count where the check has one.
type DoctorCheck struct {
	Name       string       `json:"name"`
	Status     DoctorStatus `json:"status"`
	Detail     string       `json:"detail,omitempty"`
	Repairable bool         `json:"repairable"`
	Rows       int64        `json:"rows,omitempty"`
}

// DoctorCounts summarizes the store.
type DoctorCounts struct {
	Snippets  int64 `json:"snippets"`
	Trashed   int64 `json:"trashed"`
	Folders   int64 `json:"folders"`
	Tags      int64 `json:"tags"`
	Revisions int64 `json:"revisions"`
}

// DoctorReport is a complete health report. Healthy is false when any
// check returned StatusError; warnings do not make a store unhealthy.
type DoctorReport struct {
	Healthy       bool          `json:"healthy"`
	CheckedAt     string        `json:"checked_at"`
	SchemaVersion int           `json:"schema_version"`
	BinarySchema  int           `json:"binary_schema"`
	Counts        DoctorCounts  `json:"counts"`
	Checks        []DoctorCheck `json:"checks"`
}

// DoctorOptions selects what a run or a repair touches.
type DoctorOptions struct {
	// Only restricts the run to the named checks; empty means all.
	Only []string
	// Full runs PRAGMA integrity_check instead of the faster quick_check.
	Full bool
	// KeyPath is the key file to inspect. Empty skips the key check.
	KeyPath string
}

// RepairResult reports what a repair changed.
type RepairResult struct {
	ClearedSensitiveMirrors int64 `json:"cleared_sensitive_mirrors"`
	ResyncedTagMirrors      int64 `json:"resynced_tag_mirrors"`
	RebuiltFTS              bool  `json:"rebuilt_fts"`
}

// timestampGlob matches the stored format exactly: RFC3339 UTC, second
// precision, always Z (spec §4). GLOB must match the whole string, so a
// value with trailing characters fails too. Sync and ordering compare
// these lexicographically, so the format is the invariant that matters.
const timestampGlob = "[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z"

// tagMirrorMismatch is a WHERE predicate over the snippets table, true
// when the denormalized snippets.tags mirror disagrees with the
// authoritative snippet_tags rows. The mirror is the column the FTS
// index reads back, so a disagreement means wrong search results.
//
// Tag names cannot contain spaces (spec §4), so "the mirror's token count
// matches the row count" plus "every authoritative name appears as a
// mirror token" is exactly set equality: no name can produce two tokens,
// snippet_tags is keyed on (snippet_id, tag_id), and tags.name is UNIQUE,
// so a name cannot repeat within a snippet.
const tagMirrorMismatch = `(
		(CASE WHEN TRIM(snippets.tags) = '' THEN 0
		      ELSE LENGTH(TRIM(snippets.tags)) - LENGTH(REPLACE(TRIM(snippets.tags), ' ', '')) + 1 END)
		<> (SELECT COUNT(*) FROM snippet_tags st WHERE st.snippet_id = snippets.id)
		OR EXISTS (SELECT 1 FROM snippet_tags st JOIN tags tg ON tg.id = st.tag_id
		           WHERE st.snippet_id = snippets.id
		             AND INSTR(' ' || snippets.tags || ' ', ' ' || tg.name || ' ') = 0)
	)`

// Doctor runs the selected checks and returns the report. A failing
// check is a finding, not an error; an error means the check could not be
// run at all.
func (s *Store) Doctor(opts DoctorOptions) (DoctorReport, error) {
	ctx := context.Background()
	want, err := doctorSelection(opts.Only)
	if err != nil {
		return DoctorReport{}, err
	}
	report := DoctorReport{CheckedAt: s.now(), Checks: []DoctorCheck{}}

	current, binary, err := s.schemaVersions(ctx)
	if err != nil {
		return DoctorReport{}, err
	}
	report.SchemaVersion, report.BinarySchema = current, binary
	if report.Counts, err = s.doctorCounts(ctx); err != nil {
		return DoctorReport{}, err
	}

	for _, name := range doctorChecks {
		if !want[name] {
			continue
		}
		var (
			check DoctorCheck
			err   error
		)
		switch name {
		case CheckSQLite:
			check, err = s.checkSQLite(ctx, opts.Full)
		case CheckFTS:
			check, err = s.checkFTS(ctx)
		case CheckFTSCount:
			check, err = s.checkFTSCount(ctx)
		case CheckTags:
			check, err = s.checkTags(ctx)
		case CheckSensitive:
			check, err = s.checkSensitive(ctx)
		case CheckOrphans:
			check, err = s.checkOrphans(ctx)
		case CheckTimestamps:
			check, err = s.checkTimestamps(ctx)
		case CheckSchema:
			check = schemaCheck(current, binary)
		case CheckKey:
			check = keyCheck(opts.KeyPath)
		}
		if err != nil {
			return DoctorReport{}, fmt.Errorf("doctor: %s check: %w", name, err)
		}
		report.Checks = append(report.Checks, check)
	}

	report.Healthy = true
	for _, c := range report.Checks {
		if c.Status == StatusError {
			report.Healthy = false
		}
	}
	return report, nil
}

// Repair applies the derived, recomputable repairs and reports what
// changed. It never touches snippet content: only the FTS index and the
// plaintext mirror columns that feed it.
//
// The two rules that keep this safe both come from the index being built
// out of the mirror columns:
//
//   - Rebuilding always normalizes both mirrors first, even when Only
//     selects fts alone, so a rebuild cannot bake a leaked body_text or a
//     stale tags mirror into the index.
//   - Any mirror change forces a rebuild, even when Only selects the
//     mirror's own check. Clearing a leaked body_text is not enough on its
//     own: the leaked term may already be in the index, where it stays
//     searchable until the index is rebuilt from the cleaned column.
func (s *Store) Repair(opts DoctorOptions) (RepairResult, error) {
	ctx := context.Background()
	want, err := doctorSelection(opts.Only)
	if err != nil {
		return RepairResult{}, err
	}
	rebuild := want[CheckFTS] || want[CheckFTSCount]
	// Each mirror repair is a check's own repair and a precondition of any
	// rebuild.
	clearSensitive := want[CheckSensitive] || rebuild
	resyncTags := want[CheckTags] || rebuild
	if !clearSensitive && !resyncTags {
		return RepairResult{}, fmt.Errorf("%w: the selected checks have nothing doctor can repair", ErrInvalid)
	}

	var res RepairResult
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()

	if clearSensitive {
		if res.ClearedSensitiveMirrors, err = clearSensitiveMirrorsTx(ctx, tx); err != nil {
			return RepairResult{}, err
		}
	}
	if resyncTags {
		if res.ResyncedTagMirrors, err = s.resyncTagMirrorsTx(ctx, tx); err != nil {
			return RepairResult{}, err
		}
	}
	// A mirror that changed invalidates the index built from it.
	if rebuild || res.ClearedSensitiveMirrors > 0 || res.ResyncedTagMirrors > 0 {
		if _, err := tx.ExecContext(ctx, `INSERT INTO snippets_fts(snippets_fts) VALUES('rebuild')`); err != nil {
			return RepairResult{}, fmt.Errorf("doctor: FTS rebuild: %w", err)
		}
		res.RebuiltFTS = true
	}
	if err := tx.Commit(); err != nil {
		return RepairResult{}, err
	}
	return res, nil
}

// NullOrphanFolderRefs clears folder references on live rows that point
// at a deleted or missing folder, the state that blocks Purge forever.
// It is deliberately not part of Repair: it changes real data, so the CLI
// requires an explicit flag for it. Returns the number of rows changed.
func (s *Store) NullOrphanFolderRefs() (int64, error) {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var changed int64
	res, err := tx.ExecContext(ctx, `
		UPDATE snippets SET folder_id = NULL
		WHERE deleted_at IS NULL AND folder_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM folders f WHERE f.id = snippets.folder_id AND f.deleted_at IS NULL)`)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	changed += n
	res, err = tx.ExecContext(ctx, `
		UPDATE folders SET parent_id = NULL
		WHERE deleted_at IS NULL AND parent_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM folders p WHERE p.id = folders.parent_id AND p.deleted_at IS NULL)`)
	if err != nil {
		return 0, err
	}
	if n, err = res.RowsAffected(); err != nil {
		return 0, err
	}
	changed += n
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return changed, nil
}

// doctorSelection resolves an --only list into a set of check names.
func doctorSelection(only []string) (map[string]bool, error) {
	want := map[string]bool{}
	if len(only) == 0 {
		for _, n := range doctorChecks {
			want[n] = true
		}
		return want, nil
	}
	for _, raw := range only {
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if !ValidDoctorCheck(name) {
				return nil, fmt.Errorf("%w: unknown check %q", ErrInvalid, name)
			}
			want[name] = true
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("%w: no checks selected", ErrInvalid)
	}
	return want, nil
}

// schemaVersions returns the database's schema version and the highest
// migration this binary embeds.
func (s *Store) schemaVersions(ctx context.Context) (int, int, error) {
	var current int
	if err := s.db.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(version), 0) FROM schema_version`).Scan(&current); err != nil {
		return 0, 0, err
	}
	binary, err := maxMigrationVersion()
	if err != nil {
		return 0, 0, err
	}
	return current, binary, nil
}

// maxMigrationVersion is the highest embedded migration number.
func maxMigrationVersion() (int, error) {
	entries, err := migrationFS.ReadDir("migrations")
	if err != nil {
		return 0, err
	}
	max := 0
	for _, e := range entries {
		m := migrationRe.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		if v, err := strconv.Atoi(m[1]); err == nil && v > max {
			max = v
		}
	}
	return max, nil
}

func (s *Store) doctorCounts(ctx context.Context) (DoctorCounts, error) {
	var c DoctorCounts
	err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM snippets WHERE deleted_at IS NULL),
		       (SELECT COUNT(*) FROM snippets WHERE deleted_at IS NOT NULL),
		       (SELECT COUNT(*) FROM folders WHERE deleted_at IS NULL),
		       (SELECT COUNT(*) FROM tags),
		       (SELECT COUNT(*) FROM snippet_revisions)`).
		Scan(&c.Snippets, &c.Trashed, &c.Folders, &c.Tags, &c.Revisions)
	return c, err
}

// checkSQLite runs the file-level integrity pragma. integrity_check can
// report several problems, so the rows are collected rather than taking
// the first.
func (s *Store) checkSQLite(ctx context.Context, full bool) (DoctorCheck, error) {
	pragma := "quick_check"
	if full {
		pragma = "integrity_check"
	}
	c := DoctorCheck{Name: CheckSQLite}
	rows, err := s.db.QueryContext(ctx, "PRAGMA "+pragma)
	if err != nil {
		return c, err
	}
	defer rows.Close()
	var problems []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return c, err
		}
		if line != "ok" {
			problems = append(problems, line)
		}
	}
	if err := rows.Err(); err != nil {
		return c, err
	}
	if len(problems) > 0 {
		c.Status = StatusError
		c.Rows = int64(len(problems))
		c.Detail = fmt.Sprintf("%s reported %d problem(s): %s", pragma, len(problems), joinLimited(problems, 3))
		return c, nil
	}
	c.Status = StatusOK
	c.Detail = pragma + " ok"
	return c, nil
}

// doctorProbeLimit bounds the per-snippet content probe. Each probed
// snippet costs one or two queries; the probe is a tripwire, and the
// count check is what finds missing or extra index rows.
const doctorProbeLimit = 500

// checkFTS probes the index itself, in two parts.
//
// FTS5's integrity-check verifies the index's internal structure: it
// catches the damage that surfaces as "database disk image is malformed".
// It does not compare the index against the content table, so a row whose
// content was rewritten without touching the index keeps passing it while
// searches still return the old terms. The second probe covers that: it
// asks the index for a token taken from the row's current title and
// body_text, restricted to that column, so an occurrence in another
// column cannot mask a stale one. A miss means the index holds terms the
// content no longer has, which rebuild cures.
func (s *Store) checkFTS(ctx context.Context) (DoctorCheck, error) {
	c := DoctorCheck{Name: CheckFTS, Repairable: true}
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO snippets_fts(snippets_fts) VALUES('integrity-check')`); err != nil {
		c.Status = StatusError
		c.Detail = "FTS5 integrity-check failed: " + err.Error()
		return c, nil
	}
	probe, err := s.probeFTSContent(ctx)
	if err != nil {
		return c, err
	}
	if probe.Err != nil {
		c.Status = StatusError
		c.Detail = "the index could not be queried: " + probe.Err.Error()
		return c, nil
	}
	if len(probe.Misses) > 0 {
		c.Status = StatusError
		c.Rows = int64(len(probe.Misses))
		c.Detail = fmt.Sprintf("indexed terms do not match the content table for %d column(s) (%s); search returns stale results",
			len(probe.Misses), joinLimited(probe.Misses, 3))
		return c, nil
	}
	c.Status = StatusOK
	c.Detail = fmt.Sprintf("integrity-check ok; content probe matched %d of %d snippets", probe.Probed, probe.Total)
	return c, nil
}

// ftsProbe is the outcome of the content probe. Err is set when the index
// cannot be queried at all, which is itself a finding rather than a
// failure of the check.
type ftsProbe struct {
	Misses []string
	Probed int
	Total  int
	Err    error
}

// probeFTSContent checks, for up to doctorProbeLimit snippets, that a
// distinctive token from the row's current title and body_text is
// actually present in that column of that row's index entry.
func (s *Store) probeFTSContent(ctx context.Context) (ftsProbe, error) {
	var res ftsProbe
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets`).Scan(&res.Total); err != nil {
		return res, err
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, rowid, title, body_text FROM snippets ORDER BY rowid LIMIT ?`, doctorProbeLimit)
	if err != nil {
		return res, err
	}
	type probe struct {
		id    string
		rowid int64
		col   string
		token string
	}
	var probes []probe
	for rows.Next() {
		var (
			id, title, body string
			rowid           int64
		)
		if err := rows.Scan(&id, &rowid, &title, &body); err != nil {
			rows.Close()
			return res, err
		}
		if tok := probeToken(title); tok != "" {
			probes = append(probes, probe{id: id, rowid: rowid, col: "title", token: tok})
		}
		if tok := probeToken(body); tok != "" {
			probes = append(probes, probe{id: id, rowid: rowid, col: "body_text", token: tok})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return res, err
	}
	rows.Close()

	seen := map[string]bool{}
	for _, p := range probes {
		if !seen[p.id] {
			seen[p.id] = true
			res.Probed++
		}
		expr := p.col + `:"` + p.token + `"`
		var n int64
		if err := s.db.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM snippets_fts WHERE snippets_fts MATCH ? AND rowid = ?`,
			expr, p.rowid).Scan(&n); err != nil {
			res.Err = err
			return res, nil
		}
		if n == 0 {
			res.Misses = append(res.Misses, p.id+"/"+p.col)
		}
	}
	return res, nil
}

// probeToken picks the longest ASCII alphanumeric run of 3 to 64
// characters, lowercased. ASCII-only keeps the probe clear of unicode61's
// diacritic folding, and an alphanumeric run is exactly what unicode61
// indexes as one token, so a healthy index always matches it. Runs longer
// than 64 characters are ignored rather than truncated: a truncated token
// is a prefix, not a token, and would never match.
func probeToken(text string) string {
	best := ""
	var cur strings.Builder
	flush := func() {
		if n := cur.Len(); n >= 3 && n <= 64 && n > len(best) {
			best = cur.String()
		}
		cur.Reset()
	}
	for _, r := range strings.ToLower(text) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	return best
}

// checkFTSCount asserts the schema's invariant that there is exactly one
// index row per snippet row, live or soft-deleted: soft delete leaves the
// index row alone, restore reuses it, and purge removes both together.
//
// The index's own row count comes from the FTS5 shadow table, one row per
// indexed document. Counting the virtual table instead would be useless:
// it is external-content, so COUNT(*) there reads the content table and
// can never disagree with snippets.
func (s *Store) checkFTSCount(ctx context.Context) (DoctorCheck, error) {
	c := DoctorCheck{Name: CheckFTSCount, Repairable: true}
	var snippets, indexed int64
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets`).Scan(&snippets); err != nil {
		return c, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM snippets_fts_docsize`).Scan(&indexed); err != nil {
		c.Status = StatusWarn
		c.Detail = "cannot read the FTS5 index row count: " + err.Error()
		return c, nil
	}
	if snippets != indexed {
		c.Status = StatusError
		c.Rows = snippets - indexed
		c.Detail = fmt.Sprintf("snippets=%d but the index holds %d rows (expected one index row per snippet, live or trashed)",
			snippets, indexed)
		return c, nil
	}
	c.Status = StatusOK
	c.Detail = fmt.Sprintf("%d index rows for %d snippets", indexed, snippets)
	return c, nil
}

func (s *Store) checkTags(ctx context.Context) (DoctorCheck, error) {
	c := DoctorCheck{Name: CheckTags, Repairable: true}
	var n int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM snippets WHERE `+tagMirrorMismatch).Scan(&n); err != nil {
		return c, err
	}
	if n > 0 {
		c.Status = StatusWarn
		c.Rows = n
		c.Detail = fmt.Sprintf("%d snippet(s) disagree with snippet_tags; the mirror is what the index reads", n)
		return c, nil
	}
	c.Status = StatusOK
	c.Detail = "tags mirror agrees with snippet_tags"
	return c, nil
}

// checkSensitive reports plaintext leaking into the mirrors that feed the
// index. A sensitive row's body_text and var_defaults must both be empty
// (normal writes guarantee it), so a non-empty value means the body or its
// defaults are searchable and readable in plaintext on disk.
func (s *Store) checkSensitive(ctx context.Context) (DoctorCheck, error) {
	c := DoctorCheck{Name: CheckSensitive}
	var leaks, leaksWithoutSealed, staleSealed int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM snippets WHERE is_sensitive = 1 AND (body_text <> '' OR var_defaults <> '')),
		       (SELECT COUNT(*) FROM snippets WHERE is_sensitive = 1 AND var_defaults <> '' AND var_defaults_enc IS NULL),
		       (SELECT COUNT(*) FROM snippets WHERE is_sensitive = 0 AND var_defaults_enc IS NOT NULL)`).
		Scan(&leaks, &leaksWithoutSealed, &staleSealed); err != nil {
		return c, err
	}
	switch {
	case leaks > 0:
		c.Status = StatusError
		c.Repairable = true
		c.Rows = leaks
		detail := fmt.Sprintf("%d sensitive snippet(s) have plaintext body_text or var_defaults mirrors", leaks)
		if leaksWithoutSealed > 0 {
			detail += fmt.Sprintf("; %d of them have no sealed defaults copy, so repairing drops those defaults", leaksWithoutSealed)
		}
		c.Detail = detail
	case staleSealed > 0:
		// Ciphertext left behind on a row that is no longer sensitive. Not
		// a plaintext leak, but stale state a normal write would not leave.
		c.Status = StatusWarn
		c.Rows = staleSealed
		c.Detail = fmt.Sprintf("%d non-sensitive snippet(s) still carry a sealed var_defaults copy", staleSealed)
	default:
		c.Status = StatusOK
		c.Detail = "no plaintext mirrors on sensitive rows"
	}
	return c, nil
}

// checkOrphans finds live rows pointing at a deleted or missing parent.
// These are the states that make Purge's foreign keys fail, which rolls
// back the whole purge and stops trash draining every day.
func (s *Store) checkOrphans(ctx context.Context) (DoctorCheck, error) {
	c := DoctorCheck{Name: CheckOrphans}
	var snippets, folders int64
	if err := s.db.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM snippets s WHERE s.deleted_at IS NULL AND s.folder_id IS NOT NULL
		          AND NOT EXISTS (SELECT 1 FROM folders f WHERE f.id = s.folder_id AND f.deleted_at IS NULL)),
		       (SELECT COUNT(*) FROM folders f WHERE f.deleted_at IS NULL AND f.parent_id IS NOT NULL
		          AND NOT EXISTS (SELECT 1 FROM folders p WHERE p.id = f.parent_id AND p.deleted_at IS NULL))`).
		Scan(&snippets, &folders); err != nil {
		return c, err
	}
	if total := snippets + folders; total > 0 {
		c.Status = StatusError
		c.Rows = total
		c.Detail = fmt.Sprintf("%d live snippet(s) and %d live folder(s) point at a deleted or missing parent; this blocks purge (repair with --fix-orphans)",
			snippets, folders)
		return c, nil
	}
	c.Status = StatusOK
	c.Detail = "no live row points at a deleted parent"
	return c, nil
}

func (s *Store) checkTimestamps(ctx context.Context) (DoctorCheck, error) {
	c := DoctorCheck{Name: CheckTimestamps}
	queries := []struct {
		table string
		query string
	}{
		{"snippets", `SELECT COUNT(*) FROM snippets
			WHERE created_at NOT GLOB ? OR updated_at NOT GLOB ?
			   OR (deleted_at IS NOT NULL AND deleted_at NOT GLOB ?)`},
		{"folders", `SELECT COUNT(*) FROM folders
			WHERE created_at NOT GLOB ? OR updated_at NOT GLOB ?
			   OR (deleted_at IS NOT NULL AND deleted_at NOT GLOB ?)`},
		{"revisions", `SELECT COUNT(*) FROM snippet_revisions
			WHERE saved_at NOT GLOB ? OR version_at NOT GLOB ?`},
	}
	var (
		total int64
		parts []string
	)
	for _, q := range queries {
		args := make([]any, strings.Count(q.query, "?"))
		for i := range args {
			args[i] = timestampGlob
		}
		var n int64
		if err := s.db.QueryRowContext(ctx, q.query, args...).Scan(&n); err != nil {
			return c, err
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", q.table, n))
			total += n
		}
	}
	if total > 0 {
		c.Status = StatusWarn
		c.Rows = total
		c.Detail = fmt.Sprintf("not RFC3339 UTC seconds (%s); sync and ordering compare these lexicographically",
			strings.Join(parts, " "))
		return c, nil
	}
	c.Status = StatusOK
	c.Detail = "all timestamps are RFC3339 UTC seconds"
	return c, nil
}

// schemaCheck compares the database schema with this binary's migrations.
// A newer schema is an error: migrations are forward-only, so an older
// binary would run against a schema it does not understand.
func schemaCheck(current, binary int) DoctorCheck {
	c := DoctorCheck{Name: CheckSchema}
	switch {
	case current > binary:
		c.Status = StatusError
		c.Detail = fmt.Sprintf("database schema %d is newer than this binary (%d); upgrade snp before using this database",
			current, binary)
	case current < binary:
		c.Status = StatusWarn
		c.Detail = fmt.Sprintf("database schema %d will be migrated to %d on next open", current, binary)
	default:
		c.Status = StatusOK
		c.Detail = fmt.Sprintf("schema %d matches this binary", current)
	}
	return c
}

// keyCheck inspects the key file. It is the only filesystem check, and it
// deliberately never loads or uses the key: doctor must run on a store
// whose key is missing or wrong.
func keyCheck(path string) DoctorCheck {
	c := DoctorCheck{Name: CheckKey}
	if path == "" {
		c.Status = StatusWarn
		c.Detail = "no key path given; skipped"
		return c
	}
	info, err := os.Stat(path)
	switch {
	case os.IsNotExist(err):
		c.Status = StatusWarn
		c.Detail = fmt.Sprintf("no key file at %s; sensitive bodies cannot be read until it is restored", path)
		return c
	case err != nil:
		c.Status = StatusError
		c.Detail = err.Error()
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		c.Status = StatusError
		c.Detail = err.Error()
		return c
	}
	if len(data) != 32 {
		c.Status = StatusError
		c.Detail = fmt.Sprintf("key file is %d bytes, expected 32", len(data))
		return c
	}
	var problems []string
	if perm := info.Mode().Perm(); perm != 0o600 {
		problems = append(problems, fmt.Sprintf("file mode is %04o, expected 0600", perm))
	}
	if di, err := os.Stat(filepath.Dir(path)); err == nil {
		if perm := di.Mode().Perm(); perm&0o077 != 0 {
			problems = append(problems, fmt.Sprintf("directory mode is %04o, expected 0700", perm))
		}
	}
	if len(problems) > 0 {
		c.Status = StatusWarn
		c.Detail = "32 bytes, but " + strings.Join(problems, " and ")
		return c
	}
	c.Status = StatusOK
	c.Detail = "32 bytes, mode 0600"
	return c
}

// clearSensitiveMirrorsTx empties the plaintext mirror columns on
// sensitive rows. A sensitive row's sealed copies in body and
// var_defaults_enc are untouched; only the plaintext mirrors that must
// stay empty are cleared.
func clearSensitiveMirrorsTx(ctx context.Context, tx *sql.Tx) (int64, error) {
	res, err := tx.ExecContext(ctx,
		`UPDATE snippets SET body_text = '', var_defaults = ''
		 WHERE is_sensitive = 1 AND (body_text <> '' OR var_defaults <> '')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// resyncTagMirrorsTx rewrites the snippets.tags mirror from the
// authoritative snippet_tags rows, ordered by name so the result is
// deterministic. Only rows that disagree are touched.
func (s *Store) resyncTagMirrorsTx(ctx context.Context, tx *sql.Tx) (int64, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM snippets WHERE `+tagMirrorMismatch)
	if err != nil {
		return 0, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	var changed int64
	for _, id := range ids {
		nameRows, err := tx.QueryContext(ctx,
			`SELECT tg.name FROM snippet_tags st JOIN tags tg ON tg.id = st.tag_id
			 WHERE st.snippet_id = ? ORDER BY tg.name`, id)
		if err != nil {
			return 0, err
		}
		var names []string
		for nameRows.Next() {
			var name string
			if err := nameRows.Scan(&name); err != nil {
				nameRows.Close()
				return 0, err
			}
			names = append(names, name)
		}
		if err := nameRows.Err(); err != nil {
			nameRows.Close()
			return 0, err
		}
		nameRows.Close()
		if _, err := tx.ExecContext(ctx,
			`UPDATE snippets SET tags = ? WHERE id = ?`, strings.Join(names, " "), id); err != nil {
			return 0, err
		}
		changed++
	}
	return changed, nil
}

// joinLimited joins at most max items, marking truncation.
func joinLimited(items []string, max int) string {
	if len(items) <= max {
		return strings.Join(items, "; ")
	}
	return strings.Join(items[:max], "; ") + fmt.Sprintf("; … (%d more)", len(items)-max)
}

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jstevewhite/snp/internal/config"
	"github.com/jstevewhite/snp/internal/store"
)

// doctorCLIOptions is the parsed command line for `snp doctor`.
type doctorCLIOptions struct {
	Repair     bool
	Reindex    bool
	Only       []string
	JSON       bool
	Strict     bool
	Full       bool
	FixOrphans bool
}

// doctorJSON is the --json shape. It is one object whether or not a repair
// ran, so a script always parses the same thing.
type doctorJSON struct {
	Report       store.DoctorReport  `json:"report"`
	Repair       *store.RepairResult `json:"repair,omitempty"`
	OrphansFixed int64               `json:"orphans_fixed,omitempty"`
	After        *store.DoctorReport `json:"after,omitempty"`
}

// runDoctor is the `snp doctor` entry point (spec "Health check and index
// repair"). Exit codes: 0 healthy, 1 problems found, 2 could not run.
func runDoctor(args []string) {
	fs := newSubFlags("doctor")
	repair := fs.Bool("repair", false, "apply the derived repairs: the FTS index and the mirrors it reads")
	reindex := fs.Bool("reindex", false, "shorthand for --repair restricted to the index checks")
	only := fs.String("only", "", "comma-separated checks to run: "+strings.Join(store.DoctorCheckNames(), ", "))
	asJSON := fs.Bool("json", false, "print the report as JSON")
	strict := fs.Bool("strict", false, "treat warnings as failures for the exit status")
	full := fs.Bool("full", false, "run PRAGMA integrity_check instead of quick_check")
	fixOrphans := fs.Bool("fix-orphans", false, "null live rows that point at a deleted folder (changes data)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 0 {
		usageError("usage: snp doctor [flags]")
	}
	if *reindex && *only != "" {
		usageError("snp doctor: --reindex and --only are mutually exclusive")
	}
	cfg := mustConfig(fs)
	opts := doctorCLIOptions{
		Repair:     *repair,
		Reindex:    *reindex,
		JSON:       *asJSON,
		Strict:     *strict,
		Full:       *full,
		FixOrphans: *fixOrphans,
	}
	if *only != "" {
		opts.Only = []string{*only}
	}
	code, err := doctorCmd(cfg, opts, os.Stdout)
	if err != nil {
		fmt.Fprintln(os.Stderr, "snp:", err)
		os.Exit(2)
	}
	os.Exit(code)
}

// doctorCmd is the testable core of runDoctor.
//
// The store is opened without the key: every default check reads plaintext
// columns, so doctor still works when the key file is missing or wrong,
// which is exactly when it is most needed. The key is only inspected as a
// file.
func doctorCmd(cfg config.Config, opts doctorCLIOptions, out io.Writer) (int, error) {
	db := dbPath(cfg)
	if _, err := os.Stat(db); err != nil {
		return 2, fmt.Errorf("doctor: no database at %s (has snp serve been run?)", db)
	}

	only := opts.Only
	if opts.Reindex {
		only = []string{store.CheckFTS, store.CheckFTSCount}
	}
	// Reject a bad selection before opening the database.
	for _, name := range only {
		for _, part := range strings.Split(name, ",") {
			if part = strings.TrimSpace(part); part != "" && !store.ValidDoctorCheck(part) {
				return 2, fmt.Errorf("doctor: unknown check %q (known: %s)",
					part, strings.Join(store.DoctorCheckNames(), ", "))
			}
		}
	}

	st, err := openStore(cfg, false)
	if err != nil {
		return 2, err
	}
	defer st.Close()

	dopts := store.DoctorOptions{Only: only, Full: opts.Full, KeyPath: keyPath(cfg)}
	before, err := st.Doctor(dopts)
	if err != nil {
		return 2, err
	}

	var (
		repair    *store.RepairResult
		orphans   int64
		final     = before
		didRepair bool
	)
	if opts.Repair || opts.Reindex || opts.FixOrphans {
		didRepair = true
		if opts.FixOrphans {
			n, err := st.NullOrphanFolderRefs()
			if err != nil {
				return 2, err
			}
			orphans = n
		}
		if opts.Repair || opts.Reindex {
			res, err := st.Repair(dopts)
			if err != nil {
				if errors.Is(err, store.ErrInvalid) {
					return 2, fmt.Errorf("doctor: %w", err)
				}
				return 2, err
			}
			repair = &res
		}
		if final, err = st.Doctor(dopts); err != nil {
			return 2, err
		}
	}

	if opts.JSON {
		payload := doctorJSON{Report: before}
		if didRepair {
			payload.Repair = repair
			payload.OrphansFixed = orphans
			payload.After = &final
		}
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			return 2, err
		}
	} else {
		printDoctorReport(out, db, before)
		if didRepair {
			printRepair(out, repair, orphans)
			fmt.Fprintln(out)
			printDoctorReport(out, db, final)
		}
	}
	return doctorExit(final, opts.Strict), nil
}

// doctorExit maps a report to the process exit code.
func doctorExit(rep store.DoctorReport, strict bool) int {
	for _, c := range rep.Checks {
		if c.Status == store.StatusError {
			return 1
		}
		if strict && c.Status == store.StatusWarn {
			return 1
		}
	}
	return 0
}

func printDoctorReport(w io.Writer, db string, rep store.DoctorReport) {
	fmt.Fprintf(w, "snp doctor — %s\n", db)
	fmt.Fprintf(w, "schema %d (binary %d) · %s (%d trashed) · %s · %s · %s\n\n",
		rep.SchemaVersion, rep.BinarySchema,
		plural(int(rep.Counts.Snippets), "snippet"), rep.Counts.Trashed,
		plural(int(rep.Counts.Folders), "folder"),
		plural(int(rep.Counts.Tags), "tag"),
		plural(int(rep.Counts.Revisions), "revision"))
	var errs, warns int
	for _, c := range rep.Checks {
		fmt.Fprintf(w, "  %-5s %-11s %s\n", doctorStatusLabel(c.Status), c.Name, c.Detail)
		switch c.Status {
		case store.StatusError:
			errs++
		case store.StatusWarn:
			warns++
		}
	}
	fmt.Fprintln(w)
	switch {
	case errs == 0 && warns == 0:
		fmt.Fprintln(w, "no problems found")
	case errs == 0:
		fmt.Fprintf(w, "%s, no errors\n", plural(warns, "warning"))
	default:
		fmt.Fprintf(w, "%s, %s\n", plural(errs, "error"), plural(warns, "warning"))
	}
}

func printRepair(w io.Writer, res *store.RepairResult, orphans int64) {
	if res == nil && orphans == 0 {
		return
	}
	var parts []string
	if res != nil {
		if res.ClearedSensitiveMirrors > 0 {
			parts = append(parts, fmt.Sprintf("cleared %d sensitive mirror(s)", res.ClearedSensitiveMirrors))
		}
		if res.ResyncedTagMirrors > 0 {
			parts = append(parts, fmt.Sprintf("resynced %d tags mirror(s)", res.ResyncedTagMirrors))
		}
		if res.RebuiltFTS {
			parts = append(parts, "rebuilt the FTS index")
		}
	}
	if orphans > 0 {
		parts = append(parts, fmt.Sprintf("nulled %d orphaned folder reference(s)", orphans))
	}
	if len(parts) == 0 {
		fmt.Fprintln(w, "repair: nothing to change")
		return
	}
	fmt.Fprintf(w, "repair: %s\n", strings.Join(parts, ", "))
}

func doctorStatusLabel(s store.DoctorStatus) string {
	switch s {
	case store.StatusError:
		return "ERROR"
	case store.StatusWarn:
		return "warn"
	default:
		return "ok"
	}
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

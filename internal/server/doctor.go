package server

import (
	"net/http"
	"strconv"

	"github.com/jstevewhite/snp/internal/store"
)

// doctorOut is the /api/doctor response. It is one shape whether or not a
// repair ran, so a client always parses the same thing: the state that was
// checked, and on a repair what changed plus the state afterwards.
type doctorOut struct {
	Report *store.DoctorReport `json:"report"`
	Repair *store.RepairResult `json:"repair,omitempty"`
	After  *store.DoctorReport `json:"after,omitempty"`
}

// doctorRequest is the optional body of POST /api/doctor/repair.
type doctorRequest struct {
	Only []string `json:"only"`
	Full bool     `json:"full"`
}

// handleDoctor runs the health checks and returns the report (spec
// "Health check and index repair"). Read-only, so GET is correct and the
// CSRF guard has nothing to police.
//
// Query parameters: only=<checks> narrows the run, full=1 uses the slow
// integrity pragma.
func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	opts := store.DoctorOptions{KeyPath: s.keyPath, Full: boolParam(q.Get("full"))}
	if only := q.Get("only"); only != "" {
		opts.Only = []string{only}
	}
	report, err := s.store.Doctor(opts)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doctorOut{Report: &report})
}

// handleDoctorRepair applies the derived repairs and returns what changed
// plus a fresh report.
//
// Repair only rewrites derived data — the FTS index and the plaintext
// mirror columns it reads — so it is safe to expose on a live server. It
// takes the doctor mutex so two repairs cannot overlap; the mutex is
// separate from the backup one because the two do not contend on the same
// resources and a repair is fast.
func (s *Server) handleDoctorRepair(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var in doctorRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := decodeJSON(r, &in); err != nil {
			s.handleBodyErr(w, err)
			return
		}
	}
	if !s.doctorMu.TryLock() {
		writeError(w, http.StatusConflict, "A repair is already running. Try again shortly.")
		return
	}
	defer s.doctorMu.Unlock()

	opts := store.DoctorOptions{Only: in.Only, Full: in.Full, KeyPath: s.keyPath}
	before, err := s.store.Doctor(opts)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	result, err := s.store.Repair(opts)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	after, err := s.store.Doctor(opts)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, doctorOut{Report: &before, Repair: &result, After: &after})
}

// boolParam reads a query flag, treating anything unparseable as false.
func boolParam(value string) bool {
	on, err := strconv.ParseBool(value)
	return err == nil && on
}

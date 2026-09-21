package server

import (
	"net/http"
	"strconv"
)

func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	out, err := s.store.ListTrash(limit, offset)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleRestoreSnippet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	out, err := s.store.RestoreSnippet(r.PathValue("id"))
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleRevisions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	out, err := s.store.ListRevisions(r.PathValue("id"))
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleRevision(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	rid, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || rid <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid revision"})
		return
	}
	out, err := s.store.GetRevision(r.PathValue("id"), rid)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	if out.Protected && r.URL.Query().Get("reveal") != "1" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "Reveal this protected version to view its content."})
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (s *Server) handleRestoreRevision(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	rid, err := strconv.ParseInt(r.PathValue("revision"), 10, 64)
	if err != nil || rid <= 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid revision"})
		return
	}
	out, err := s.store.RestoreRevision(r.PathValue("id"), rid)
	if err != nil {
		s.handleStoreErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

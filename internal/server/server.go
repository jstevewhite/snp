// Package server implements snp's HTTP API and embedded SPA serving
// (spec §5).
//
// The middleware chain, in order, is: recover → request logging → auth →
// content-type check → body size limit. Auth and the content-type/body
// guards apply to /api/* only; the embedded SPA is served without auth so
// the shell loads for any tailnet peer, while every data endpoint still
// requires the configured owner.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"sync"
	"time"

	"github.com/jstevewhite/snp/internal/ai"
	"github.com/jstevewhite/snp/internal/store"
	"github.com/jstevewhite/snp/internal/tsauth"
	webembed "github.com/jstevewhite/snp/web"
)

// maxBodyBytes caps request bodies at 10 MiB (spec §5).
const maxBodyBytes = 10 << 20

// stateChanging lists the HTTP methods that must carry a JSON body
// (spec §3 CSRF rule).
var stateChanging = map[string]bool{"POST": true, "PUT": true, "DELETE": true}

type ctxKey int

const (
	identityKey ctxKey = iota
	requestInfoKey
)

// requestInfo is planted in the context by loggingMW and filled in by
// authMW. Context values only flow inward: authMW's r.WithContext copy
// never reaches the logger, which holds the original request, so the
// login has to travel back through a pointer the logger owns.
type requestInfo struct {
	login string
}

// Server serves the snp API and the embedded SPA.
type Server struct {
	store    *store.Store
	resolver tsauth.IdentityResolver
	owner    string
	log      *slog.Logger
	staticFS http.FileSystem
	// ai is the optional one-shot snippet-generation client (spec §13);
	// nil means the AI feature is not configured.
	ai       *ai.Client
	backupMu sync.Mutex
	cryptoMu sync.Mutex
}

// New builds a Server. owner is the Tailscale login allowed in; pass "" to
// disable the owner check (dev mode, where the resolver is a fixed
// identity).
func New(st *store.Store, resolver tsauth.IdentityResolver, owner string, log *slog.Logger) *Server {
	return NewWithAI(st, resolver, owner, log, nil)
}

// NewWithAI is New with an optional AI client (spec §13). Pass nil (or
// use New) to leave the AI feature unconfigured: /api/ai/status then
// reports enabled:false and /api/ai/generate answers 503.
func NewWithAI(st *store.Store, resolver tsauth.IdentityResolver, owner string, log *slog.Logger, aiClient *ai.Client) *Server {
	dist, err := fs.Sub(webembed.FS, "dist")
	if err != nil {
		panic("server: web/dist not found: " + err.Error())
	}
	return &Server{
		store:    st,
		resolver: resolver,
		owner:    owner,
		log:      log,
		staticFS: http.FS(dist),
		ai:       aiClient,
	}
}

// Handler returns the root http.Handler for the snp binary.
func (s *Server) Handler() http.Handler {
	api := s.apiMux()
	api = s.guardMW(api)
	api = s.authMW(api)

	top := http.NewServeMux()
	top.Handle("/api/", api)
	top.Handle("/", s.staticHandler())

	return s.recoverMW(s.loggingMW(top))
}

// recoverMW recovers from panics and returns 500.
func (s *Server) recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic", "err", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "internal error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// loggingMW logs each request with method, path, status, duration, and the
// caller's login. The query string is intentionally omitted (spec §9: no
// full query strings, which can carry sensitive content).
func (s *Server) loggingMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		info := &requestInfo{}
		r = r.WithContext(context.WithValue(r.Context(), requestInfoKey, info))
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", int(time.Since(start).Milliseconds()),
			"login", info.login,
		)
	})
}

// authMW resolves the caller's identity via the resolver, enforces the
// owner, and stores the identity in the request context.
func (s *Server) authMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, err := s.resolver.WhoIs(r.Context(), r.RemoteAddr)
		if err != nil {
			s.log.Error("whois failed", "err", err, "remote", r.RemoteAddr)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		// Record the login for the request log before the owner check,
		// so a rejected caller is logged too.
		if info, ok := r.Context().Value(requestInfoKey).(*requestInfo); ok {
			info.login = id.Login
		}
		if s.owner != "" && id.Login != s.owner {
			writeError(w, http.StatusForbidden, "forbidden")
			return
		}
		ctx := context.WithValue(r.Context(), identityKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// guardMW enforces the JSON content-type on state-changing methods (415)
// and caps request bodies at maxBodyBytes (413).
func (s *Server) guardMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if stateChanging[r.Method] && !isJSONContentType(r.Header.Get("Content-Type")) {
			writeError(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
			return
		}
		if (r.Method == http.MethodPost || r.Method == http.MethodPut) && r.Body != nil {
			limit := int64(maxBodyBytes)
			// Armoring adds ~36%; decrypted JSON remains limited to 10 MiB.
			if r.URL.Path == "/api/decrypt-import" {
				limit = 16 << 20
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// identityFrom returns the caller's identity from the request context, or
// an empty identity if auth did not run (e.g. static requests).
func identityFrom(ctx context.Context) tsauth.Identity {
	if id, ok := ctx.Value(identityKey).(tsauth.Identity); ok {
		return id
	}
	return tsauth.Identity{}
}

// isJSONContentType reports whether ct is application/json, ignoring any
// parameters (e.g. a charset).
func isJSONContentType(ct string) bool {
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil {
		return false
	}
	return mediaType == "application/json"
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error body (spec §5: {"error": "message"}).
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// decodeJSON reads and parses the request body.
func decodeJSON(r *http.Request, v any) error {
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// handleBodyErr maps a body read/parse error to a status: an oversized
// body (rejected by the MaxBytesReader guard) is 413, anything else is a
// 400.
func (s *Server) handleBodyErr(w http.ResponseWriter, err error) {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
		return
	}
	writeError(w, http.StatusBadRequest, "invalid JSON body")
}

// handleStoreErr maps a store error to a status (spec §8). 5xx details are
// logged but never returned.
func (s *Server) handleStoreErr(w http.ResponseWriter, err error) {
	code := statusCode(err)
	if code >= 500 {
		s.log.Error("store error", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeError(w, code, err.Error())
}

// statusCode maps store errors to HTTP status codes (spec §8).
func statusCode(err error) int {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, store.ErrFolderNotEmpty):
		return http.StatusConflict
	case errors.Is(err, store.ErrNameTaken):
		return http.StatusConflict
	case errors.Is(err, store.ErrFolderCycle):
		return http.StatusBadRequest
	case errors.Is(err, store.ErrInvalidTag):
		return http.StatusBadRequest
	case errors.Is(err, store.ErrInvalid):
		return http.StatusBadRequest
	case errors.Is(err, store.ErrFTS):
		return http.StatusBadRequest
	case errors.Is(err, store.ErrImport):
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// statusRecorder captures the response status for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.status = http.StatusOK
		s.wrote = true
	}
	return s.ResponseWriter.Write(b)
}

// Package studio serves a local web editor for registry data. Edits are held as
// entity-level drafts outside the registry tree and reach it only through the
// generator, either in the working tree or on a pull-request branch.
package studio

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/registrygen"
	"github.com/rigerc/nosey-9/internal/settings"
)

//go:embed web
var webFiles embed.FS

const (
	tokenHeader  = "X-Nosey-Studio-Token" // #nosec G101 -- header name, not a credential.
	maxBodyBytes = 8 << 20
)

// Options configures one studio session.
type Options struct {
	Root           string
	Store          settings.Store
	RegistryChecks bool
	// Local edits the checkout as it stands instead of the remote default branch.
	Local  bool
	Runner process.Executor
	// GitURL rewrites upstream URLs; tests point it at local repositories.
	GitURL func(string) string
}

// Server is one studio session bound to a registry directory.
type Server struct {
	opts    Options
	root    string
	token   string
	allowed map[string]bool

	mu sync.Mutex
	// view is the directory being read: root, or a snapshot of the remote.
	view      string
	exported  string
	source    Source
	cfg       registrygen.Config
	automated bool
	draft     Draft
	conflicts map[string]Conflict
}

// New loads the registry configuration and any saved draft.
func New(opts Options) (*Server, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return nil, err
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		return nil, err
	}
	cfg, err := registrygen.Load(root)
	if err != nil {
		return nil, err
	}
	draft, err := loadDraft(opts.Store.Data, root)
	if err != nil {
		return nil, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return nil, err
	}
	if opts.Runner == nil {
		opts.Runner = process.Runner{Timeout: 10 * time.Minute, Limit: 8 << 20}
	}
	if opts.GitURL == nil {
		opts.GitURL = func(uri string) string { return uri }
	}
	return &Server{
		opts: opts, root: root, view: root, source: Source{Mode: sourceCheckout}, cfg: cfg, automated: cfg.Registry.ID == officialID,
		token: hex.EncodeToString(secret), allowed: map[string]bool{}, draft: draft, conflicts: map[string]Conflict{},
	}, nil
}

// Allow permits requests addressed to host, normally the bound loopback port.
func (s *Server) Allow(host string) { s.allowed[host] = true }

// Token returns the per-session API credential.
func (s *Server) Token() string { return s.token }

// Handler returns the guarded HTTP surface.
func (s *Server) Handler() http.HandlerFunc {
	mux := http.NewServeMux()
	static, err := fs.Sub(webFiles, "web")
	if err != nil {
		// The web directory is embedded at build time, so this cannot fail at runtime.
		panic(err)
	}
	mux.Handle("GET /", http.FileServerFS(static))
	mux.Handle("GET /api/state", s.api(s.handleState))
	mux.Handle("POST /api/draft", s.api(s.handleDraft))
	mux.Handle("POST /api/draft/clear", s.api(s.handleClear))
	mux.Handle("POST /api/draft/resolve", s.api(s.handleResolveConflict))
	mux.Handle("POST /api/preview", s.api(s.handlePreview))
	mux.Handle("POST /api/rules/test", s.api(s.handleRuleTest))
	mux.Handle("POST /api/skills/resolve", s.api(s.handleResolveSkill))
	mux.Handle("POST /api/refresh", s.api(s.handleRefresh))
	mux.Handle("POST /api/apply", s.api(s.handleApply))
	mux.Handle("POST /api/publish", s.api(s.handlePublish))
	return s.guard(mux)
}

// guard rejects DNS-rebinding and cross-origin requests before any handler runs.
func (s *Server) guard(next http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.allowed[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
			http.Error(w, "forbidden origin", http.StatusForbidden)
			return
		}
		header := w.Header()
		header.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; connect-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "no-referrer")
		header.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// handlerFunc returns the response value and an error mapped to a status.
type handlerFunc func(*http.Request) (response, error)

type response struct {
	status int
	body   json.RawMessage
}

func ok[T any](value T) (response, error) { return encoded(http.StatusOK, value) }

func encoded[T any](status int, value T) (response, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return response{}, err
	}
	return response{status, data}, nil
}

func (s *Server) api(handler handlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get(tokenHeader)), []byte(s.token)) != 1 {
			writeError(w, http.StatusUnauthorized, "missing or invalid session token")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		result, err := handler(r)
		if err != nil {
			var policy policyError
			var request requestError
			switch {
			case errors.As(err, &policy):
				writeError(w, http.StatusForbidden, policy.Error())
			case errors.As(err, &request):
				writeError(w, http.StatusBadRequest, request.Error())
			default:
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(result.status)
		_, _ = w.Write(result.body)
	})
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

// requestError is a malformed or unusable client request.
type requestError struct{ message string }

func (e requestError) Error() string { return e.message }

func badRequest(format string, args ...any) error {
	return requestError{fmt.Sprintf(format, args...)}
}

// decodeInto reads one JSON request body into target.
func decodeInto[T any](r *http.Request, target *T) error {
	if err := json.NewDecoder(r.Body).Decode(target); err != nil && !errors.Is(err, io.EOF) {
		return badRequest("invalid request: %v", err)
	}
	return nil
}

// Meta describes the session for the UI.
type Meta struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Root           string   `json:"root"`
	Automated      bool     `json:"automated"`
	RegistryChecks bool     `json:"registryChecks"`
	Roles          []string `json:"roles"`
	Local          bool     `json:"local"`
	Git            GitState `json:"git"`
	Source         Source   `json:"source"`
}

// State is the full editor payload.
type State struct {
	Meta        Meta                         `json:"meta"`
	Documents   map[string][]json.RawMessage `json:"documents"`
	Ops         []Op                         `json:"ops"`
	Published   *Published                   `json:"published"`
	Diagnostics []domain.Diagnostic          `json:"diagnostics"`
	Assessments Assessments                  `json:"assessments"`
}

func (s *Server) handleState(r *http.Request) (response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	docs, err := loadDocuments(s.view, s.cfg)
	if err != nil {
		return response{}, err
	}
	state := State{
		Meta:      Meta{ID: s.cfg.Registry.ID, Name: s.cfg.Registry.Name, Root: s.root, Automated: s.automated, RegistryChecks: s.opts.RegistryChecks, Local: s.opts.Local, Git: s.gitState(r.Context()), Source: s.source},
		Documents: make(map[string][]json.RawMessage, len(docs)),
		Ops:       s.draft.Ops, Published: s.draft.Published,
		Assessments: readAssessments(s.view),
	}
	for role, doc := range docs {
		state.Documents[role] = doc.entities()
		state.Meta.Roles = append(state.Meta.Roles, role)
	}
	state.Diagnostics = s.diagnostics(r.Context(), s.draft.Ops)
	return ok(state)
}

type opInput struct {
	Role   string          `json:"role"`
	ID     string          `json:"id"`
	Entity json.RawMessage `json:"entity"`
	Delete bool            `json:"delete"`
	// Location accompanies a newly added skill.
	Location *Location `json:"location"`
}

type draftResult struct {
	Ops         []Op                `json:"ops"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
}

func (s *Server) handleDraft(r *http.Request) (response, error) {
	var input struct {
		Ops []opInput `json:"ops"`
	}
	if err := decodeInto(r, &input); err != nil {
		return response{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	docs, err := loadDocuments(s.view, s.cfg)
	if err != nil {
		return response{}, err
	}
	next := map[string]Op{}
	for _, op := range s.draft.Ops {
		next[op.key()] = op
	}
	probes := readProbes(s.view)
	for _, change := range input.Ops {
		if err = s.stageOp(next, docs, probes, change); err != nil {
			return response{}, err
		}
	}
	ops := make([]Op, 0, len(next))
	for _, op := range next {
		ops = append(ops, op)
	}
	updated := s.draft
	updated.Ops = ops
	if err = saveDraft(s.opts.Store.Data, s.root, updated); err != nil {
		return response{}, err
	}
	s.draft = updated
	return ok(draftResult{Ops: s.draft.Ops, Diagnostics: s.diagnostics(r.Context(), s.draft.Ops)})
}

// stageOp validates one change against ownership rules and merges it into next.
func (s *Server) stageOp(next map[string]Op, docs map[string]*document, probes map[string]bool, change opInput) error {
	doc := docs[change.Role]
	if doc == nil {
		return badRequest("registry does not declare %q", change.Role)
	}
	if change.ID == "" {
		return badRequest("entity id is required")
	}
	op := Op{Role: change.Role, ID: change.ID, Location: change.Location}
	if existing, pending := next[op.key()]; pending {
		op.Base = existing.Base
		if op.Location == nil {
			op.Location = existing.Location
		}
	} else {
		op.Base = doc.byID[change.ID]
	}
	if !change.Delete {
		if len(change.Entity) == 0 {
			return badRequest("%s %s has no entity; set delete to remove it", change.Role, change.ID)
		}
		var header struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(change.Entity, &header); err != nil || header.ID != change.ID {
			return badRequest("entity id does not match %q", change.ID)
		}
		normalized, err := normalizeEntity(change.Role, change.Entity)
		if err != nil {
			return badRequest("%s %s: %v", change.Role, change.ID, err)
		}
		op.Entity = normalized
	}
	if op.Entity == nil && op.Base == nil {
		delete(next, op.key())
		return nil
	}
	if op.Entity != nil && op.Base != nil && sameJSON(op.Entity, normalizedOrRaw(change.Role, op.Base)) {
		delete(next, op.key())
		return nil
	}
	if s.automated && change.Role == roleSkills {
		if err := guardSkill(op, s.opts.RegistryChecks, probes[change.ID]); err != nil {
			return err
		}
	}
	next[op.key()] = op
	return nil
}

func normalizedOrRaw(role string, entity json.RawMessage) json.RawMessage {
	normalized, err := normalizeEntity(role, entity)
	if err != nil {
		return entity
	}
	return normalized
}

func (s *Server) handleClear(*http.Request) (response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cleared := Draft{Version: 1, Ops: []Op{}}
	if err := saveDraft(s.opts.Store.Data, s.root, cleared); err != nil {
		return response{}, err
	}
	s.draft = cleared
	s.conflicts = map[string]Conflict{}
	return ok(map[string]bool{"ok": true})
}

// handleResolveConflict keeps an edit on top of the upstream entity or drops it.
func (s *Server) handleResolveConflict(r *http.Request) (response, error) {
	var input struct {
		Role   string `json:"role"`
		ID     string `json:"id"`
		Action string `json:"action"`
	}
	if err := decodeInto(r, &input); err != nil {
		return response{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := Op{Role: input.Role, ID: input.ID}.key()
	conflict, known := s.conflicts[key]
	if !known {
		return response{}, badRequest("no recorded conflict for %s", input.ID)
	}
	ops := make([]Op, 0, len(s.draft.Ops))
	for _, op := range s.draft.Ops {
		if op.key() != key {
			ops = append(ops, op)
			continue
		}
		switch input.Action {
		case "keep":
			op.Base = conflict.Current
			ops = append(ops, op)
		case "discard":
		default:
			return response{}, badRequest("unknown action %q", input.Action)
		}
	}
	updated := s.draft
	updated.Ops = ops
	if err := saveDraft(s.opts.Store.Data, s.root, updated); err != nil {
		return response{}, err
	}
	s.draft = updated
	delete(s.conflicts, key)
	return ok(draftResult{Ops: s.draft.Ops, Diagnostics: s.diagnostics(r.Context(), s.draft.Ops)})
}

// Run serves the studio on loopback until ctx is cancelled.
func Run(ctx context.Context, opts Options, port int, openBrowser bool, out io.Writer) error {
	server, err := New(opts)
	if err != nil {
		return err
	}
	defer server.Close()
	if !opts.Local {
		_, _ = fmt.Fprintln(out, "Fetching the latest registry data…")
	}
	source := server.Sync(ctx)
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	address := listener.Addr().String()
	server.Allow(address)
	_, bound, _ := net.SplitHostPort(address)
	server.Allow("localhost:" + bound)
	link := "http://" + address + "/#token=" + server.Token()
	_, _ = fmt.Fprintf(out, "Registry studio for %s\n%s\n%s\nPress Ctrl+C to stop.\n", server.cfg.Registry.ID, describeSource(source), link)
	if openBrowser {
		openURL(ctx, link)
	}
	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second}
	stopped := make(chan error, 1)
	go func() { stopped <- httpServer.Serve(listener) }()
	select {
	case err = <-stopped:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdown)
	}
}

func describeSource(source Source) string {
	switch {
	case source.Warning != "":
		return "Warning: " + source.Warning
	case source.Mode == sourceRemote:
		return "Editing " + source.Ref + " at " + source.Commit[:min(len(source.Commit), 7)]
	default:
		return "Editing the local checkout"
	}
}

// openURL is best effort: the link is already printed for manual use.
func openURL(ctx context.Context, link string) {
	var name string
	switch runtime.GOOS {
	case "linux":
		name = "xdg-open"
	case "darwin":
		name = "open"
	default:
		return
	}
	if !strings.HasPrefix(link, "http://127.0.0.1:") {
		return
	}
	go func() {
		_ = process.Command(context.WithoutCancel(ctx), "", nil, name, link).Run()
	}()
}

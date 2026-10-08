// Package api exposes the Orxest orchestration core over the documented REST
// interface and Server-Sent Events (spec §32, §42).
//
// The HTTP layer is deliberately thin: it decodes requests, calls application
// services and maps domain errors onto status codes. No orchestration logic
// lives here.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/config"
	"github.com/orxest/orxest/internal/domain"
)

// MaxBodyBytes caps request bodies.
const MaxBodyBytes = 4 << 20

// Server is the Orxest HTTP server.
type Server struct {
	cfg     config.Config
	svc     *app.Service
	log     *slog.Logger
	mux     *http.ServeMux
	web     http.Handler
	started time.Time
}

// New builds the API server. web may be nil, in which case a placeholder page
// is served for non-API routes.
func New(cfg config.Config, svc *app.Service, web http.Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{
		cfg:     cfg,
		svc:     svc,
		log:     log,
		mux:     http.NewServeMux(),
		web:     web,
		started: time.Now().UTC(),
	}
	s.routes()
	return s
}

// Handler returns the fully decorated HTTP handler. When server.base_path is
// configured, every route (API, streams and the web interface) is served under
// that prefix, which is what a reverse proxy usually expects.
func (s *Server) Handler() http.Handler {
	var root http.Handler = s.mux
	if prefix := strings.TrimSuffix(s.cfg.Server.BasePath, "/"); prefix != "" {
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		root = http.StripPrefix(prefix, s.mux)
	}
	return s.recoverer(s.requestLogger(s.cors(root)))
}

func (s *Server) routes() {
	m := s.mux

	// System
	m.HandleFunc("GET /api/health", s.handleHealth)
	m.HandleFunc("GET /api/meta", s.handleMeta)
	m.HandleFunc("GET /api/openapi.json", s.handleOpenAPI)
	m.HandleFunc("GET /api/scheduler", s.handleSchedulerStatus)
	m.HandleFunc("POST /api/scheduler/tick", s.handleSchedulerTick)
	m.HandleFunc("GET /api/events", s.handleRecentEvents)

	// Projects
	m.HandleFunc("GET /api/projects", s.handleListProjects)
	m.HandleFunc("POST /api/projects", s.handleCreateProject)
	m.HandleFunc("POST /api/projects/import", s.handleImportProject)
	m.HandleFunc("POST /api/projects/validate", s.handleValidateProjectConfig)
	m.HandleFunc("GET /api/projects/{id}", s.handleGetProject)
	m.HandleFunc("PATCH /api/projects/{id}", s.handleUpdateProject)
	m.HandleFunc("DELETE /api/projects/{id}", s.handleDeleteProject)
	m.HandleFunc("GET /api/projects/{id}/stats", s.handleProjectStats)
	m.HandleFunc("GET /api/projects/{id}/board", s.handleBoard)
	m.HandleFunc("GET /api/projects/{id}/repository", s.handleRepositoryStatus)
	m.HandleFunc("POST /api/projects/{id}/repository/ensure", s.handleEnsureRepository)
	m.HandleFunc("POST /api/projects/{id}/config", s.handleImportProjectConfig)
	m.HandleFunc("POST /api/projects/{id}/decompose", s.handleDecompose)
	m.HandleFunc("GET /api/projects/{id}/dependencies", s.handleDependencyGraph)

	// Issues
	m.HandleFunc("GET /api/projects/{id}/issues", s.handleListIssues)
	m.HandleFunc("POST /api/projects/{id}/issues", s.handleCreateIssue)
	m.HandleFunc("GET /api/projects/{id}/tasks", s.handleListProjectTasks)
	m.HandleFunc("GET /api/issues/{id}", s.handleGetIssue)
	m.HandleFunc("PATCH /api/issues/{id}", s.handleUpdateIssue)
	m.HandleFunc("DELETE /api/issues/{id}", s.handleDeleteIssue)
	m.HandleFunc("GET /api/issues/{id}/tasks", s.handleListIssueTasks)
	m.HandleFunc("POST /api/issues/{id}/tasks", s.handleCreateIssueTask)

	// Tasks
	m.HandleFunc("GET /api/tasks/{id}", s.handleGetTask)
	m.HandleFunc("PATCH /api/tasks/{id}", s.handleUpdateTask)
	m.HandleFunc("DELETE /api/tasks/{id}", s.handleDeleteTask)
	m.HandleFunc("GET /api/tasks/{id}/detail", s.handleTaskDetail)
	m.HandleFunc("GET /api/tasks/{id}/executions", s.handleListTaskExecutions)
	m.HandleFunc("GET /api/tasks/{id}/events", s.handleListTaskEvents)
	m.HandleFunc("POST /api/tasks/{id}/start", s.handleStartTask)
	m.HandleFunc("POST /api/tasks/{id}/retry", s.handleRetryTask)
	m.HandleFunc("POST /api/tasks/{id}/cancel", s.handleCancelTask)
	m.HandleFunc("POST /api/tasks/{id}/approve", s.handleApproveTask)
	m.HandleFunc("POST /api/tasks/{id}/reject", s.handleRejectTask)
	m.HandleFunc("POST /api/tasks/{id}/reassign", s.handleReassignTask)
	m.HandleFunc("POST /api/tasks/{id}/move", s.handleMoveTask)
	m.HandleFunc("POST /api/tasks/{id}/dependencies", s.handleAddDependency)
	m.HandleFunc("DELETE /api/tasks/{id}/dependencies/{dependsOnId}", s.handleRemoveDependency)

	// Executions
	m.HandleFunc("GET /api/executions", s.handleListExecutions)
	m.HandleFunc("GET /api/executions/{id}", s.handleGetExecution)
	m.HandleFunc("POST /api/executions/{id}/cancel", s.handleCancelExecution)
	m.HandleFunc("GET /api/executions/{id}/events", s.handleExecutionEvents)
	m.HandleFunc("GET /api/executions/{id}/log", s.handleExecutionLog)
	m.HandleFunc("GET /api/executions/{id}/stream", s.handleExecutionStream)

	// Agents, roles, projects agents
	m.HandleFunc("GET /api/agents", s.handleListAgents)
	m.HandleFunc("POST /api/agents", s.handleCreateAgent)
	m.HandleFunc("GET /api/agents/{id}", s.handleGetAgent)
	m.HandleFunc("PATCH /api/agents/{id}", s.handleUpdateAgent)
	m.HandleFunc("DELETE /api/agents/{id}", s.handleDeleteAgent)
	m.HandleFunc("GET /api/roles", s.handleListRoles)
	m.HandleFunc("POST /api/roles", s.handleCreateRole)
	m.HandleFunc("GET /api/workflow-templates", s.handleWorkflowTemplates)
	m.HandleFunc("GET /api/projects/{id}/agents", s.handleListProjectAgents)
	m.HandleFunc("POST /api/projects/{id}/agents", s.handleAssignAgent)
	m.HandleFunc("PATCH /api/projects/{id}/agents/{assignmentId}", s.handleUpdateAssignment)
	m.HandleFunc("DELETE /api/projects/{id}/agents/{assignmentId}", s.handleDeleteAssignment)
	m.HandleFunc("GET /api/projects/{id}/workflows", s.handleListWorkflows)
	m.HandleFunc("PUT /api/projects/{id}/workflow", s.handleReplaceWorkflow)

	// Live event streams
	m.HandleFunc("GET /api/projects/{id}/events", s.handleProjectEvents)
	m.HandleFunc("GET /api/projects/{id}/stream", s.handleProjectStream)

	// Unknown API routes must answer with JSON, not with the web shell.
	m.HandleFunc("/api/", s.handleAPINotFound)

	// Single page application
	m.Handle("/", s.webHandler())
}

// handleAPINotFound keeps the API surface honest: a mistyped endpoint returns a
// JSON 404 instead of the single page application.
func (s *Server) handleAPINotFound(w http.ResponseWriter, r *http.Request) {
	s.fail(w, r, domain.NotFoundf("route %s %s", r.Method, r.URL.Path))
}

func (s *Server) webHandler() http.Handler {
	if s.web != nil {
		return s.web
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			s.fail(w, r, domain.NotFoundf("route %s", r.URL.Path))
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Orxest API is running. The web interface was not built into this binary; run `make web` and rebuild, or use the API at /api.")
	})
}

// ---------------------------------------------------------------------------
// middleware
// ---------------------------------------------------------------------------

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.ErrorContext(r.Context(), "panic while handling request",
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Any("panic", rec))
				s.writeError(w, http.StatusInternalServerError, "internal_error", "internal server error", "")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Flush keeps SSE working through the recorder.
func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if strings.HasSuffix(r.URL.Path, "/stream") {
			return // long lived streams log their own start/stop
		}
		level := slog.LevelInfo
		switch {
		case rec.status >= 500:
			level = slog.LevelError
		case rec.status >= 400:
			level = slog.LevelWarn
		}
		s.log.Log(r.Context(), level, "http request",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", rec.status),
			slog.Int("bytes", rec.bytes),
			slog.Duration("duration", time.Since(start)))
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	origins := map[string]bool{}
	for _, o := range s.cfg.Server.DevCORSOrigins {
		origins[o] = true
	}
	if len(origins) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	// Responses always express collections as arrays/objects, never as null.
	if err := enc.Encode(normalizedValue(v)); err != nil {
		s.log.Error("encoding response failed", slog.String("error", err.Error()))
	}
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func (s *Server) writeError(w http.ResponseWriter, status int, code, message, field string) {
	s.writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message, Field: field}})
}

// fail maps a domain error onto HTTP.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	field := ""

	var validation *domain.ValidationError
	switch {
	case errors.As(err, &validation):
		status = http.StatusBadRequest
		code = "invalid_request"
		field = validation.Field
	case errors.Is(err, domain.ErrInvalid):
		status = http.StatusBadRequest
		code = "invalid_request"
	case errors.Is(err, domain.ErrNotFound):
		status = http.StatusNotFound
		code = "not_found"
	case errors.Is(err, domain.ErrDependencyCycle):
		status = http.StatusConflict
		code = "dependency_cycle"
	case errors.Is(err, domain.ErrConflict):
		status = http.StatusConflict
		code = "conflict"
	case errors.Is(err, domain.ErrUnavailable):
		status = http.StatusServiceUnavailable
		code = "unavailable"
	}
	message := err.Error()
	if status >= 500 {
		s.log.ErrorContext(r.Context(), "request failed",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.String("error", err.Error()))
		message = "internal server error"
	}
	s.writeError(w, status, code, message, field)
}

// decode reads a JSON request body strictly: unknown fields are rejected so
// that typos do not silently do nothing.
func decode(r *http.Request, dst any) error {
	if r.Body == nil {
		return domain.Invalidf("body", "a JSON body is required")
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, MaxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if errors.Is(err, io.EOF) {
			return domain.Invalidf("body", "a JSON body is required")
		}
		return domain.Invalidf("body", "invalid JSON: %s", err.Error())
	}
	return nil
}

// decodeOptional accepts an empty body.
func decodeOptional(r *http.Request, dst any) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	return decode(r, dst)
}

func pathValue(r *http.Request, name string) string {
	return r.PathValue(name)
}

func queryInt(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return def
	}
	return v
}

func queryBool(r *http.Request, name string, def bool) bool {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		return def
	}
	return v
}

func queryList(r *http.Request, name string) []string {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func taskStatuses(values []string) []domain.TaskStatus {
	out := make([]domain.TaskStatus, 0, len(values))
	for _, v := range values {
		out = append(out, domain.TaskStatus(v))
	}
	return out
}

func executionStatuses(values []string) []domain.ExecutionStatus {
	out := make([]domain.ExecutionStatus, 0, len(values))
	for _, v := range values {
		out = append(out, domain.ExecutionStatus(v))
	}
	return out
}

// listResponse is the envelope for collection endpoints.
type listResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

func listOf[T any](items []T) listResponse[T] {
	if items == nil {
		items = []T{}
	}
	return listResponse[T]{Items: items, Total: len(items)}
}

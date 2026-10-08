package api

import (
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/orxest/orxest/internal/domain/repository"
)

func (s *Server) handleListExecutions(w http.ResponseWriter, r *http.Request) {
	execs, err := s.svc.Executions.List(r.Context(), repository.ExecutionFilter{
		ProjectID: r.URL.Query().Get("project_id"),
		TaskID:    r.URL.Query().Get("task_id"),
		AgentID:   r.URL.Query().Get("agent_id"),
		Statuses:  executionStatuses(queryList(r, "status")),
		Limit:     queryInt(r, "limit", 100),
		Offset:    queryInt(r, "offset", 0),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(execs))
}

func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	exec, err := s.svc.Executions.Get(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, exec)
}

func (s *Server) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
	cancelled, err := s.svc.Executions.Cancel(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"cancelled": cancelled})
}

func (s *Server) handleExecutionEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.Events.ExecutionEvents(r.Context(),
		pathValue(r, "id"), queryInt(r, "after_seq", 0), queryInt(r, "limit", 1000))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(events))
}

// handleExecutionLog streams the raw harness output file. Raw logs are stored
// outside the database but stay inspectable (spec §31).
func (s *Server) handleExecutionLog(w http.ResponseWriter, r *http.Request) {
	path, err := s.svc.Executions.Log(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if path == "" {
		s.writeJSON(w, http.StatusOK, map[string]any{"available": false, "content": ""})
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]any{"available": false, "path": path})
		return
	}
	const maxBytes = 2 << 20
	offset := int64(0)
	if info.Size() > maxBytes {
		offset = info.Size() - maxBytes
	}
	f, err := os.Open(path)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	defer f.Close()
	if _, err := f.Seek(offset, 0); err != nil {
		s.fail(w, r, err)
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	content := strings.ToValidUTF8(string(data), "\uFFFD")
	s.writeJSON(w, http.StatusOK, map[string]any{
		"available": true,
		"path":      path,
		"truncated": offset > 0,
		"content":   content,
	})
}

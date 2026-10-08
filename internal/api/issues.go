package api

import (
	"net/http"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

func (s *Server) handleListIssues(w http.ResponseWriter, r *http.Request) {
	issues, err := s.svc.Issues.List(r.Context(), repository.IssueFilter{
		ProjectID: pathValue(r, "id"),
		Status:    domain.IssueStatus(r.URL.Query().Get("status")),
		Limit:     queryInt(r, "limit", 0),
		Offset:    queryInt(r, "offset", 0),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(issues))
}

func (s *Server) handleCreateIssue(w http.ResponseWriter, r *http.Request) {
	var in app.CreateIssueInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	issue, err := s.svc.Issues.Create(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, issue)
}

func (s *Server) handleGetIssue(w http.ResponseWriter, r *http.Request) {
	issue, err := s.svc.Issues.Get(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, issue)
}

func (s *Server) handleUpdateIssue(w http.ResponseWriter, r *http.Request) {
	var in app.UpdateIssueInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	issue, err := s.svc.Issues.Update(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, issue)
}

func (s *Server) handleDeleteIssue(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Issues.Delete(r.Context(), pathValue(r, "id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListIssueTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.svc.Issues.Tasks(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.dependenciesOrEmpty(r, tasks)
	s.writeJSON(w, http.StatusOK, listOf(tasks))
}

func (s *Server) handleCreateIssueTask(w http.ResponseWriter, r *http.Request) {
	var in app.CreateTaskInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	task, err := s.svc.Tasks.Create(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, task)
}

func (s *Server) handleListProjectTasks(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.svc.Tasks.List(r.Context(), repository.TaskFilter{
		ProjectID: pathValue(r, "id"),
		IssueID:   r.URL.Query().Get("issue_id"),
		Statuses:  taskStatuses(queryList(r, "status")),
		Limit:     queryInt(r, "limit", 0),
		Offset:    queryInt(r, "offset", 0),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(tasks))
}

// dependenciesOrEmpty is a hook for future enrichment of task listings.
func (s *Server) dependenciesOrEmpty(*http.Request, []domain.Task) {}

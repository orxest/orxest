package api

import (
	"io"
	"net/http"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.svc.Projects.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(projects))
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var in app.CreateProjectInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	bundle, err := s.svc.Projects.Create(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, bundle)
}

func (s *Server) handleGetProject(w http.ResponseWriter, r *http.Request) {
	project, err := s.svc.Projects.Get(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleUpdateProject(w http.ResponseWriter, r *http.Request) {
	var in app.UpdateProjectInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	project, err := s.svc.Projects.Update(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, project)
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	removeWorktrees := queryBool(r, "remove_worktrees", false)
	if err := s.svc.Projects.Delete(r.Context(), pathValue(r, "id"), removeWorktrees); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleProjectStats(w http.ResponseWriter, r *http.Request) {
	stats, err := s.svc.Projects.Stats(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleRepositoryStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.svc.Projects.EnsureRepository(r.Context(), pathValue(r, "id"), false)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleEnsureRepository(w http.ResponseWriter, r *http.Request) {
	init := queryBool(r, "init", false)
	status, err := s.svc.Projects.EnsureRepository(r.Context(), pathValue(r, "id"), init)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Server) handleImportProject(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	bundle, result, err := s.svc.Projects.CreateFromConfig(r.Context(), body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"project": bundle, "import": result})
}

func (s *Server) handleImportProjectConfig(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	result, err := s.svc.Projects.ImportConfig(r.Context(), pathValue(r, "id"), body)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, result)
}

func (s *Server) handleValidateProjectConfig(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	file, err := app.ParseProjectFile(body)
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]any{"valid": false, "error": err.Error()})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"valid": true, "config": file})
}

func (s *Server) handleDecompose(w http.ResponseWriter, r *http.Request) {
	var in app.DecomposeInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	result, err := s.svc.Planner.Decompose(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleDependencyGraph(w http.ResponseWriter, r *http.Request) {
	graph, err := s.svc.Tasks.DependencyGraph(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"edges": graph})
}

// boardColumn is one column of the kanban board (spec §28).
type boardColumn struct {
	Key      string        `json:"key"`
	Title    string        `json:"title"`
	Statuses []string      `json:"statuses"`
	Tasks    []domain.Task `json:"tasks"`
}

type boardResponse struct {
	Project      domain.Project            `json:"project"`
	Workflow     *domain.Workflow          `json:"workflow,omitempty"`
	Columns      []boardColumn             `json:"columns"`
	Issues       []domain.Issue            `json:"issues"`
	Stats        *app.ProjectStats         `json:"stats"`
	Dependencies map[string][]string       `json:"dependencies"`
	Agents       []domain.ProjectAgentView `json:"agents"`
}

func (s *Server) handleBoard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	projectID := pathValue(r, "id")
	project, err := s.svc.Projects.Get(ctx, projectID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	tasks, err := s.svc.Tasks.List(ctx, repository.TaskFilter{ProjectID: projectID})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	issues, err := s.svc.Issues.List(ctx, repository.IssueFilter{ProjectID: projectID})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	stats, err := s.svc.Projects.Stats(ctx, projectID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	graph, err := s.svc.Tasks.DependencyGraph(ctx, projectID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	agents, err := s.svc.Agents.ListProjectAgents(ctx, projectID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	workflow, _ := s.svc.Workflows.Default(ctx, projectID)

	columns := []boardColumn{
		{Key: "backlog", Title: "Backlog", Statuses: []string{string(domain.TaskBacklog)}},
		{Key: "ready", Title: "Ready", Statuses: []string{string(domain.TaskReady)}},
		{Key: "in_progress", Title: "In Progress", Statuses: []string{string(domain.TaskQueued), string(domain.TaskRunning)}},
		{Key: "review", Title: "Review", Statuses: []string{string(domain.TaskReview)}},
		{Key: "blocked", Title: "Blocked", Statuses: []string{string(domain.TaskBlocked)}},
		{Key: "done", Title: "Done", Statuses: []string{string(domain.TaskDone)}},
		{Key: "failed", Title: "Failed", Statuses: []string{string(domain.TaskFailed)}},
		{Key: "cancelled", Title: "Cancelled", Statuses: []string{string(domain.TaskCancelled)}},
	}
	index := map[string]int{}
	for i, c := range columns {
		for _, st := range c.Statuses {
			index[st] = i
		}
	}
	for _, t := range tasks {
		if i, ok := index[string(t.Status)]; ok {
			columns[i].Tasks = append(columns[i].Tasks, t)
			continue
		}
		columns[0].Tasks = append(columns[0].Tasks, t)
	}
	s.writeJSON(w, http.StatusOK, boardResponse{
		Project:      *project,
		Workflow:     workflow,
		Columns:      columns,
		Issues:       issues,
		Stats:        stats,
		Dependencies: graph,
		Agents:       agents,
	})
}

// readBody reads a bounded request body, used for YAML configuration uploads.
func readBody(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, domain.Invalidf("body", "a request body is required")
	}
	defer r.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		return nil, domain.Invalidf("body", "reading request body: %s", err.Error())
	}
	if len(buf) == 0 {
		return nil, domain.Invalidf("body", "a request body is required")
	}
	if len(buf) > MaxBodyBytes {
		return nil, domain.Invalidf("body", "request body is too large")
	}
	return buf, nil
}

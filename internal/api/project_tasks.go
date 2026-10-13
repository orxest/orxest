package api

import (
	"net/http"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// userVisibleKindsV is the default project task listing: user-visible work plus
// the containers that group it. Internal architect planning records are
// deliberately excluded, because they are not work items.
func userVisibleKinds() []domain.TaskKind {
	return []domain.TaskKind{domain.TaskKindWork, domain.TaskKindContainer}
}

// handleListProjectTasks lists the tasks of a project. The listing is project
// scoped: there is no issue to filter by. Pass `planning=true` to list the
// internal planning records instead, or `kind=all` for everything.
func (s *Server) handleListProjectTasks(w http.ResponseWriter, r *http.Request) {
	filter := repository.TaskFilter{
		ProjectID: pathValue(r, "id"),
		Statuses:  taskStatuses(queryList(r, "status")),
		Limit:     queryInt(r, "limit", 0),
		Offset:    queryInt(r, "offset", 0),
	}
	switch {
	case r.URL.Query().Get("kind") == "all":
		// no kind restriction
	case r.URL.Query().Get("planning") == "true":
		filter.Kinds = []domain.TaskKind{domain.TaskKindDecomposition}
	default:
		filter.Kinds = userVisibleKinds()
	}
	tasks, err := s.svc.Tasks.List(r.Context(), filter)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(tasks))
}

// handleCreateProjectTask creates a task directly in a project. This is the
// manual path: a title and useful details are enough, and no issue, parent or
// mandatory category is required.
func (s *Server) handleCreateProjectTask(w http.ResponseWriter, r *http.Request) {
	var in app.CreateTaskInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Source == "" {
		in.Source = domain.SourceManual
	}
	task, err := s.svc.Tasks.Create(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, task)
}

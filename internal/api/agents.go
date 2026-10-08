package api

import (
	"net/http"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	var enabled *bool
	switch r.URL.Query().Get("enabled") {
	case "true":
		v := true
		enabled = &v
	case "false":
		v := false
		enabled = &v
	}
	agents, err := s.svc.Agents.ListAgents(r.Context(), repository.AgentFilter{
		Enabled: enabled,
		Harness: r.URL.Query().Get("harness"),
		Limit:   queryInt(r, "limit", 0),
		Offset:  queryInt(r, "offset", 0),
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(agents))
}

func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	agent, err := s.svc.Agents.GetAgent(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleCreateAgent(w http.ResponseWriter, r *http.Request) {
	var in app.AgentInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	agent, err := s.svc.Agents.CreateAgent(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, agent)
}

func (s *Server) handleUpdateAgent(w http.ResponseWriter, r *http.Request) {
	var in app.AgentInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	agent, err := s.svc.Agents.UpdateAgent(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, agent)
}

func (s *Server) handleDeleteAgent(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Agents.DeleteAgent(r.Context(), pathValue(r, "id")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := s.svc.Agents.ListRoles(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(roles))
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var in app.RoleInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	role, err := s.svc.Agents.CreateRole(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleListProjectAgents(w http.ResponseWriter, r *http.Request) {
	agents, err := s.svc.Agents.ListProjectAgents(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(agents))
}

func (s *Server) handleAssignAgent(w http.ResponseWriter, r *http.Request) {
	var in app.AssignmentInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	pa, err := s.svc.Agents.AssignAgent(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, pa)
}

func (s *Server) handleUpdateAssignment(w http.ResponseWriter, r *http.Request) {
	var in app.AssignmentInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	pa, err := s.svc.Agents.UpdateAssignment(r.Context(), pathValue(r, "assignmentId"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, pa)
}

func (s *Server) handleDeleteAssignment(w http.ResponseWriter, r *http.Request) {
	if err := s.svc.Agents.DeleteAssignment(r.Context(), pathValue(r, "assignmentId")); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleWorkflowTemplates(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, listOf(s.svc.Workflows.Templates()))
}

func (s *Server) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	workflows, err := s.svc.Workflows.List(r.Context(), pathValue(r, "id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(workflows))
}

// handleReplaceWorkflow replaces the project's default workflow definition
// (spec §42: PUT /api/projects/{id}/workflow).
func (s *Server) handleReplaceWorkflow(w http.ResponseWriter, r *http.Request) {
	var in domain.Workflow
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	workflow, err := s.svc.Workflows.Replace(r.Context(), pathValue(r, "id"), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, workflow)
}

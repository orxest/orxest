package api

import (
	"net/http"
	"runtime"
	"time"

	"github.com/orxest/orxest/internal/domain"
)

// Version is the Orxest build version reported by the API.
var Version = "0.1.0-dev"

type healthResponse struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	Uptime    string    `json:"uptime"`
	Time      time.Time `json:"time"`
	Database  string    `json:"database"`
	GoVersion string    `json:"go_version"`
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	status := "ok"
	if _, err := s.svc.Projects.List(ctx); err != nil {
		status = "degraded"
	}
	s.writeJSON(w, http.StatusOK, healthResponse{
		Status:    status,
		Version:   Version,
		Uptime:    time.Since(s.started).Round(time.Second).String(),
		Time:      time.Now().UTC(),
		Database:  s.cfg.Database.Path,
		GoVersion: runtime.Version(),
	})
}

// metaResponse describes the deployment so the UI can render configured choices
// without hard-coding them.
type metaResponse struct {
	Version    string                    `json:"version"`
	Harnesses  []string                  `json:"harnesses"`
	Roles      []domain.Role             `json:"roles"`
	Templates  []domain.WorkflowTemplate `json:"workflow_templates"`
	Decision   decisionMeta              `json:"decision"`
	Limits     limitsMeta                `json:"limits"`
	EventTypes []string                  `json:"event_types"`
}

type decisionMeta struct {
	Provider string `json:"provider"`
	Enabled  bool   `json:"enabled"`
}

type limitsMeta struct {
	GlobalMaxConcurrentExecutions int `json:"global_max_concurrent_executions"`
	ExecutionTimeoutSeconds       int `json:"execution_timeout_seconds"`
	PollIntervalMillis            int `json:"poll_interval_ms"`
}

func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	roles, err := s.svc.Agents.ListRoles(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	decision := s.svc.Deps.Decision
	decisionName := "disabled"
	enabled := false
	if decision != nil {
		decisionName = decision.Name()
		enabled = decision.Enabled()
	}
	s.writeJSON(w, http.StatusOK, metaResponse{
		Version:   Version,
		Harnesses: s.svc.Deps.HarnessNames(),
		Roles:     roles,
		Templates: s.svc.Workflows.Templates(),
		Decision:  decisionMeta{Provider: decisionName, Enabled: enabled},
		Limits: limitsMeta{
			GlobalMaxConcurrentExecutions: s.cfg.Orchestration.MaxConcurrentExecutions,
			ExecutionTimeoutSeconds:       int(s.cfg.Orchestration.ExecutionTimeout.Seconds()),
			PollIntervalMillis:            int(s.cfg.Orchestration.PollInterval.Milliseconds()),
		},
		EventTypes: eventTypes(),
	})
}

func eventTypes() []string {
	return []string{
		domain.EventProjectCreated, domain.EventProjectUpdated,
		domain.EventIssueCreated, domain.EventIssueUpdated,
		domain.EventTaskCreated, domain.EventTaskUpdated, domain.EventTaskReady,
		domain.EventTaskQueued, domain.EventTaskStarted, domain.EventTaskProgress,
		domain.EventTaskCompleted, domain.EventTaskFailed, domain.EventTaskBlocked,
		domain.EventTaskRework, domain.EventTaskCancelled, domain.EventTaskReview,
		domain.EventExecutionStarted, domain.EventExecutionOutput,
		domain.EventExecutionCompleted, domain.EventExecutionFailed,
		domain.EventAgentAvailable, domain.EventAgentBusy,
		domain.EventWorkflowUpdated, domain.EventAgentConfigured, domain.EventDecision,
	}
}

func (s *Server) handleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	status := s.svc.Scheduler.Status(r.Context())
	s.writeJSON(w, http.StatusOK, status)
}

// handleSchedulerTick forces one scheduling pass. It is the manual counterpart
// of the background loop and is used by tests and operators.
func (s *Server) handleSchedulerTick(w http.ResponseWriter, r *http.Request) {
	dispatched, err := s.svc.Scheduler.Tick(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"dispatched": dispatched})
}

func (s *Server) handleRecentEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.Events.Recent(r.Context(), queryInt(r, "limit", 100))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(events))
}

func (s *Server) handleProjectEvents(w http.ResponseWriter, r *http.Request) {
	events, err := s.svc.Events.ByProject(r.Context(), pathValue(r, "id"),
		queryInt(r, "limit", 100), queryInt(r, "offset", 0))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.writeJSON(w, http.StatusOK, listOf(events))
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, OpenAPIDocument(s.cfg.Server.BasePath))
}

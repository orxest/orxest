package app

import (
	"context"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/domain/repository"
)

// IssueService owns product level work items (spec §6).
type IssueService struct {
	deps  Deps
	tasks *TaskService
}

// NewIssueService creates the service.
func NewIssueService(deps Deps, tasks *TaskService) *IssueService {
	return &IssueService{deps: deps, tasks: tasks}
}

// CreateIssueInput is the issue creation request.
type CreateIssueInput struct {
	Title              string             `json:"title"`
	Description        string             `json:"description"`
	Priority           int                `json:"priority"`
	Labels             []string           `json:"labels"`
	AcceptanceCriteria string             `json:"acceptance_criteria"`
	Status             domain.IssueStatus `json:"status"`
}

// Create creates an issue in a project.
func (s *IssueService) Create(ctx context.Context, projectID string, in CreateIssueInput) (*domain.Issue, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, projectID); err != nil {
		return nil, err
	}
	now := s.deps.now()
	issue := &domain.Issue{
		ID:                 domain.NewID(domain.IDPrefixIssue),
		ProjectID:          projectID,
		Title:              strings.TrimSpace(in.Title),
		Description:        in.Description,
		Priority:           in.Priority,
		Status:             in.Status,
		Labels:             in.Labels,
		AcceptanceCriteria: in.AcceptanceCriteria,
		Source:             domain.SourceManual,
		CreatedAt:          now,
		UpdatedAt:          now,
	}
	if issue.Status == "" {
		issue.Status = domain.IssueStatusOpen
	}
	if err := issue.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Issues().Create(ctx, issue); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: projectID,
		IssueID:   issue.ID,
		Type:      domain.EventIssueCreated,
		Message:   "issue " + issue.Title + " created",
		Payload:   map[string]any{"priority": issue.Priority, "source": issue.Source},
		CreatedAt: now,
	})
	return issue, nil
}

// Get returns an issue.
func (s *IssueService) Get(ctx context.Context, id string) (*domain.Issue, error) {
	return s.deps.Store.Issues().Get(ctx, id)
}

// List returns issues matching a filter.
func (s *IssueService) List(ctx context.Context, f repository.IssueFilter) ([]domain.Issue, error) {
	return s.deps.Store.Issues().List(ctx, f)
}

// UpdateIssueInput is a partial issue update.
type UpdateIssueInput struct {
	Title              *string             `json:"title"`
	Description        *string             `json:"description"`
	Priority           *int                `json:"priority"`
	Status             *domain.IssueStatus `json:"status"`
	Labels             *[]string           `json:"labels"`
	AcceptanceCriteria *string             `json:"acceptance_criteria"`
}

// Update applies a partial update.
func (s *IssueService) Update(ctx context.Context, id string, in UpdateIssueInput) (*domain.Issue, error) {
	issue, err := s.deps.Store.Issues().Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if in.Title != nil {
		issue.Title = strings.TrimSpace(*in.Title)
	}
	if in.Description != nil {
		issue.Description = *in.Description
	}
	if in.Priority != nil {
		issue.Priority = *in.Priority
	}
	if in.Status != nil {
		issue.Status = *in.Status
	}
	if in.Labels != nil {
		issue.Labels = *in.Labels
	}
	if in.AcceptanceCriteria != nil {
		issue.AcceptanceCriteria = *in.AcceptanceCriteria
	}
	issue.UpdatedAt = s.deps.now()
	if err := issue.Validate(); err != nil {
		return nil, err
	}
	if err := s.deps.Store.Issues().Update(ctx, issue); err != nil {
		return nil, err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: issue.ProjectID,
		IssueID:   issue.ID,
		Type:      domain.EventIssueUpdated,
		Message:   "issue " + issue.Title + " updated",
		Payload:   map[string]any{"status": string(issue.Status)},
		CreatedAt: issue.UpdatedAt,
	})
	return issue, nil
}

// Delete removes an issue and, by cascade, its tasks.
func (s *IssueService) Delete(ctx context.Context, id string) error {
	issue, err := s.deps.Store.Issues().Get(ctx, id)
	if err != nil {
		return err
	}
	tasks, err := s.deps.Store.Tasks().List(ctx, repository.TaskFilter{IssueID: id})
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.Status == domain.TaskRunning || t.Status == domain.TaskQueued {
			return domain.Conflictf("issue has a running task (%s); cancel it first", t.Title)
		}
	}
	if err := s.deps.Store.Issues().Delete(ctx, id); err != nil {
		return err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: issue.ProjectID,
		IssueID:   issue.ID,
		Type:      domain.EventIssueUpdated,
		Message:   "issue deleted",
		CreatedAt: s.deps.now(),
	})
	return nil
}

// Tasks returns the tasks of an issue.
func (s *IssueService) Tasks(ctx context.Context, id string) ([]domain.Task, error) {
	return s.deps.Store.Tasks().List(ctx, repository.TaskFilter{IssueID: id})
}

// RecomputeStatus derives the issue status from its tasks. The issue status is
// a projection of orchestration state, not an independent variable (spec §28).
func (s *IssueService) RecomputeStatus(ctx context.Context, issueID string) error {
	if issueID == "" {
		return nil
	}
	issue, err := s.deps.Store.Issues().Get(ctx, issueID)
	if err != nil {
		return err
	}
	if issue.Status == domain.IssueStatusCancelled {
		return nil
	}
	tasks, err := s.deps.Store.Tasks().List(ctx, repository.TaskFilter{IssueID: issueID})
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		return nil
	}
	status := domain.IssueStatusDone
	anyActive := false
	allDone := true
	for _, t := range tasks {
		switch t.Status {
		case domain.TaskDone:
		case domain.TaskCancelled:
			// Cancelled tasks do not keep an issue open forever.
		default:
			allDone = false
			if t.Status != domain.TaskBacklog {
				anyActive = true
			}
		}
	}
	switch {
	case allDone:
		status = domain.IssueStatusDone
	case anyActive:
		status = domain.IssueStatusInProgress
	default:
		status = domain.IssueStatusOpen
	}
	if status == issue.Status {
		return nil
	}
	issue.Status = status
	issue.UpdatedAt = s.deps.now()
	if err := s.deps.Store.Issues().Update(ctx, issue); err != nil {
		return err
	}
	s.deps.publish(ctx, domain.Event{
		ProjectID: issue.ProjectID,
		IssueID:   issue.ID,
		Type:      domain.EventIssueUpdated,
		Message:   "issue " + issue.Title + " is " + string(status),
		Payload:   map[string]any{"status": string(status)},
		CreatedAt: issue.UpdatedAt,
	})
	return nil
}

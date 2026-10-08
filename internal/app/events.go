package app

import (
	"context"

	"github.com/orxest/orxest/internal/domain"
)

// EventService exposes the persisted orchestration activity feed (spec §29,
// §48).
type EventService struct {
	deps Deps
}

// NewEventService creates the service.
func NewEventService(deps Deps) *EventService { return &EventService{deps: deps} }

// ByProject returns recent events of a project, newest first.
func (s *EventService) ByProject(ctx context.Context, projectID string, limit, offset int) ([]domain.Event, error) {
	if _, err := s.deps.Store.Projects().Get(ctx, projectID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	return s.deps.Store.Events().ListByProject(ctx, projectID, limit, offset)
}

// ByTask returns recent events of a task.
func (s *EventService) ByTask(ctx context.Context, taskID string, limit int) ([]domain.Event, error) {
	if _, err := s.deps.Store.Tasks().Get(ctx, taskID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	return s.deps.Store.Events().ListByTask(ctx, taskID, limit)
}

// Recent returns the most recent events across all projects.
func (s *EventService) Recent(ctx context.Context, limit int) ([]domain.Event, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.deps.Store.Events().ListRecent(ctx, limit)
}

// ExecutionEvents returns the persisted output log of an execution.
func (s *EventService) ExecutionEvents(ctx context.Context, executionID string, afterSeq, limit int) ([]domain.ExecutionEvent, error) {
	if _, err := s.deps.Store.Executions().Get(ctx, executionID); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 1000
	}
	return s.deps.Store.Executions().ListEvents(ctx, executionID, afterSeq, limit)
}

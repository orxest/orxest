package repository

import (
	"context"
	"time"

	"github.com/orxest/orxest/internal/domain"
)

// ExecutionFilter narrows an execution listing.
type ExecutionFilter struct {
	ProjectID string
	TaskID    string
	AgentID   string
	Status    domain.ExecutionStatus
	Statuses  []domain.ExecutionStatus
	Limit     int
	Offset    int
	// Since limits the result to executions created after the given time.
	Since time.Time
}

// ExecutionRepository persists the append-only execution history and the
// per-execution event log.
type ExecutionRepository interface {
	Create(ctx context.Context, e *domain.Execution) error
	Get(ctx context.Context, id string) (*domain.Execution, error)
	// Update persists lifecycle changes of an execution. Append-only columns
	// (id, task_id, created_at, attempt) are never modified.
	Update(ctx context.Context, e *domain.Execution) error
	List(ctx context.Context, f ExecutionFilter) ([]domain.Execution, error)
	// CountActiveByAgent returns the number of non-terminal executions per agent.
	CountActiveByAgent(ctx context.Context) (map[string]int, error)
	// CountActiveByProject returns the number of non-terminal executions per project.
	CountActiveByProject(ctx context.Context) (map[string]int, error)

	AppendEvent(ctx context.Context, e *domain.ExecutionEvent) error
	ListEvents(ctx context.Context, executionID string, afterSeq, limit int) ([]domain.ExecutionEvent, error)
	ListEventsByProject(ctx context.Context, projectID string, afterSeq, limit int) ([]domain.ExecutionEvent, error)
	MaxEventSeq(ctx context.Context, executionID string) (int, error)
}

// EventRepository persists orchestration events for the activity feed.
type EventRepository interface {
	Append(ctx context.Context, e *domain.Event) error
	ListByProject(ctx context.Context, projectID string, limit, offset int) ([]domain.Event, error)
	ListByTask(ctx context.Context, taskID string, limit int) ([]domain.Event, error)
	ListRecent(ctx context.Context, limit int) ([]domain.Event, error)
}

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

// ExecutionTotals aggregates the runtime cost of a project's executions: how
// long agents worked and what they consumed while doing it. It is computed over
// the append-only history, so it can never drift from the executions themselves.
type ExecutionTotals struct {
	// Executions counts every recorded execution, including the internal
	// architect planning runs.
	Executions int `json:"executions"`
	// Finished counts the executions that reached a terminal status.
	Finished int `json:"finished"`
	// DurationMs is the summed wall-clock time of every finished execution. It
	// is agent time, not project wall time: executions that ran in parallel each
	// count their own span.
	DurationMs int64 `json:"duration_ms"`
	// Token usage summed across executions, with the cache and reasoning subsets
	// and the provider-reported cost when a harness supplies them.
	InputTokens      int64   `json:"input_tokens"`
	OutputTokens     int64   `json:"output_tokens"`
	TotalTokens      int64   `json:"total_tokens"`
	CacheReadTokens  int64   `json:"cache_read_tokens"`
	CacheWriteTokens int64   `json:"cache_write_tokens"`
	ReasoningTokens  int64   `json:"reasoning_tokens"`
	Steps            int64   `json:"steps"`
	CostUSD          float64 `json:"cost_usd"`
}

// Add accumulates one execution's contribution into the totals.
func (t *ExecutionTotals) Add(e domain.Execution) {
	t.Executions++
	if e.Status.Terminal() {
		t.Finished++
	}
	if e.StartedAt != nil && e.FinishedAt != nil {
		if ms := e.FinishedAt.Sub(*e.StartedAt).Milliseconds(); ms > 0 {
			t.DurationMs += ms
		}
	}
	t.InputTokens += e.Metrics.InputTokens
	t.OutputTokens += e.Metrics.OutputTokens
	t.TotalTokens += e.Metrics.TotalTokens
	t.CacheReadTokens += e.Metrics.CacheReadTokens
	t.CacheWriteTokens += e.Metrics.CacheWriteTokens
	t.ReasoningTokens += e.Metrics.ReasoningTokens
	t.Steps += int64(e.Metrics.Steps)
	t.CostUSD += e.Metrics.CostUSD
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
	// Totals aggregates execution time and token/cost usage for one project, or
	// for every project when projectID is empty.
	Totals(ctx context.Context, projectID string) (ExecutionTotals, error)

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

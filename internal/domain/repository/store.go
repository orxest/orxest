// Package repository declares the persistence contracts of Orxest.
//
// The interfaces are defined in terms of domain types only, so the application
// layer never depends on SQLite, and SQLite can be replaced without touching
// orchestration logic (spec §39).
package repository

import (
	"context"

	"github.com/orxest/orxest/internal/domain"
)

// Store is the complete persistence surface of Orxest.
type Store interface {
	Projects() ProjectRepository
	Repositories() RepositoryRepository
	Tasks() TaskRepository
	Roles() RoleRepository
	Agents() AgentRepository
	Workflows() WorkflowRepository
	Executions() ExecutionRepository
	Events() EventRepository
	// WithTx runs fn inside a single transaction. The Store handed to fn must be
	// used for all work inside the callback: it is bound to the transaction.
	WithTx(ctx context.Context, fn func(ctx context.Context, tx Store) error) error
	// Migrate applies all pending migrations.
	Migrate(ctx context.Context) error
	// Close releases the underlying database handle.
	Close() error
}

// ListOptions narrows a collection query. Zero values mean "no filter".
type ListOptions struct {
	Limit  int
	Offset int
}

// ProjectRepository persists projects.
type ProjectRepository interface {
	Create(ctx context.Context, p *domain.Project) error
	Get(ctx context.Context, id string) (*domain.Project, error)
	GetBySlug(ctx context.Context, slug string) (*domain.Project, error)
	List(ctx context.Context, opts ListOptions) ([]domain.Project, error)
	Update(ctx context.Context, p *domain.Project) error
	Delete(ctx context.Context, id string) error
}

// RepositoryRepository persists resolved Git state per project.
type RepositoryRepository interface {
	Upsert(ctx context.Context, r *domain.RepositoryState) error
	Get(ctx context.Context, projectID string) (*domain.RepositoryState, error)
}

// TaskDependencyStatus is a dependency edge joined with the dependency's state.
type TaskDependencyStatus struct {
	TaskID string            `json:"task_id"`
	Title  string            `json:"title"`
	Status domain.TaskStatus `json:"status"`
}

// TaskProgress is the derived progress of a container's descendant work tasks.
// It is computed on read and never stored on the container itself.
type TaskProgress struct {
	TaskID    string                    `json:"task_id"`
	Total     int                       `json:"total"`
	Done      int                       `json:"done"`
	Failed    int                       `json:"failed"`
	Cancelled int                       `json:"cancelled"`
	Progress  domain.DescendantProgress `json:"progress"`
}

// TaskFilter narrows a task listing, used by the board and the API.
type TaskFilter struct {
	ProjectID string
	// ParentTaskID filters by direct parent. When ParentSet is true an empty
	// ParentTaskID means "top-level tasks only".
	ParentTaskID string
	ParentSet    bool
	Status       domain.TaskStatus
	Statuses     []domain.TaskStatus
	// Kinds restricts the listing to structural task kinds. It is how the board
	// excludes internal decomposition tasks and how the scheduler excludes
	// non-executable containers.
	Kinds []domain.TaskKind
	// RootsOnly restricts the listing to tasks without a parent.
	RootsOnly bool
	Limit     int
	Offset    int
	// OrderByCreated orders by creation time ascending (deterministic default).
	OrderByCreated bool
}

// TaskRepository persists tasks and their dependency graph.
type TaskRepository interface {
	Create(ctx context.Context, t *domain.Task) error
	Get(ctx context.Context, id string) (*domain.Task, error)
	List(ctx context.Context, f TaskFilter) ([]domain.Task, error)
	Update(ctx context.Context, t *domain.Task) error
	Delete(ctx context.Context, id string) error

	// AddDependency records that taskID depends on dependsOnTaskID. Implementations
	// must reject self-dependencies and duplicates.
	AddDependency(ctx context.Context, taskID, dependsOnTaskID string) error
	RemoveDependency(ctx context.Context, taskID, dependsOnTaskID string) error
	// Dependencies returns the ids taskID depends on.
	Dependencies(ctx context.Context, taskID string) ([]string, error)
	// Dependents returns the ids that depend on taskID.
	Dependents(ctx context.Context, taskID string) ([]string, error)
	// DependenciesFor returns the dependency edges for a set of tasks.
	DependenciesFor(ctx context.Context, taskIDs []string) (map[string][]string, error)
	// Reachable reports whether `to` is reachable from `from` following
	// dependency edges. It is used to reject cycles before they are created.
	Reachable(ctx context.Context, from, to string) (bool, error)
	// DependencyStatuses returns the status of every direct dependency of a
	// task. The scheduler uses it to decide readiness and to explain blocking.
	DependencyStatuses(ctx context.Context, taskID string) ([]TaskDependencyStatus, error)

	// Children returns the direct children of a task, ordered deterministically.
	Children(ctx context.Context, parentTaskID string) ([]domain.Task, error)
	// Ancestors returns the container chain above a task, outermost first.
	Ancestors(ctx context.Context, taskID string) ([]domain.Task, error)
	// Descendants returns every task below a task, excluding the task itself.
	Descendants(ctx context.Context, taskID string) ([]domain.Task, error)
	// Progress returns derived progress for a task from its descendant work
	// tasks. Containers have no execution of their own, so this is a read model.
	Progress(ctx context.Context, taskID string) (TaskProgress, error)
	// ReachableByParent reports whether `to` is reachable from `from` following
	// parent edges. It is used to reject hierarchy cycles.
	ReachableByParent(ctx context.Context, from, to string) (bool, error)
	// CountChildren returns how many direct children a task has.
	CountChildren(ctx context.Context, taskID string) (int, error)

	CountByStatus(ctx context.Context, projectID string) (map[domain.TaskStatus]int, error)
	// CountExecutableByStatus counts only executable work tasks, so containers
	// and internal decomposition tasks never appear as project work.
	CountExecutableByStatus(ctx context.Context, projectID string) (map[domain.TaskStatus]int, error)
	// NextOrderIndex returns the next ordering slot inside a project. When
	// parentTaskID is set the slot is scoped to that container's children.
	NextOrderIndex(ctx context.Context, projectID, parentTaskID string) (int, error)
}

package domain

import (
	"strings"
	"time"
)

// IssueStatus is the lifecycle of a product level work item.
type IssueStatus string

const (
	IssueStatusOpen       IssueStatus = "open"
	IssueStatusInProgress IssueStatus = "in_progress"
	IssueStatusDone       IssueStatus = "done"
	IssueStatusCancelled  IssueStatus = "cancelled"
)

// SourceManual and SourceArchitect record how an issue was created. Architect
// generated issues are always user visible as such (spec §16).
const (
	SourceManual    = "manual"
	SourceArchitect = "architect"
)

// Priority levels. Higher numbers run first. The values are intentionally
// coarse so that scheduling stays deterministic and explainable.
const (
	PriorityLow      = 0
	PriorityNormal   = 1
	PriorityHigh     = 2
	PriorityCritical = 3
)

// Issue is a product level unit of work that groups one or more tasks.
type Issue struct {
	ID          string      `json:"id"`
	ProjectID   string      `json:"project_id"`
	Title       string      `json:"title"`
	Description string      `json:"description"`
	Priority    int         `json:"priority"`
	Status      IssueStatus `json:"status"`
	Labels      []string    `json:"labels"`
	// AcceptanceCriteria is free text that is copied into the prompt context of
	// the tasks belonging to the issue.
	AcceptanceCriteria string `json:"acceptance_criteria"`
	Source             string `json:"source"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Validate checks the structural invariants of an issue.
func (i *Issue) Validate() error {
	if strings.TrimSpace(i.Title) == "" {
		return Invalidf("title", "must not be empty")
	}
	if len(i.Title) > 300 {
		return Invalidf("title", "must be at most 300 characters")
	}
	if i.Status == "" {
		i.Status = IssueStatusOpen
	}
	if !validIssueStatus(i.Status) {
		return Invalidf("status", "unknown status %q", i.Status)
	}
	if i.Priority < 0 || i.Priority > PriorityCritical {
		return Invalidf("priority", "must be between 0 and %d", PriorityCritical)
	}
	if i.Source == "" {
		i.Source = SourceManual
	}
	return nil
}

func validIssueStatus(s IssueStatus) bool {
	switch s {
	case IssueStatusOpen, IssueStatusInProgress, IssueStatusDone, IssueStatusCancelled:
		return true
	}
	return false
}

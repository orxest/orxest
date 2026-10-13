package domain

// This file holds the task classification and origin constants that used to
// live on the removed Issue entity. There is exactly one user-facing work-item
// entity, Task, and these values are optional metadata on it: they never gate
// validation and never change how the scheduler routes work.

// SourceManual and SourceArchitect record how a task was created. Architect
// generated tasks are always user visible as such (spec §16). The source is
// metadata only.
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

// Common task labels. They are examples of optional classification, not a
// closed set: a project may use any label it likes, and a label never becomes a
// precondition for execution.
const (
	LabelBug           = "bug"
	LabelFeature       = "feature"
	LabelRefactoring   = "refactoring"
	LabelResearch      = "research"
	LabelDocumentation = "documentation"
)

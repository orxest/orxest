package app_test

import (
	"context"
	"sync"
	"testing"

	"github.com/orxest/orxest/internal/adapters/fake"
	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
	"github.com/orxest/orxest/internal/testsupport"
)

// recordingDecision answers every question with a fixed choice and records the
// requests, which is how these tests observe the boundary from the other side.
type recordingDecision struct {
	mu       sync.Mutex
	choice   string
	requests []ports.DecisionRequest
	enabled  bool
}

func (d *recordingDecision) Name() string  { return "recording" }
func (d *recordingDecision) Enabled() bool { return d.enabled }

func (d *recordingDecision) Decide(_ context.Context, req ports.DecisionRequest) (ports.DecisionResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.requests = append(d.requests, req)
	return ports.DecisionResult{Choice: d.choice, Confidence: 0.9, Rationale: "test"}, nil
}

func (d *recordingDecision) kinds() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	seen := []string{}
	for _, r := range d.requests {
		seen = append(seen, r.Kind)
	}
	return seen
}

// TestDecisionProviderOnlyRecommends documents spec §33/§54: when enabled, a
// provider may reorder ready work and pick between available agents, but an
// answer outside the offered options is ignored and orchestration continues
// deterministically.
func TestDecisionProviderOnlyRecommends(t *testing.T) {
	repo := testsupport.Repository(t, "main")
	harness := fake.New(fake.WithoutWork())
	provider := &recordingDecision{enabled: true, choice: "not-an-option"}
	svc := testsupport.Service(t, testsupport.Options{
		Harnesses: harnesses(harness),
		Decision:  provider,
	})
	project, err := svc.Projects.Create(context.Background(), app.CreateProjectInput{
		Name:             "Decision provider",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents: []app.CreateProjectAgentInput{
			{Name: "first", Harness: "fake", Role: domain.RoleDeveloper, Priority: 10},
			{Name: "second", Harness: "fake", Role: domain.RoleDeveloper, Priority: 5},
		},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	first := newIssueAndTask(t, svc, &project.Project, "First task", nil)
	_ = newIssueAndTask(t, svc, &project.Project, "Second task", nil)

	// The task must still complete even though the recommendation is invalid.
	drive(t, svc, "the task to finish despite an invalid recommendation", func() bool {
		return loadTask(t, svc, first.ID).Status == domain.TaskDone
	})

	kinds := provider.kinds()
	if len(kinds) == 0 {
		t.Fatal("expected the provider to be consulted")
	}
	consultedAgentSelection := false
	for _, kind := range kinds {
		if kind == ports.DecisionAgentSelection {
			consultedAgentSelection = true
		}
	}
	if !consultedAgentSelection {
		t.Errorf("expected an agent selection question with two candidates for the role, saw %v", kinds)
	}

	// Every recommendation is recorded for observability, with applied=false
	// because the choice was not one of the offered options.
	events, err := svc.Events.ByProject(context.Background(), project.Project.ID, 100, 0)
	if err != nil {
		t.Fatalf("listing events: %v", err)
	}
	seen := false
	for _, evt := range events {
		if evt.Type == domain.EventDecision {
			seen = true
			if applied, ok := evt.Payload["applied"].(bool); !ok || applied {
				t.Errorf("expected applied=false for an invalid recommendation, got %+v", evt.Payload)
			}
		}
	}
	if !seen {
		t.Error("expected decision.made events to be recorded")
	}
}

func TestDisabledDecisionProviderIsNeverConsulted(t *testing.T) {
	repo := testsupport.Repository(t, "main")
	provider := &recordingDecision{enabled: false, choice: "whatever"}
	svc := testsupport.Service(t, testsupport.Options{
		Harnesses: harnesses(fake.New(fake.WithoutWork())),
		Decision:  provider,
	})
	project, err := svc.Projects.Create(context.Background(), app.CreateProjectInput{
		Name:             "No decisions",
		RepositoryPath:   repo,
		TargetBranch:     "main",
		WorkflowTemplate: "minimal",
		Agents:           []app.CreateProjectAgentInput{{Name: "dev", Harness: "fake", Role: domain.RoleDeveloper}},
	})
	if err != nil {
		t.Fatalf("creating project: %v", err)
	}
	task := newIssueAndTask(t, svc, &project.Project, "Do the work", nil)
	drive(t, svc, "the task to finish", func() bool {
		return loadTask(t, svc, task.ID).Status == domain.TaskDone
	})
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if len(provider.requests) != 0 {
		t.Errorf("a disabled provider must never be consulted, saw %d requests", len(provider.requests))
	}
}

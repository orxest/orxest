package app

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/orxest/orxest/internal/domain"
	"github.com/orxest/orxest/internal/ports"
)

// PromptInput is the focused context used to render an execution prompt.
// Spec §20: only what the task needs is sent to an agent — never the whole
// project database.
type PromptInput struct {
	Project      domain.Project
	Issue        *domain.Issue
	Task         domain.Task
	Workflow     domain.Workflow
	Step         domain.WorkflowStep
	Role         domain.Role
	Agent        domain.Agent
	Dependencies []ports.TaskSummary
	WorktreePath string
	BranchName   string
	TargetBranch string
	// PreviousSummary describes the previous execution of this task, which is
	// how a developer learns why tests failed before reworking.
	PreviousSummary string
	PreviousError   string
	// Reports lists recent execution summaries of this task, newest first.
	Reports []ExecutionReport
}

// ExecutionReport is a compact view of a previous execution, used to give
// rework executions the context they need.
type ExecutionReport struct {
	Step    string
	Agent   string
	Outcome string
	Summary string
	Error   string
	Attempt int
	// FailureKind classifies a failed execution (integration conflicts carry
	// FailureIntegration), which is how a re-run learns *why* it is running.
	FailureKind string
}

// BuildPrompt renders the instruction text for one execution. The prompt is
// deterministic so that prompt changes are reviewable.
func BuildPrompt(in PromptInput) string {
	var b strings.Builder
	roleName := in.Role.Name
	if roleName == "" {
		roleName = in.Step.RoleID
	}
	fmt.Fprintf(&b, "# Orxest task execution\n\n")
	fmt.Fprintf(&b, "You are the **%s** working on one task inside the Orxest orchestration platform.\n", roleName)
	fmt.Fprintf(&b, "Orxest decides how work moves through the workflow; you perform this one stage well and report a structured result.\n\n")

	fmt.Fprintf(&b, "## Project\n\n")
	fmt.Fprintf(&b, "- Name: %s\n", in.Project.Name)
	fmt.Fprintf(&b, "- Repository: %s\n", in.Project.RepositoryPath)
	fmt.Fprintf(&b, "- Target branch: %s\n", in.TargetBranch)
	if in.Project.Description != "" {
		fmt.Fprintf(&b, "- Description: %s\n", oneLine(in.Project.Description))
	}
	fmt.Fprintf(&b, "\n## Workspace\n\n")
	fmt.Fprintf(&b, "- Working directory: %s\n", in.WorktreePath)
	fmt.Fprintf(&b, "- Your branch: %s\n", in.BranchName)
	if integrationConflict(in) != "" {
		// The generic "never merge" rule would contradict the conflict
		// resolution instructions, so state the single exception explicitly.
		fmt.Fprintf(&b, "- This is an isolated Git worktree dedicated to this task. Do not switch branches, do not push, and do not touch other worktrees. The one exception is the merge of `%s` required to resolve the integration conflict described below.\n", in.TargetBranch)
	} else {
		fmt.Fprintf(&b, "- This is an isolated Git worktree dedicated to this task. Do not switch branches, do not merge, do not push, and do not touch other worktrees.\n")
	}

	fmt.Fprintf(&b, "\n## Task\n\n")
	fmt.Fprintf(&b, "- Task id: %s\n", in.Task.ID)
	fmt.Fprintf(&b, "- Title: %s\n", in.Task.Title)
	fmt.Fprintf(&b, "- Priority: %d (0 low … 3 critical)\n", in.Task.Priority)
	fmt.Fprintf(&b, "- Workflow: %s\n", in.Workflow.Name)
	fmt.Fprintf(&b, "- Current step: %s (role: %s)\n", in.Step.Name, in.Step.RoleID)
	fmt.Fprintf(&b, "- Execution attempt: %d of this step\n", max(in.Task.AttemptCount, 1))
	fmt.Fprintf(&b, "\n### Task description\n\n%s\n", strings.TrimSpace(in.Task.Description))
	if strings.TrimSpace(in.Task.AcceptanceCriteria) != "" {
		fmt.Fprintf(&b, "\n### Acceptance criteria\n\n%s\n", strings.TrimSpace(in.Task.AcceptanceCriteria))
	}
	if in.Issue != nil {
		fmt.Fprintf(&b, "\n## Issue context\n\n")
		fmt.Fprintf(&b, "- Title: %s\n", in.Issue.Title)
		fmt.Fprintf(&b, "- Description: %s\n", oneLine(in.Issue.Description))
		if strings.TrimSpace(in.Issue.AcceptanceCriteria) != "" {
			fmt.Fprintf(&b, "- Issue acceptance criteria: %s\n", oneLine(in.Issue.AcceptanceCriteria))
		}
	}
	if len(in.Dependencies) > 0 {
		fmt.Fprintf(&b, "\n## Completed dependencies\n\n")
		for _, d := range in.Dependencies {
			fmt.Fprintf(&b, "- %s (%s): %s", d.Title, d.Status, oneLine(d.AcceptanceCriteria))
			if d.Summary != "" {
				fmt.Fprintf(&b, " — %s", oneLine(d.Summary))
			}
			fmt.Fprintf(&b, "\n")
		}
	}
	if len(in.Reports) > 0 {
		fmt.Fprintf(&b, "\n## Previous executions of this task\n\n")
		for _, r := range in.Reports {
			fmt.Fprintf(&b, "- attempt %d, step %s, agent %s: %s", r.Attempt, r.Step, r.Agent, r.Outcome)
			if r.Summary != "" {
				fmt.Fprintf(&b, " — %s", oneLine(r.Summary))
			}
			if r.Error != "" {
				fmt.Fprintf(&b, " (error: %s)", oneLine(r.Error))
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	if conflict := integrationConflict(in); conflict != "" {
		fmt.Fprintf(&b, "\n## Integration conflict to resolve\n\n%s\n", conflict)
	}

	if strings.TrimSpace(in.Step.Instructions) != "" {
		fmt.Fprintf(&b, "\n## Stage instructions\n\n%s\n", strings.TrimSpace(in.Step.Instructions))
	}
	if strings.TrimSpace(in.Agent.Instructions) != "" {
		fmt.Fprintf(&b, "\n## Agent instructions\n\n%s\n", strings.TrimSpace(in.Agent.Instructions))
	}

	fmt.Fprintf(&b, "\n## Role expectations\n\n%s\n", roleExpectations(in.Role.ID))
	fmt.Fprintf(&b, "\n## How to work\n\n%s", howToWork(in.Step))

	if in.Task.Kind == domain.TaskKindDecomposition {
		// A decomposition task has exactly one output: the plan. Emitting the
		// generic execution report contract as well would ask the agent for two
		// different JSON objects as its final message, and harnesses without
		// structured-output support (Pi, fake) will answer with the last one
		// they were told about.
		fmt.Fprintf(&b, "\n## Decomposition output\n\n%s\n", architectInstructions())
		fmt.Fprintf(&b, "\n## Required plan JSON\n\n%s\n", architectPlanContract())
		return b.String()
	}

	fmt.Fprintf(&b, "\n## Required final report\n\n%s\n", reportContract())
	return b.String()
}

// integrationConflict reports the merge conflict a previous execution left
// behind, so a re-run resolves it instead of re-implementing the task blindly.
func integrationConflict(in PromptInput) string {
	var conflict *ExecutionReport
	for i := range in.Reports {
		if in.Reports[i].FailureKind == string(domain.FailureIntegration) {
			conflict = &in.Reports[i]
			break
		}
	}
	if conflict == nil {
		return ""
	}
	lines := []string{
		"This task was sent back because its branch does not merge cleanly into `" + in.TargetBranch + "`.",
		"",
		"1. Run `git merge " + in.TargetBranch + "` in your working directory. It will report the conflicting files.",
		"2. Resolve every conflict by hand, keeping **both** sides' intent: your task's change and the change that already landed on `" + in.TargetBranch + "`.",
		"   Never delete someone else's work to make the merge succeed, and never abort the merge.",
		"3. Run the project's checks (build, tests, linter) again on the merged result and fix what they report.",
		"4. Commit the merge. The working tree must be clean when you finish.",
		"",
		"Everything else you were asked for in this step still applies.",
	}
	if conflict.Summary != "" {
		lines = append(lines, "", "The last integration attempt reported:", "", "> "+oneLine(conflict.Summary))
	}
	return strings.Join(lines, "\n")
}

func howToWork(step domain.WorkflowStep) string {
	return strings.Join([]string{
		"1. Inspect the repository and understand the existing conventions before changing anything.",
		"2. Make the smallest coherent change that satisfies the acceptance criteria.",
		"3. Run the relevant build, tests and linters yourself and fix what you break.",
		"4. Commit your work on the current branch with a descriptive message. Leave the working tree clean.",
		"5. Do not modify files outside this repository worktree.",
		"",
		"If this stage is verification or review, prefer running the real checks over reading the code, and report concrete evidence.",
	}, "\n")
}

func roleExpectations(roleID string) string {
	switch roleID {
	case domain.RoleArchitect:
		return "Produce a concrete technical design: components, interfaces, data model, failure handling and the order in which the work should be done. You do not implement the feature."
	case domain.RoleSeniorDeveloper:
		return "Implement the change end to end, including tests. Take responsibility for correctness, edge cases and backwards compatibility. Rework anything a later stage reports."
	case domain.RoleJuniorDeveloper:
		return "Implement the specified change following the existing patterns. Keep the diff focused and ask for nothing beyond the task description."
	case domain.RoleDeveloper:
		return "Implement the change end to end, including tests, following existing conventions."
	case domain.RoleTester:
		return "Try to falsify the implementation. Run the acceptance criteria as tests, add regression tests where behaviour is not covered, and report failures with the exact command and output."
	case domain.RoleReviewer:
		return "Review the diff against the acceptance criteria for correctness, clarity, security and maintainability. Report APPROVED only when you would merge it yourself."
	case domain.RoleSecurity:
		return "Review the change for security impact: input validation, authentication, authorisation, secrets handling, injection and dependency risk."
	default:
		return "Perform the stage described above to a high professional standard and report evidence."
	}
}

func architectInstructions() string {
	return strings.Join([]string{
		"You are executing a decomposition **planning** task, not an implementation task.",
		"",
		"Analyse the repository and the request above, then produce an issue and a set of tasks with",
		"dependencies and verifiable acceptance criteria. Every task must be independently executable",
		"by a single coding agent in one workflow run. Tasks must not assign agents: Orxest selects",
		"agents from roles. Do not decide scheduling or sequencing beyond `depends_on` and `order_index`.",
		"",
		"Do not change application source code. The only files you create are the plan files under",
		"`" + PlanDirRelativePath + "/` described below.",
	}, "\n")
}

// architectPlanContract is the decomposition counterpart of reportContract: it
// asks for exactly one JSON object, hands the agent the schema even when its
// harness cannot enforce one, and tells it to also persist the plan to a file so
// a harness that mangles the final message cannot lose the work.
func architectPlanContract() string {
	schema, err := json.MarshalIndent(domain.ArchitectPlanSchema(), "", "  ")
	schemaText := string(schema)
	if err != nil {
		schemaText = "{}"
	}
	return strings.Join([]string{
		"Your final message must be exactly one JSON object — the plan — and nothing else.",
		"Do **not** emit the usual `status`/`summary` execution report for this task: the plan",
		"JSON *is* the result Orxest reads. Do not wrap it in prose or a second JSON object.",
		"",
		"Before you finish, also write the identical JSON to `" + PlanFileRelativePath + "` inside",
		"the worktree and commit it. Orxest falls back to that file when a harness cannot return",
		"structured output reliably, so the plan is never lost.",
		"",
		"The object must match this JSON Schema:",
		"",
		"```json",
		schemaText,
		"```",
		"",
		"Rules:",
		"- `issue.title` and `issue.description` are required.",
		"- every task needs a unique plan-local `ref`, plus `title`, `description` and non-empty `acceptance_criteria`.",
		"- `depends_on` may only reference sibling `ref` values, and the graph must be acyclic.",
		"- do not assign agents, models, harnesses or roles: Orxest selects them.",
	}, "\n")
}

// reportContract renders the structured result contract. Review and security
// stages report `approved` or `changes_requested`; every other stage reports
// `success` or `failure`. Orxest interprets this report, which is the only way
// an agent influences the workflow (an agent can never change state itself).
func reportContract() string {
	return strings.Join([]string{
		"End your final message with exactly one JSON object, on its own, with this shape:",
		"",
		"```json",
		"{",
		`  "status": "success" | "failure" | "approved" | "changes_requested" | "blocked",`,
		`  "summary": "one short paragraph a human can read in the Orxest UI",`,
		`  "failure_kind": "agent_failure" | "task_failure" | "environment_failure",`,
		`  "evidence": ["exact command you ran and its result"],`,
		`  "changed_files": ["path/to/file"],`,
		`  "notes": "anything the next workflow stage must know"`,
		"}",
		"```",
		"",
		"Use `success` only when the stage goal is genuinely met and committed.",
		"Use `approved` when this is a review stage and the change is acceptable.",
		"Use `changes_requested` when this is a review stage and the implementation needs changes;",
		"Orxest routes the task back to the implementation step for rework.",
		"Use `failure` when the work is wrong or incomplete; set `failure_kind` accordingly.",
		"`failure_kind: environment_failure` means the tooling, dependencies or workspace prevented the work.",
		"Orxest interprets this report; you never change workflow state yourself.",
	}, "\n")
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}

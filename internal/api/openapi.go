package api

import (
	"reflect"
	"strings"
	"time"

	"github.com/orxest/orxest/internal/app"
	"github.com/orxest/orxest/internal/domain"
)

// OpenAPIDocument builds the OpenAPI 3.1 description of the Orxest API.
//
// The document is generated from the Go types themselves (see schemaOf), so the
// specification cannot silently drift away from the implementation. It is
// served at GET /api/openapi.json and can be written to disk with
// `orxest openapi > docs/openapi.json` (spec §42).
func OpenAPIDocument(basePath string) map[string]any {
	schemas := map[string]any{}
	for name, typ := range schemaTypes() {
		schemas[name] = schemaOf(reflect.TypeOf(typ))
	}
	schemas["Error"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"error": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"code":    map[string]any{"type": "string"},
					"message": map[string]any{"type": "string"},
					"field":   map[string]any{"type": "string"},
				},
				"required": []string{"code", "message"},
			},
		},
		"required": []string{"error"},
	}
	schemas["List"] = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"items": map[string]any{"type": "array", "items": map[string]any{}},
			"total": map[string]any{"type": "integer"},
		},
		"required": []string{"items", "total"},
	}
	return map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":   "Orxest API",
			"version": Version,
			"description": strings.Join([]string{
				"Orxest is a project-centric orchestration platform for AI-assisted software development.",
				"Agents perform work; Orxest decides how work moves through the system.",
				"Projects own issues, tasks and a workflow; the workflow selects role-based agents for each step,",
				"the scheduler runs them in isolated Git worktrees, and every attempt is preserved as an execution.",
			}, " "),
			"license": map[string]any{"name": "MIT"},
		},
		"servers": []map[string]any{{"url": basePath}},
		"tags": []map[string]any{
			{"name": "system", "description": "Health, metadata and scheduler control."},
			{"name": "projects", "description": "Project lifecycle, board and repository state."},
			{"name": "issues", "description": "Product level work items."},
			{"name": "tasks", "description": "Executable units of work, dependencies and human control."},
			{"name": "executions", "description": "Execution history, live output and raw logs."},
			{"name": "agents", "description": "Agent configurations, roles and per-project assignments."},
			{"name": "workflows", "description": "Workflow definitions and templates."},
			{"name": "events", "description": "Persisted orchestration activity and live streams."},
		},
		"paths": buildPaths(),
		"components": map[string]any{
			"schemas": schemas,
		},
	}
}

func schemaTypes() map[string]any {
	return map[string]any{
		"Project":            domain.Project{},
		"ProjectSettings":    domain.ProjectSettings{},
		"RepositoryState":    domain.RepositoryState{},
		"Issue":              domain.Issue{},
		"Task":               domain.Task{},
		"TaskDependency":     domain.TaskDependency{},
		"Execution":          domain.Execution{},
		"ExecutionEvent":     domain.ExecutionEvent{},
		"Event":              domain.Event{},
		"Agent":              domain.Agent{},
		"ProjectAgent":       domain.ProjectAgent{},
		"ProjectAgentView":   domain.ProjectAgentView{},
		"Role":               domain.Role{},
		"Workflow":           domain.Workflow{},
		"WorkflowStep":       domain.WorkflowStep{},
		"WorkflowTemplate":   domain.WorkflowTemplate{},
		"ArchitectPlan":      domain.ArchitectPlan{},
		"ArchitectIssue":     domain.ArchitectIssue{},
		"ArchitectTask":      domain.ArchitectTask{},
		"CreateProjectInput": app.CreateProjectInput{},
		"CreateIssueInput":   app.CreateIssueInput{},
		"CreateTaskInput":    app.CreateTaskInput{},
		"UpdateTaskInput":    app.UpdateTaskInput{},
		"UpdateIssueInput":   app.UpdateIssueInput{},
		"UpdateProjectInput": app.UpdateProjectInput{},
		"AgentInput":         app.AgentInput{},
		"AssignmentInput":    app.AssignmentInput{},
		"RoleInput":          app.RoleInput{},
		"RetryInput":         app.RetryInput{},
		"MoveInput":          app.MoveInput{},
		"DecomposeInput":     app.DecomposeInput{},
		"DecomposeResult":    app.DecomposeResult{},
		"ProjectStats":       app.ProjectStats{},
		"ConfigImportResult": app.ConfigImportResult{},
		"ProjectBundle":      app.ProjectBundle{},
	}
}

var timeType = reflect.TypeOf(time.Time{})

// schemaOf converts a Go type into a JSON Schema, driven by the json struct
// tags that the API already uses.
func schemaOf(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return schemaOf(t.Elem())
	case reflect.Interface:
		return map[string]any{}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaOf(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": schemaOf(t.Elem())}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Struct:
		if t == timeType {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		properties := map[string]any{}
		var required []string
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			name, opts := jsonField(field)
			if name == "-" || name == "" {
				continue
			}
			property := schemaOf(field.Type)
			if field.Type.Kind() == reflect.Pointer {
				// Optional values are expressed by absence rather than null.
				delete(property, "type")
			}
			properties[name] = property
			if !strings.Contains(opts, "omitempty") && field.Type.Kind() != reflect.Pointer {
				required = append(required, name)
			}
		}
		out := map[string]any{
			"type":                 "object",
			"properties":           properties,
			"additionalProperties": false,
		}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	default:
		return map[string]any{}
	}
}

func jsonField(field reflect.StructField) (string, string) {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name, ""
	}
	parts := strings.SplitN(tag, ",", 2)
	name := parts[0]
	opts := ""
	if len(parts) > 1 {
		opts = parts[1]
	}
	if name == "" {
		name = field.Name
	}
	return name, opts
}

type specParam struct {
	Name        string
	In          string
	Description string
	Required    bool
	Schema      string
}

type specRoute struct {
	Method   string
	Path     string
	Summary  string
	Tag      string
	Body     string
	Response string
	Params   []specParam
}

func pathParam(name, description string) specParam {
	return specParam{Name: name, In: "path", Description: description, Required: true, Schema: "string"}
}

func queryParam(name, description string) specParam {
	return specParam{Name: name, In: "query", Description: description, Schema: "string"}
}

func buildPaths() map[string]any {
	routes := []specRoute{
		{Method: "get", Path: "/api/health", Summary: "Service health and version", Tag: "system", Response: "object"},
		{Method: "get", Path: "/api/meta", Summary: "Deployment metadata: harnesses, roles, templates, limits", Tag: "system", Response: "object"},
		{Method: "get", Path: "/api/openapi.json", Summary: "This document", Tag: "system", Response: "object"},
		{Method: "get", Path: "/api/scheduler", Summary: "Scheduler status and concurrency accounting", Tag: "system", Response: "object"},
		{Method: "post", Path: "/api/scheduler/tick", Summary: "Run one scheduling pass immediately", Tag: "system", Response: "object"},

		{Method: "get", Path: "/api/projects", Summary: "List projects", Tag: "projects", Response: "List<Project>"},
		{Method: "post", Path: "/api/projects", Summary: "Create a project with workflow, agents and repository", Tag: "projects", Body: "CreateProjectInput", Response: "ProjectBundle"},
		{Method: "post", Path: "/api/projects/import", Summary: "Create a project from a YAML configuration file", Tag: "projects", Body: "string", Response: "object"},
		{Method: "post", Path: "/api/projects/validate", Summary: "Validate a YAML configuration file without applying it", Tag: "projects", Body: "string", Response: "object"},
		{Method: "get", Path: "/api/projects/{id}", Summary: "Get one project", Tag: "projects", Response: "Project", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "patch", Path: "/api/projects/{id}", Summary: "Update a project", Tag: "projects", Body: "UpdateProjectInput", Response: "Project", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "delete", Path: "/api/projects/{id}", Summary: "Delete a project", Tag: "projects", Response: "object", Params: []specParam{pathParam("id", "Project id"), queryParam("remove_worktrees", "Also remove task worktrees")}},
		{Method: "get", Path: "/api/projects/{id}/stats", Summary: "Dashboard counters", Tag: "projects", Response: "ProjectStats", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "get", Path: "/api/projects/{id}/board", Summary: "Kanban board, issues, statistics and dependency graph", Tag: "projects", Response: "object", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "get", Path: "/api/projects/{id}/repository", Summary: "Repository status", Tag: "projects", Response: "object", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "post", Path: "/api/projects/{id}/repository/ensure", Summary: "Prepare or re-check the repository", Tag: "projects", Response: "object", Params: []specParam{pathParam("id", "Project id"), queryParam("init", "Initialise a new repository when missing")}},
		{Method: "post", Path: "/api/projects/{id}/config", Summary: "Apply a YAML configuration file to a project", Tag: "projects", Body: "string", Response: "ConfigImportResult", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "post", Path: "/api/projects/{id}/decompose", Summary: "Architect-driven decomposition into issues and tasks", Tag: "projects", Body: "DecomposeInput", Response: "DecomposeResult", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "get", Path: "/api/projects/{id}/dependencies", Summary: "Task dependency edges", Tag: "projects", Response: "object", Params: []specParam{pathParam("id", "Project id")}},

		{Method: "get", Path: "/api/projects/{id}/issues", Summary: "List issues", Tag: "issues", Response: "List<Issue>", Params: []specParam{pathParam("id", "Project id"), queryParam("status", "Filter by status")}},
		{Method: "post", Path: "/api/projects/{id}/issues", Summary: "Create an issue", Tag: "issues", Body: "CreateIssueInput", Response: "Issue", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "get", Path: "/api/issues/{id}", Summary: "Get an issue", Tag: "issues", Response: "Issue", Params: []specParam{pathParam("id", "Issue id")}},
		{Method: "patch", Path: "/api/issues/{id}", Summary: "Update an issue", Tag: "issues", Body: "UpdateIssueInput", Response: "Issue", Params: []specParam{pathParam("id", "Issue id")}},
		{Method: "delete", Path: "/api/issues/{id}", Summary: "Delete an issue", Tag: "issues", Response: "object", Params: []specParam{pathParam("id", "Issue id")}},
		{Method: "get", Path: "/api/issues/{id}/tasks", Summary: "List the tasks of an issue", Tag: "issues", Response: "List<Task>", Params: []specParam{pathParam("id", "Issue id")}},
		{Method: "post", Path: "/api/issues/{id}/tasks", Summary: "Create a task in an issue", Tag: "issues", Body: "CreateTaskInput", Response: "Task", Params: []specParam{pathParam("id", "Issue id")}},
		{Method: "get", Path: "/api/projects/{id}/tasks", Summary: "List the tasks of a project", Tag: "tasks", Response: "List<Task>", Params: []specParam{pathParam("id", "Project id"), queryParam("status", "Comma separated status filter")}},

		{Method: "get", Path: "/api/tasks/{id}", Summary: "Get a task", Tag: "tasks", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "patch", Path: "/api/tasks/{id}", Summary: "Update a task", Tag: "tasks", Body: "UpdateTaskInput", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "delete", Path: "/api/tasks/{id}", Summary: "Delete a task", Tag: "tasks", Response: "object", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "get", Path: "/api/tasks/{id}/detail", Summary: "Full task view: issue, workflow, agent, executions and events", Tag: "tasks", Response: "object", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "get", Path: "/api/tasks/{id}/executions", Summary: "Execution history of a task", Tag: "tasks", Response: "List<Execution>", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "get", Path: "/api/tasks/{id}/events", Summary: "Orchestration events of a task", Tag: "tasks", Response: "List<Event>", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/start", Summary: "Make a task eligible for execution", Tag: "tasks", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/retry", Summary: "Retry a failed or blocked task", Tag: "tasks", Body: "RetryInput", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/cancel", Summary: "Cancel a task and its running execution", Tag: "tasks", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/approve", Summary: "Approve the work of an approval gate step", Tag: "tasks", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/reject", Summary: "Reject the work of an approval gate step (rework)", Tag: "tasks", Body: "object", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/reassign", Summary: "Point the next execution at a specific agent", Tag: "tasks", Body: "object", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/move", Summary: "Explicit operator move (status and/or workflow step)", Tag: "tasks", Body: "MoveInput", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "post", Path: "/api/tasks/{id}/dependencies", Summary: "Add a dependency edge", Tag: "tasks", Body: "object", Response: "Task", Params: []specParam{pathParam("id", "Task id")}},
		{Method: "delete", Path: "/api/tasks/{id}/dependencies/{dependsOnId}", Summary: "Remove a dependency edge", Tag: "tasks", Response: "Task", Params: []specParam{pathParam("id", "Task id"), pathParam("dependsOnId", "Dependency task id")}},

		{Method: "get", Path: "/api/executions", Summary: "List executions", Tag: "executions", Response: "List<Execution>", Params: []specParam{queryParam("project_id", "Filter by project"), queryParam("task_id", "Filter by task"), queryParam("agent_id", "Filter by agent"), queryParam("status", "Comma separated status filter")}},
		{Method: "get", Path: "/api/executions/{id}", Summary: "Get an execution", Tag: "executions", Response: "Execution", Params: []specParam{pathParam("id", "Execution id")}},
		{Method: "post", Path: "/api/executions/{id}/cancel", Summary: "Cancel a running execution", Tag: "executions", Response: "object", Params: []specParam{pathParam("id", "Execution id")}},
		{Method: "get", Path: "/api/executions/{id}/events", Summary: "Persisted output of an execution", Tag: "executions", Response: "List<ExecutionEvent>", Params: []specParam{pathParam("id", "Execution id"), queryParam("after_seq", "Return events after this sequence number")}},
		{Method: "get", Path: "/api/executions/{id}/log", Summary: "Raw harness log output", Tag: "executions", Response: "object", Params: []specParam{pathParam("id", "Execution id")}},
		{Method: "get", Path: "/api/executions/{id}/stream", Summary: "Live Server-Sent Events stream of an execution", Tag: "executions", Response: "string", Params: []specParam{pathParam("id", "Execution id")}},

		{Method: "get", Path: "/api/agents", Summary: "List agent configurations", Tag: "agents", Response: "List<Agent>"},
		{Method: "post", Path: "/api/agents", Summary: "Create an agent configuration", Tag: "agents", Body: "AgentInput", Response: "Agent"},
		{Method: "get", Path: "/api/agents/{id}", Summary: "Get an agent configuration", Tag: "agents", Response: "Agent", Params: []specParam{pathParam("id", "Agent id")}},
		{Method: "patch", Path: "/api/agents/{id}", Summary: "Update an agent configuration", Tag: "agents", Body: "AgentInput", Response: "Agent", Params: []specParam{pathParam("id", "Agent id")}},
		{Method: "delete", Path: "/api/agents/{id}", Summary: "Delete an agent configuration", Tag: "agents", Response: "object", Params: []specParam{pathParam("id", "Agent id")}},
		{Method: "get", Path: "/api/roles", Summary: "List roles", Tag: "agents", Response: "List<Role>"},
		{Method: "post", Path: "/api/roles", Summary: "Create a role", Tag: "agents", Body: "RoleInput", Response: "Role"},
		{Method: "get", Path: "/api/projects/{id}/agents", Summary: "List the agents assigned in a project", Tag: "agents", Response: "List<ProjectAgentView>", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "post", Path: "/api/projects/{id}/agents", Summary: "Assign an agent to a role in a project", Tag: "agents", Body: "AssignmentInput", Response: "ProjectAgent", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "patch", Path: "/api/projects/{id}/agents/{assignmentId}", Summary: "Update a project agent assignment", Tag: "agents", Body: "AssignmentInput", Response: "ProjectAgent", Params: []specParam{pathParam("id", "Project id"), pathParam("assignmentId", "Assignment id")}},
		{Method: "delete", Path: "/api/projects/{id}/agents/{assignmentId}", Summary: "Remove a project agent assignment", Tag: "agents", Response: "object", Params: []specParam{pathParam("id", "Project id"), pathParam("assignmentId", "Assignment id")}},

		{Method: "get", Path: "/api/projects/{id}/workflows", Summary: "List the workflows of a project", Tag: "workflows", Response: "List<Workflow>", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "put", Path: "/api/projects/{id}/workflow", Summary: "Replace the default workflow definition", Tag: "workflows", Body: "Workflow", Response: "Workflow", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "get", Path: "/api/workflow-templates", Summary: "Built-in workflow templates", Tag: "workflows", Response: "List<WorkflowTemplate>"},

		{Method: "get", Path: "/api/events", Summary: "Recent orchestration activity", Tag: "events", Response: "List<Event>"},
		{Method: "get", Path: "/api/projects/{id}/events", Summary: "Persisted activity of a project", Tag: "events", Response: "List<Event>", Params: []specParam{pathParam("id", "Project id")}},
		{Method: "get", Path: "/api/projects/{id}/stream", Summary: "Live Server-Sent Events stream of a project", Tag: "events", Response: "string", Params: []specParam{pathParam("id", "Project id")}},
	}

	paths := map[string]any{}
	for _, route := range routes {
		item, ok := paths[route.Path].(map[string]any)
		if !ok {
			item = map[string]any{}
			paths[route.Path] = item
		}
		operation := map[string]any{
			"summary":   route.Summary,
			"tags":      []string{route.Tag},
			"responses": responsesFor(route),
		}
		if len(route.Params) > 0 {
			params := make([]map[string]any, 0, len(route.Params))
			for _, p := range route.Params {
				schema := map[string]any{"type": p.Schema}
				if p.Schema == "" {
					schema = map[string]any{"type": "string"}
				}
				params = append(params, map[string]any{
					"name":        p.Name,
					"in":          p.In,
					"description": p.Description,
					"required":    p.Required || p.In == "path",
					"schema":      schema,
				})
			}
			operation["parameters"] = params
		}
		if route.Body != "" {
			operation["requestBody"] = map[string]any{
				"required": true,
				"content": map[string]any{
					"application/json": map[string]any{"schema": bodySchema(route.Body)},
				},
			}
		}
		item[route.Method] = operation
	}
	return paths
}

func bodySchema(name string) map[string]any {
	switch name {
	case "string":
		return map[string]any{"type": "string", "description": "Raw YAML configuration file"}
	case "object":
		return map[string]any{"type": "object"}
	case "":
		return map[string]any{"type": "object"}
	}
	if strings.HasPrefix(name, "List<") {
		inner := strings.TrimSuffix(strings.TrimPrefix(name, "List<"), ">")
		return map[string]any{"type": "array", "items": ref(inner)}
	}
	return ref(name)
}

func responsesFor(route specRoute) map[string]any {
	responses := map[string]any{
		"400": errorResponse("Invalid request"),
		"404": errorResponse("Not found"),
		"409": errorResponse("Conflicting state"),
	}
	if route.Response == "string" {
		responses["200"] = map[string]any{
			"description": "Server-Sent Events stream",
			"content": map[string]any{
				"text/event-stream": map[string]any{"schema": map[string]any{"type": "string"}},
			},
		}
		return responses
	}
	schema := bodySchema(route.Response)
	if route.Response == "" {
		schema = map[string]any{"type": "object"}
	}
	responses["200"] = map[string]any{
		"description": "Success",
		"content":     map[string]any{"application/json": map[string]any{"schema": schema}},
	}
	if route.Method == "post" {
		responses["201"] = map[string]any{
			"description": "Created",
			"content":     map[string]any{"application/json": map[string]any{"schema": schema}},
		}
	}
	responses["500"] = errorResponse("Internal error")
	return responses
}

func errorResponse(description string) map[string]any {
	return map[string]any{
		"description": description,
		"content":     map[string]any{"application/json": map[string]any{"schema": ref("Error")}},
	}
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

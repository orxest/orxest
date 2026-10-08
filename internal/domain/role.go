package domain

import (
	"strings"
	"time"
)

// Built-in role identifiers. Roles are reusable definitions, independent from
// models and from any particular agent (spec §9).
const (
	RoleArchitect       = "architect"
	RoleSeniorDeveloper = "senior-developer"
	RoleJuniorDeveloper = "junior-developer"
	RoleDeveloper       = "developer"
	RoleTester          = "tester"
	RoleReviewer        = "reviewer"
	RoleSecurity        = "security"
)

// Role is a reusable responsibility that a workflow step can require.
type Role struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	BuiltIn     bool      `json:"built_in"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Validate checks structural invariants of a role.
func (r *Role) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return Invalidf("id", "must not be empty")
	}
	if !isSlug(r.ID) {
		return Invalidf("id", "must be a lowercase slug such as \"senior-developer\"")
	}
	if strings.TrimSpace(r.Name) == "" {
		r.Name = r.ID
	}
	return nil
}

func isSlug(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.':
		default:
			return false
		}
	}
	return true
}

// BuiltInRoles returns the roles seeded into every Orxest installation.
func BuiltInRoles() []Role {
	return []Role{
		{ID: RoleArchitect, Name: "Architect", Description: "Decomposes product intent into issues, tasks, dependencies and acceptance criteria.", BuiltIn: true},
		{ID: RoleSeniorDeveloper, Name: "Senior Developer", Description: "Implements complex changes and reworks rejected work.", BuiltIn: true},
		{ID: RoleJuniorDeveloper, Name: "Junior Developer", Description: "Implements well specified, low risk changes.", BuiltIn: true},
		{ID: RoleDeveloper, Name: "Developer", Description: "Generic implementation role for lightweight workflows.", BuiltIn: true},
		{ID: RoleTester, Name: "Tester", Description: "Verifies behaviour against acceptance criteria and reports failures.", BuiltIn: true},
		{ID: RoleReviewer, Name: "Reviewer", Description: "Reviews the implementation and approves or requests changes.", BuiltIn: true},
		{ID: RoleSecurity, Name: "Security", Description: "Reviews changes for security impact.", BuiltIn: true},
	}
}

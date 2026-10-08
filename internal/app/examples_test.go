package app_test

import (
	"os"
	"testing"

	"github.com/orxest/orxest/internal/app"
)

func TestExampleProjectPiFileParses(t *testing.T) {
	for _, path := range []string{"../../examples/project-pi.yaml", "../../examples/project.yaml"} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Skipf("example missing: %v", err)
		}
		file, err := app.ParseProjectFile(data)
		if err != nil {
			t.Fatalf("%s must parse: %v", path, err)
		}
		if file.Project.Name == "" || len(file.Agents) == 0 || file.Workflow == nil {
			t.Fatalf("%s: incomplete example", path)
		}
	}
}

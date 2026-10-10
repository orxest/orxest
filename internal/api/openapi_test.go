package api_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/orxest/orxest/internal/api"
)

// TestCommittedOpenAPIDocumentIsCurrent keeps docs/openapi.json honest. Without
// it the checked-in document silently drifts from the routes and schemas the
// server actually serves (fix-plan §12).
func TestCommittedOpenAPIDocumentIsCurrent(t *testing.T) {
	const path = "../../docs/openapi.json"
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("OpenAPI document missing: %v", err)
	}
	generated, err := json.MarshalIndent(api.OpenAPIDocument(""), "", "  ")
	if err != nil {
		t.Fatalf("encoding the generated document: %v", err)
	}
	generated = append(generated, '\n')
	if !bytes.Equal(generated, committed) {
		t.Errorf("%s is stale: regenerate it with `make openapi`", path)
	}
}

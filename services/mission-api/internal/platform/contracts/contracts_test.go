package contracts_test

import (
	"testing"

	"github.com/croppedeyebrow/sentinel-grid/services/mission-api/internal/platform/contracts"
)

// Every published example is validated against its schema from the Go side as
// well, so a contract change cannot land with only the console updated.
func TestPublishedExamplesMatchTheirSchemas(t *testing.T) {
	cases := []struct {
		example string
		schema  string
		refs    []string
	}{
		{example: "operational-picture", schema: "operational-picture"},
		{
			example: "patrol-mission-list",
			schema:  "patrol-mission-list",
			refs:    []string{"patrol-mission"},
		},
		{example: "patrol-mission-create-request", schema: "patrol-mission-create-request"},
		{example: "patrol-mission-transition-request", schema: "patrol-mission-transition-request"},
		{example: "api-error", schema: "api-error"},
	}

	for _, testCase := range cases {
		t.Run(testCase.example, func(t *testing.T) {
			schema, err := contracts.Compile(testCase.schema, testCase.refs...)
			if err != nil {
				t.Fatalf("compile schema: %v", err)
			}
			example, err := contracts.Example(testCase.example)
			if err != nil {
				t.Fatalf("read example: %v", err)
			}

			if err := schema.Validate(example); err != nil {
				t.Fatalf("example violates its schema: %v", err)
			}
		})
	}
}

func TestSchemasRejectAnUndeclaredField(t *testing.T) {
	schema, err := contracts.Compile("patrol-mission-transition-request")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	if err := schema.Validate(map[string]any{"version": 3.0, "note": "why not"}); err == nil {
		t.Fatal("a request with an undeclared field must be refused")
	}
}

func TestTransitionRequestRequiresTheVersion(t *testing.T) {
	schema, err := contracts.Compile("patrol-mission-transition-request")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	if err := schema.Validate(map[string]any{"reason": "BATTERY"}); err == nil {
		t.Fatal("a transition without the caller's version must be refused")
	}
}

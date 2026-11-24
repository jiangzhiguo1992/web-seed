package check

import (
	"strings"
	"testing"
)

func TestHTTPSharedComponentSections(t *testing.T) {
	c := &Checker{}
	c.checkHTTPComponents(map[string]any{
		"schemas": map[string]any{
			"Good": map[string]any{"type": "object", "additionalProperties": false},
		},
		"headers": map[string]any{
			"Good": map[string]any{"schema": map[string]any{"type": "string"}},
		},
		"responses": map[string]any{
			"MissingRef": map[string]any{
				"description": "x",
				"headers":     map[string]any{"X": map[string]any{"$ref": "#/components/headers/Missing"}},
			},
		},
		"examples": map[string]any{
			"Bad":  map[string]any{"value": 1, "externalValue": "x"},
			"None": map[string]any{},
			"Ref":  map[string]any{"$ref": "#/components/examples/Missing"},
			"Data": map[string]any{"value": map[string]any{"$ref": "#/components/schemas/Missing"}},
		},
		"requestBodies": map[string]any{
			"Empty":    map[string]any{},
			"BadMedia": map[string]any{"content": map[string]any{"application/json": map[string]any{}}},
			"Good": map[string]any{"content": map[string]any{
				"application/json": map[string]any{
					"schema":  map[string]any{"type": "string"},
					"example": map[string]any{"$ref": "#/components/schemas/Missing"},
				},
			}},
		},
		"links": map[string]any{
			"None": map[string]any{},
			"Both": map[string]any{"operationRef": "#/x", "operationId": "y"},
			"Good": map[string]any{"operationId": "y"},
		},
		"callbacks": map[string]any{
			"Empty": map[string]any{},
			"Bad":   map[string]any{"expr": map[string]any{}},
			"Good":  map[string]any{"expr": map[string]any{"get": map[string]any{"responses": map[string]any{}}}},
		},
		"pathItems": map[string]any{
			"Empty": map[string]any{},
			"Good":  map[string]any{"get": map[string]any{"responses": map[string]any{}}},
		},
	}, "contracts/platform/http_conventions.yaml", true)
	got := strings.Join(c.errs, "\n")
	for _, want := range []string{
		"examples.Bad must declare exactly one of value/externalValue",
		"examples.None must declare exactly one of value/externalValue",
		"requestBodies.Empty.content required",
		"requestBodies.BadMedia.content.application/json.schema required",
		"links.None must declare exactly one of operationRef/operationId",
		"links.Both must declare exactly one of operationRef/operationId",
		"callbacks.Empty must not be empty",
		"callbacks.Bad.expr must contain an operation or $ref",
		"pathItems.Empty must contain an operation or $ref",
		"$ref #/components/headers/Missing does not resolve",
		"$ref #/components/examples/Missing does not resolve",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

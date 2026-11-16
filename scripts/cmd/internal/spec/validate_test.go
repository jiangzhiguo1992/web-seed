package spec

import "testing"

func TestValidateRejectsDanglingRef(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/x": map[string]any{
				"get": map[string]any{
					"operationId": "getX",
					"x-service":   "cap.services.x.get",
					"responses": map[string]any{
						"200": map[string]any{
							"description": "OK",
							"content": map[string]any{
								"application/json": map[string]any{
									"schema": map[string]any{"$ref": "#/components/schemas/Missing"},
								},
							},
						},
					},
				},
			},
		},
		"components": map[string]any{"schemas": map[string]any{}},
	}
	if err := Validate(doc); err == nil {
		t.Fatal("dangling ref not reported")
	}
}

func TestValidateRejectsMissingRequired(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"paths":   map[string]any{},
		"components": map[string]any{
			"schemas": map[string]any{
				"X": map[string]any{
					"type":       "object",
					"required":   []any{"id"},
					"properties": map[string]any{},
				},
			},
		},
	}
	if err := Validate(doc); err == nil {
		t.Fatal("missing required field not reported")
	}
}

func TestValidateAcceptsMinimalSpec(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/x": map[string]any{
				"get": map[string]any{
					"operationId": "getX",
					"x-service":   "cap.services.x.get",
					"responses":   map[string]any{"200": map[string]any{"description": "OK"}},
				},
			},
		},
		"components": map[string]any{"schemas": map[string]any{}},
	}
	if err := Validate(doc); err != nil {
		t.Fatal(err)
	}
}

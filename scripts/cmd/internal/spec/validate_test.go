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

func TestValidateRejectsMissingPathParameter(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/x/{id}": map[string]any{
				"get": map[string]any{
					"operationId": "getX",
					"x-service":   "cap.services.x.get",
					"responses":   map[string]any{"200": map[string]any{"description": "OK"}},
				},
			},
		},
	}
	if err := Validate(doc); err == nil {
		t.Fatal("missing path parameter not reported")
	}
}

func TestValidateRejectsUnknownOperationKey(t *testing.T) {
	doc := map[string]any{
		"openapi": "3.1.0",
		"paths": map[string]any{
			"/x": map[string]any{
				"get": map[string]any{
					"operationId": "getX",
					"x-service":   "cap.services.x.get",
					"respones":    map[string]any{"200": map[string]any{"description": "OK"}},
				},
			},
		},
	}
	if err := Validate(doc); err == nil {
		t.Fatal("unknown operation key not reported")
	}
}

func TestValidateRejectsBadResponse(t *testing.T) {
	base := func(responses map[string]any) map[string]any {
		return map[string]any{
			"openapi": "3.1.0",
			"paths": map[string]any{
				"/x": map[string]any{
					"get": map[string]any{
						"operationId": "getX",
						"x-service":   "cap.services.x.get",
						"responses":   responses,
					},
				},
			},
		}
	}
	if err := Validate(base(map[string]any{"600": map[string]any{"description": "bad"}})); err == nil {
		t.Fatal("invalid status not reported")
	}
	if err := Validate(base(map[string]any{"200": map[string]any{}})); err == nil {
		t.Fatal("missing response description not reported")
	}
	if err := Validate(base(map[string]any{"200": map[string]any{
		"description": "OK",
		"content":     map[string]any{"application/json": map[string]any{}},
	}})); err == nil {
		t.Fatal("content without schema not reported")
	}
}

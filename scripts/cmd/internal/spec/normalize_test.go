package spec

import (
	"fmt"
	"strings"
	"testing"
)

func httpDoc(operation map[string]any) map[string]any {
	if operation["name"] == nil {
		operation["name"] = "postX"
	}
	if operation["contract"] == nil {
		operation["contract"] = map[string]any{
			"id":      "cap.protocols.http.x.post",
			"service": "cap.services.x.post",
		}
	}
	if operation["security"] == nil {
		operation["security"] = map[string]any{"BearerAuth": false}
	}
	return map[string]any{
		"mount": map[string]any{"api_version": "v1", "path": "/", "audience": []any{"admin"}},
		"paths": map[string]any{
			"/x": map[string]any{"post": operation},
		},
	}
}

func testResolve(name string) (map[string]any, error) {
	switch name {
	case "_types.integer":
		return map[string]any{"type": "integer"}, nil
	case "_types.string":
		return map[string]any{"type": "string"}, nil
	}
	return nil, fmt.Errorf("unknown type %s", name)
}

func TestNormalizeHTTPDocumentGrouped(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"query": []any{
				map[string]any{"name": "limit", "schema": "_types.integer", "required": false, "description": "size", "to": "params.limit"},
			},
			"body": map[string]any{
				"content_type": "application/json",
				"schema":       map[string]any{"$ref": "#/components/schemas/Input"},
				"to":           "params.input",
			},
		},
		"response": map[string]any{
			"200": map[string]any{
				"description": "ok",
				"body": map[string]any{
					"content_type": "application/json",
					"schema":       map[string]any{"$ref": "#/components/schemas/Output"},
				},
			},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	if op["request"] != nil || op["response"] != nil {
		t.Fatalf("request/response must be removed: %v", op)
	}
	params := asList(op["parameters"])
	if len(params) != 1 {
		t.Fatalf("want 1 parameter, got %d: %v", len(params), params)
	}
	kinds := map[string]bool{}
	for _, raw := range params {
		param := asMap(raw)
		in, _ := param["in"].(string)
		name, _ := param["name"].(string)
		kinds[in+" "+name] = true
	}
	for _, want := range []string{"query limit"} {
		if !kinds[want] {
			t.Fatalf("missing parameter %s: %v", want, params)
		}
	}
	body := asMap(op["requestBody"])
	if body == nil || asMap(asMap(body["content"])["application/json"]) == nil {
		t.Fatalf("requestBody not normalized: %v", op["requestBody"])
	}
	serviceParams := asMap(op["x-service-params"])
	if serviceParams["body"] != "params.input" {
		t.Fatalf("body target not preserved: %v", serviceParams)
	}
	if got := asList(serviceParams["query"]); len(got) != 1 || asMap(got[0])["to"] != "params.limit" {
		t.Fatalf("query target not preserved: %v", serviceParams)
	}
	resp := asMap(asMap(op["responses"])["200"])
	if resp == nil || asMap(asMap(resp["content"])["application/json"]) == nil {
		t.Fatalf("response body not normalized: %v", op["responses"])
	}
}

func TestNormalizeHTTPDocumentHeaders(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "Authorization", "auth": "BearerAuth", "required": true, "description": "token"},
				map[string]any{"name": "Content-Type", "value": "application/json", "required": true, "description": "body"},
				map[string]any{"name": "Accept", "value": "application/json", "required": false, "description": "accept"},
			},
			"body": map[string]any{"kind": "json", "schema": "_types.string", "to": "params.input"},
		},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	if got := asList(op["security"]); len(got) != 1 || asMap(got[0])["BearerAuth"] == nil {
		t.Fatalf("security not derived from Authorization: %v", op["security"])
	}
	params := asList(op["parameters"])
	if len(params) != 1 || asMap(params[0])["name"] != "Accept" {
		t.Fatalf("non-special headers not normalized: %v", params)
	}
}

func TestNormalizeHTTPDocumentFormBody(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "Content-Type", "value": "application/x-www-form-urlencoded", "required": true, "description": "body"},
			},
			"body": map[string]any{
				"kind": "form",
				"fields": []any{
					map[string]any{"name": "username", "schema": "_types.string", "required": true, "description": "name", "to": "params.input.username"},
				},
			},
		},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	body := asMap(op["requestBody"])
	if body == nil || asMap(asMap(body["content"])["application/x-www-form-urlencoded"]) == nil {
		t.Fatalf("form body not normalized: %v", op["requestBody"])
	}
	params := asMap(op["x-service-params"])
	if got := asList(params["form"]); len(got) != 1 || asMap(got[0])["to"] != "params.input.username" {
		t.Fatalf("form target not preserved: %v", params)
	}
}

func TestNormalizeHTTPDocumentSecurity(t *testing.T) {
	doc := httpDoc(map[string]any{
		"security": map[string]any{"BearerAuth": true},
		"request":  map[string]any{},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	security := asList(op["security"])
	if len(security) != 1 || asMap(security[0])["BearerAuth"] == nil {
		t.Fatalf("security not normalized: %v", security)
	}
	params := asMap(op["x-service-params"])
	if params == nil || len(params) != 0 {
		t.Fatalf("auth-injected params must not be stored in x-service-params: %v", params)
	}
}

func TestNormalizeHTTPDocumentErrorCodes(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{},
		"response": map[string]any{
			"204": map[string]any{"description": "ok"},
			"409": map[string]any{"codes": []any{"platform.errors.ACCOUNT_NUMBER_TAKEN"}},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	responses := asMap(asMap(asMap(doc["paths"])["/x"])["post"])["responses"]
	resp := asMap(asMap(responses)["409"])
	if resp["$ref"] != "#/components/responses/Conflict" {
		t.Fatalf("409 must map to Conflict: %v", resp)
	}
	codes := asList(resp["x-error-codes"])
	if len(codes) != 1 || codes[0] != "platform.errors.ACCOUNT_NUMBER_TAKEN" {
		t.Fatalf("x-error-codes not preserved: %v", resp)
	}
}

func TestNormalizeHTTPDocumentRejectsFlatErrors(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request":  map[string]any{},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
		"errors":   []any{"platform.errors.ACCOUNT_NOT_FOUND"},
	})
	err := NormalizeHTTPDocument(doc, testResolve, false)
	if err == nil || !strings.Contains(err.Error(), "response.<status>.codes") {
		t.Fatalf("flat errors not rejected: %v", err)
	}
}

func TestNormalizeHTTPDocumentRejectsEmptyCodes(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request":  map[string]any{},
		"response": map[string]any{"409": map[string]any{"codes": []any{}}},
	})
	err := NormalizeHTTPDocument(doc, testResolve, false)
	if err == nil || !strings.Contains(err.Error(), "requires non-empty codes") {
		t.Fatalf("empty codes not rejected: %v", err)
	}
}

func TestCollectHTTPOperationMeta(t *testing.T) {
	doc := map[string]any{
		"mount": map[string]any{"api_version": "v1", "path": "/", "audience": []any{"admin"}},
		"paths": map[string]any{
			"/x": map[string]any{
				"get": map[string]any{
					"id":      "cap.protocols.http.x.get",
					"used_by": []any{"feature.pages.x"},
				},
			},
		},
	}
	meta := CollectHTTPOperationMeta(doc)
	got := meta["cap.protocols.http.x.get"]
	if len(got.UsedBy) != 1 || got.UsedBy[0] != "feature.pages.x" {
		t.Fatalf("used_by not collected: %v", got)
	}
}

func TestNormalizeHTTPDocumentRejectsComponents(t *testing.T) {
	doc := map[string]any{"components": map[string]any{}}
	err := NormalizeHTTPDocument(doc, nil, true)
	if err == nil || !strings.Contains(err.Error(), "components") {
		t.Fatalf("components not rejected: %v", err)
	}
}

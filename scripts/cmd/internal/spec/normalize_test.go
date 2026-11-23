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
			"headers": map[string]any{
				"X-Request-Id": map[string]any{"$ref": "#/components/parameters/RequestId"},
				"X-CSRF-Token": map[string]any{"schema": map[string]any{"type": "string"}},
			},
			"query": map[string]any{
				"limit": map[string]any{"schema": map[string]any{"type": "integer"}, "required": false},
			},
			"body": map[string]any{
				"content_type": "application/json",
				"schema":       map[string]any{"$ref": "#/components/schemas/Input"},
			},
		},
		"response": map[string]any{
			"200": map[string]any{
				"description": "ok",
				"headers": map[string]any{
					"X-Request-Id": map[string]any{"$ref": "#/components/headers/XRequestId"},
				},
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
	if len(params) != 3 {
		t.Fatalf("want 3 parameters, got %d: %v", len(params), params)
	}
	kinds := map[string]bool{}
	refFound := false
	for _, raw := range params {
		param := asMap(raw)
		if ref, _ := param["$ref"].(string); ref == "#/components/parameters/RequestId" {
			refFound = true
			continue
		}
		in, _ := param["in"].(string)
		name, _ := param["name"].(string)
		kinds[in+" "+name] = true
	}
	if !refFound {
		t.Fatalf("missing RequestId parameter ref: %v", params)
	}
	for _, want := range []string{"header X-CSRF-Token", "query limit"} {
		if !kinds[want] {
			t.Fatalf("missing parameter %s: %v", want, params)
		}
	}
	body := asMap(op["requestBody"])
	if body == nil || asMap(asMap(body["content"])["application/json"]) == nil {
		t.Fatalf("requestBody not normalized: %v", op["requestBody"])
	}
	resp := asMap(asMap(op["responses"])["200"])
	if resp == nil || asMap(asMap(resp["content"])["application/json"]) == nil {
		t.Fatalf("response body not normalized: %v", op["responses"])
	}
}

func TestNormalizeHTTPDocumentCookies(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"cookies": map[string]any{
				"refresh_token": map[string]any{"$ref": "#/components/parameters/RefreshTokenCookie"},
			},
		},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	param := asMap(asList(asMap(asMap(asMap(doc["paths"])["/x"])["post"])["parameters"])[0])
	if param["$ref"] != "#/components/parameters/RefreshTokenCookie" {
		t.Fatalf("cookie parameter not normalized: %v", param)
	}
}

func TestNormalizeHTTPDocumentRejectsLegacy(t *testing.T) {
	doc := httpDoc(map[string]any{
		"parameters":  []any{},
		"responses":   map[string]any{"200": map[string]any{"description": "ok"}},
		"requestBody": map[string]any{},
	})
	err := NormalizeHTTPDocument(doc, testResolve, false)
	if err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("legacy shape not rejected: %v", err)
	}
}

func TestNormalizeHTTPDocumentRequiresBlocks(t *testing.T) {
	if err := NormalizeHTTPDocument(httpDoc(map[string]any{"response": map[string]any{}}), testResolve, false); err == nil {
		t.Fatal("missing request block not rejected")
	}
	if err := NormalizeHTTPDocument(httpDoc(map[string]any{"request": map[string]any{}}), testResolve, false); err == nil {
		t.Fatal("missing response block not rejected")
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
	if err == nil || !strings.Contains(err.Error(), "shared") {
		t.Fatalf("components not rejected: %v", err)
	}
}

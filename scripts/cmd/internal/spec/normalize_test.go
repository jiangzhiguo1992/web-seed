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
			"headers": []any{
				map[string]any{"name": "Content-Type", "value": "application/json", "required": true, "description": "body"},
			},
			"query": []any{
				map[string]any{"name": "limit", "type": "_types.integer", "required": false, "description": "size", "to": "params.limit"},
			},
			"body": map[string]any{
				"kind": "json",
				"type": map[string]any{"$ref": "#/components/schemas/Input"},
				"to":   "params.input",
			},
		},
		"response": map[string]any{
			"200": map[string]any{
				"description": "ok",
				"headers": []any{
					map[string]any{"name": "Content-Type", "value": "application/json", "required": true, "description": "body"},
				},
				"body": map[string]any{
					"kind": "json",
					"type": map[string]any{"$ref": "#/components/schemas/Output"},
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
			"body": map[string]any{"kind": "json", "type": "_types.string", "to": "params.input"},
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

func TestNormalizeHTTPDocumentResponseHeaders(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{},
		"response": map[string]any{
			"200": map[string]any{
				"description": "ok",
				"headers": []any{
					map[string]any{"name": "Content-Type", "value": "application/json", "required": true, "description": "body"},
					map[string]any{"name": "Link", "type": "_types.string", "multiple": true, "required": false, "description": "link"},
					map[string]any{"name": "ETag", "type": "_types.string", "required": false, "description": "etag"},
				},
				"body": map[string]any{"kind": "json", "type": "_types.string"},
			},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	resp := asMap(asMap(op["responses"])["200"])
	headers := asMap(resp["headers"])
	if asMap(headers["ETag"]) == nil {
		t.Fatalf("ETag header missing: %v", headers)
	}
	link := asMap(headers["Link"])
	if asMap(link["schema"])["type"] != "array" {
		t.Fatalf("Link multiple not array: %v", link)
	}
	if _, ok := headers["Content-Type"]; ok {
		t.Fatalf("Content-Type must become response content, not header: %v", headers)
	}
}

func TestNormalizeHTTPDocumentHeaderRequiredDefaultsFalse(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "X-Trace", "type": "_types.string", "description": "trace"},
			},
		},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	params := asList(op["parameters"])
	if len(params) != 1 || asMap(params[0])["required"] != false {
		t.Fatalf("header required must default to false: %v", params)
	}
}

func TestNormalizeHTTPDocumentAllowsOptionalContentType(t *testing.T) {
	doc := map[string]any{
		"mount": map[string]any{"api_version": "v1", "path": "/", "audience": []any{"admin"}},
		"paths": map[string]any{
			"/x": map[string]any{"post": map[string]any{
				"id":      "cap.protocols.http.x.post",
				"name":    "postX",
				"service": "cap.services.x.post",
				"request": map[string]any{
					"headers": []any{
						map[string]any{"name": "Content-Type", "value": "application/json", "required": false, "description": "body"},
					},
					"body": map[string]any{"kind": "json", "type": "_types.string", "to": "params.input"},
				},
				"response": map[string]any{"204": map[string]any{"description": "ok"}},
			}},
		},
	}
	if err := NormalizeHTTPDocument(doc, testResolve, true); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeHTTPDocumentRejectsLegacySchemaField(t *testing.T) {
	doc := map[string]any{
		"mount": map[string]any{"api_version": "v1", "path": "/", "audience": []any{"admin"}},
		"paths": map[string]any{
			"/x": map[string]any{"post": map[string]any{
				"id":      "cap.protocols.http.x.post",
				"name":    "postX",
				"service": "cap.services.x.post",
				"request": map[string]any{
					"headers": []any{
						map[string]any{"name": "Content-Type", "value": "application/json", "description": "body"},
					},
					"body": map[string]any{"kind": "json", "schema": "_types.string", "to": "params.input"},
				},
				"response": map[string]any{"204": map[string]any{"description": "ok"}},
			}},
		},
	}
	err := NormalizeHTTPDocument(doc, testResolve, true)
	if err == nil || !strings.Contains(err.Error(), "request.body.type") {
		t.Fatalf("legacy request.body.schema must be rejected: %v", err)
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
					map[string]any{"name": "username", "type": "_types.string", "required": true, "description": "name", "to": "params.input.username"},
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

func TestNormalizeHTTPDocumentRejectsTransportHeaders(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   map[string]any
	}{
		{"request connection", map[string]any{
			"request": map[string]any{
				"headers": []any{map[string]any{"name": "Connection", "value": "keep-alive", "required": false, "description": "transport"}},
			},
			"response": map[string]any{"204": map[string]any{"description": "ok"}},
		}},
		{"request expect", map[string]any{
			"request": map[string]any{
				"headers": []any{map[string]any{"name": "Expect", "value": "100-continue", "required": false, "description": "transport"}},
			},
			"response": map[string]any{"204": map[string]any{"description": "ok"}},
		}},
		{"response content-length", map[string]any{
			"request": map[string]any{},
			"response": map[string]any{
				"200": map[string]any{
					"description": "ok",
					"headers":     []any{map[string]any{"name": "Content-Length", "type": "_types.integer", "required": false, "description": "length"}},
					"body":        map[string]any{"kind": "json", "type": "_types.string"},
				},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := httpDoc(tc.op)
			err := NormalizeHTTPDocument(doc, testResolve, false, []string{"Connection", "Content-Length", "Expect"})
			if err == nil || !strings.Contains(err.Error(), "transport-managed") {
				t.Fatalf("transport header not rejected: %v", err)
			}
		})
	}
}

func TestNormalizeHTTPDocumentAllowsUpgradeInsecureRequests(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "Upgrade-Insecure-Requests", "type": "_types.string", "required": false, "description": "browser hint"},
			},
		},
		"response": map[string]any{"204": map[string]any{"description": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, []string{"Connection", "Content-Length", "Expect"}); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	params := asList(op["parameters"])
	if len(params) != 1 || asMap(params[0])["name"] != "Upgrade-Insecure-Requests" {
		t.Fatalf("Upgrade-Insecure-Requests must remain an ordinary header: %v", params)
	}
}

func TestNormalizeHTTPDocumentMultipartBody(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "Content-Type", "value": "multipart/form-data", "required": true, "description": "body"},
			},
			"body": map[string]any{
				"kind": "multipart",
				"fields": []any{
					map[string]any{"name": "nickname", "type": "_types.string", "required": true, "description": "name", "to": "params.input.nickname"},
					map[string]any{"name": "avatar", "kind": "file", "required": false, "description": "file", "to": "params.input.avatar"},
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
	media := asMap(asMap(body["content"])["multipart/form-data"])
	schema := asMap(media["schema"])
	props := asMap(schema["properties"])
	if asMap(props["avatar"])["format"] != "binary" {
		t.Fatalf("multipart file field not normalized: %v", props)
	}
}

func TestNormalizeHTTPDocumentSecurity(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "Authorization", "auth": "BearerAuth", "required": true, "description": "token"},
			},
		},
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

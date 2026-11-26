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

func testNormalizeOptions(transport ...string) HTTPNormalizeOptions {
	return HTTPNormalizeOptions{
		TransportHeaders: transport,
		BodyTypes: map[string]string{
			"json":      "application/json",
			"form":      "application/x-www-form-urlencoded",
			"multipart": "multipart/form-data",
			"text":      "text/plain",
			"binary":    "application/octet-stream",
		},
	}
}

func testResolve(name string) (map[string]any, error) {
	switch name {
	case "_types.basics.integer":
		return map[string]any{"type": "integer"}, nil
	case "_types.basics.string":
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
				map[string]any{"name": "limit", "type": "_types.basics.integer", "required": false, "description": "size", "to": "params.limit"},
			},
			"body": map[string]any{
				"kind": "json",
				"type": map[string]any{"$ref": "#/components/schemas/Input"},
				"to":   "params.input",
			},
		},
		"response": map[string]any{
			"200": map[string]any{
				"msg": "ok",
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
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
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
				map[string]any{"name": "Authorization", "auth": "Bearer", "required": true, "description": "token"},
				map[string]any{"name": "Content-Type", "value": "application/json", "required": true, "description": "body"},
				map[string]any{"name": "Accept", "value": "application/json", "required": false, "description": "accept"},
			},
			"body": map[string]any{"kind": "json", "type": "_types.basics.string", "to": "params.input"},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	if got := asList(op["security"]); len(got) != 1 || asMap(got[0])["Bearer"] == nil {
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
				"msg": "ok",
				"headers": []any{
					map[string]any{"name": "Content-Type", "value": "application/json", "required": true, "description": "body"},
					map[string]any{"name": "Link", "type": "_types.basics.string", "multiple": true, "required": false, "description": "link"},
					map[string]any{"name": "ETag", "type": "_types.basics.string", "required": false, "description": "etag"},
					map[string]any{"name": "X-Required", "value": "yes", "required": true, "description": "required"},
				},
				"body": map[string]any{"kind": "json", "type": "_types.basics.string"},
			},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
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
	if asMap(headers["X-Required"])["required"] != true {
		t.Fatalf("required response header not preserved: %v", headers["X-Required"])
	}
	if _, ok := headers["Content-Type"]; ok {
		t.Fatalf("Content-Type must become response content, not header: %v", headers)
	}
}

func TestNormalizeHTTPDocumentHeaderRequiredDefaultsTrue(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "X-Trace", "type": "_types.basics.string", "description": "trace"},
			},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	params := asList(op["parameters"])
	if len(params) != 1 || asMap(params[0])["required"] != true {
		t.Fatalf("header required must default to true: %v", params)
	}
}

func TestNormalizeHTTPDocumentQueryRequiredDefaultsTrue(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"query": []any{
				map[string]any{"name": "q", "type": "_types.basics.string", "description": "query", "to": "params.q"},
			},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	params := asList(op["parameters"])
	if len(params) != 1 || asMap(params[0])["required"] != true {
		t.Fatalf("query required must default to true: %v", params)
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
					"body": map[string]any{"kind": "json", "type": "_types.basics.string", "to": "params.input"},
				},
				"response": map[string]any{"204": map[string]any{"msg": "ok"}},
			}},
		},
	}
	if err := NormalizeHTTPDocument(doc, testResolve, true, testNormalizeOptions()); err != nil {
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
					"body": map[string]any{"kind": "json", "schema": "_types.basics.string", "to": "params.input"},
				},
				"response": map[string]any{"204": map[string]any{"msg": "ok"}},
			}},
		},
	}
	err := NormalizeHTTPDocument(doc, testResolve, true, testNormalizeOptions())
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
					map[string]any{"name": "username", "type": "_types.basics.string", "description": "name", "to": "params.input.username"},
				},
			},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	body := asMap(op["requestBody"])
	if body == nil || asMap(asMap(body["content"])["application/x-www-form-urlencoded"]) == nil {
		t.Fatalf("form body not normalized: %v", op["requestBody"])
	}
	schema := asMap(asMap(asMap(body["content"])["application/x-www-form-urlencoded"])["schema"])
	if required := asList(schema["required"]); len(required) != 1 || required[0] != "username" {
		t.Fatalf("form field required must default to true: %v", schema)
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
			"response": map[string]any{"204": map[string]any{"msg": "ok"}},
		}},
		{"request expect", map[string]any{
			"request": map[string]any{
				"headers": []any{map[string]any{"name": "Expect", "value": "100-continue", "required": false, "description": "transport"}},
			},
			"response": map[string]any{"204": map[string]any{"msg": "ok"}},
		}},
		{"response content-length", map[string]any{
			"request": map[string]any{},
			"response": map[string]any{
				"200": map[string]any{
					"msg":     "ok",
					"headers": []any{map[string]any{"name": "Content-Length", "type": "_types.basics.integer", "required": false, "description": "length"}},
					"body":    map[string]any{"kind": "json", "type": "_types.basics.string"},
				},
			},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := httpDoc(tc.op)
			err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions("Connection", "Content-Length", "Expect"))
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
				map[string]any{"name": "Upgrade-Insecure-Requests", "type": "_types.basics.string", "required": false, "description": "browser hint"},
			},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions("Connection", "Content-Length", "Expect")); err != nil {
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
					map[string]any{"name": "nickname", "type": "_types.basics.string", "required": true, "description": "name", "to": "params.input.nickname"},
					map[string]any{"name": "avatar", "kind": "file", "required": false, "description": "file", "to": "params.input.avatar"},
				},
			},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
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
				map[string]any{"name": "Authorization", "auth": "Bearer", "required": true, "description": "token"},
			},
		},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	security := asList(op["security"])
	if len(security) != 1 || asMap(security[0])["Bearer"] == nil {
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
			"204": map[string]any{"msg": "ok"},
			"409": map[string]any{"msg": "conflict", "errors": []any{"platform.errors.ACCOUNT_NUMBER_TAKEN"}},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
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
	if resp["x-response-msg"] != "conflict" {
		t.Fatalf("x-response-msg not preserved: %v", resp)
	}
}

func TestNormalizeHTTPDocumentResponseMsgRequired(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{},
		"response": map[string]any{
			"204": map[string]any{},
		},
	})
	err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions())
	if err == nil || !strings.Contains(err.Error(), "msg required") {
		t.Fatalf("missing msg not rejected: %v", err)
	}
}

func TestNormalizeHTTPDocumentResponseMsgBecomesDescription(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{},
		"response": map[string]any{
			"200": map[string]any{"msg": "成功"},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	resp := asMap(asMap(asMap(asMap(doc["paths"])["/x"])["post"])["responses"])["200"]
	respMap, _ := resp.(map[string]any)
	if respMap["description"] != "成功" {
		t.Fatalf("msg must become OpenAPI description: %v", resp)
	}
}

func TestNormalizeHTTPDocumentRejectsFlatErrors(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request":  map[string]any{},
		"response": map[string]any{"204": map[string]any{"msg": "ok"}},
		"errors":   []any{"platform.errors.ACCOUNT_NOT_FOUND"},
	})
	err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions())
	if err == nil || !strings.Contains(err.Error(), "response.<status>.errors") {
		t.Fatalf("flat errors not rejected: %v", err)
	}
}

func TestNormalizeHTTPDocumentRejectsEmptyCodes(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request":  map[string]any{},
		"response": map[string]any{"409": map[string]any{"msg": "conflict", "errors": []any{}}},
	})
	err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions())
	if err == nil || !strings.Contains(err.Error(), "requires non-empty errors") {
		t.Fatalf("empty errors not rejected: %v", err)
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
	err := NormalizeHTTPDocument(doc, nil, true, testNormalizeOptions())
	if err == nil || !strings.Contains(err.Error(), "components") {
		t.Fatalf("components not rejected: %v", err)
	}
}

func TestNormalizeHTTPRequiredDefaults(t *testing.T) {
	doc := httpDoc(map[string]any{
		"request": map[string]any{
			"headers": []any{
				map[string]any{"name": "Content-Type", "value": "application/x-www-form-urlencoded", "description": "body"},
				map[string]any{"name": "X-Trace", "value": "1", "description": "trace"},
			},
			"query": []any{
				map[string]any{"name": "limit", "type": "_types.basics.integer", "description": "size", "to": "params.limit"},
			},
			"body": map[string]any{
				"kind": "form",
				"fields": []any{
					map[string]any{"name": "username", "type": "_types.basics.string", "description": "name", "to": "params.input.username"},
				},
			},
		},
		"response": map[string]any{
			"200": map[string]any{
				"msg": "ok",
				"headers": []any{
					map[string]any{"name": "Content-Type", "value": "application/json", "description": "body"},
					map[string]any{"name": "X-Total", "value": "1", "description": "total"},
				},
				"body": map[string]any{"kind": "json", "type": "_types.basics.string"},
			},
		},
	})
	if err := NormalizeHTTPDocument(doc, testResolve, false, testNormalizeOptions()); err != nil {
		t.Fatal(err)
	}
	op := asMap(asMap(asMap(doc["paths"])["/x"])["post"])
	for _, raw := range asList(op["parameters"]) {
		param := asMap(raw)
		name, _ := param["name"].(string)
		if name != "X-Trace" && name != "limit" {
			continue
		}
		if required, _ := param["required"].(bool); !required {
			t.Fatalf("%s required must default to true: %v", name, param)
		}
	}

	body := asMap(op["requestBody"])
	if required, _ := body["required"].(bool); !required {
		t.Fatalf("body required must default to true: %v", body)
	}
	schema := asMap(asMap(asMap(body["content"])["application/x-www-form-urlencoded"])["schema"])
	if required := asList(schema["required"]); len(required) != 1 || required[0] != "username" {
		t.Fatalf("form field required must default to true: %v", schema)
	}

	resp := asMap(asMap(op["responses"])["200"])
	total := asMap(asMap(resp["headers"])["X-Total"])
	if required, _ := total["required"].(bool); !required {
		t.Fatalf("response header required must default to true: %v", total)
	}
}

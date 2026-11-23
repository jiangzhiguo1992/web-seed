// Package spec 校验生成的 OpenAPI 3.1 spec 结构。
package spec

import (
	"fmt"
	"strings"
)

// DefaultResponseName 返回默认错误状态码对应的 components.responses 名称。
// 契约与生成器共用同一张表，避免默认响应被静默跳过。
func DefaultResponseName(status int) (string, bool) {
	name, ok := defaultResponseNames[status]
	return name, ok
}

var defaultResponseNames = map[int]string{
	400: "BadRequest",
	401: "Unauthorized",
	403: "Forbidden",
	404: "NotFound",
	409: "Conflict",
	410: "Gone",
	413: "PayloadTooLarge",
	415: "UnsupportedMediaType",
	422: "UnprocessableEntity",
	423: "Locked",
	429: "TooManyRequests",
	500: "Internal",
	503: "ServiceUnavailable",
}

// Validate 检查 spec 的关键结构：路径/参数/响应/请求体、operationId、$ref 与 required。
func Validate(spec map[string]any) error {
	if spec["openapi"] == nil {
		return fmt.Errorf("openapi version required")
	}
	paths := asMap(spec["paths"])
	if paths == nil {
		return fmt.Errorf("paths required")
	}
	components := asMap(spec["components"])
	schemas := asMap(components["schemas"])
	responses := asMap(components["responses"])
	headers := asMap(components["headers"])
	parameters := asMap(components["parameters"])

	seen := map[string]bool{}
	for path, raw := range paths {
		if !strings.HasPrefix(path, "/") {
			return fmt.Errorf("path %q must start with /", path)
		}
		item := asMap(raw)
		if item == nil {
			return fmt.Errorf("path %s: path item must be an object", path)
		}
		if err := validatePathParameters(item, path); err != nil {
			return err
		}
		for method, rawOp := range item {
			if !isHTTPMethod(method) {
				if !isPathItemKey(method) && !strings.HasPrefix(method, "x-") {
					return fmt.Errorf("%s %s: unknown path item key", method, path)
				}
				continue
			}
			op := asMap(rawOp)
			if op == nil {
				return fmt.Errorf("%s %s: operation must be an object", method, path)
			}
			id, _ := op["operationId"].(string)
			if id == "" {
				return fmt.Errorf("%s %s: operationId required", method, path)
			}
			if seen[id] {
				return fmt.Errorf("duplicate operationId %s", id)
			}
			seen[id] = true
			if err := validateOperation(op, method, path); err != nil {
				return err
			}
			if err := checkRefs(op, schemas, responses, headers, parameters); err != nil {
				return fmt.Errorf("%s %s: %w", method, path, err)
			}
		}
	}
	for name, raw := range schemas {
		schema := asMap(raw)
		if schema == nil {
			continue
		}
		if err := checkRequired(schema, name); err != nil {
			return err
		}
		if err := checkSchemaTypes(schema, name); err != nil {
			return err
		}
		if err := checkRefs(schema, schemas, responses, headers, parameters); err != nil {
			return fmt.Errorf("schema %s: %w", name, err)
		}
	}
	for name, raw := range responses {
		response := asMap(raw)
		if response == nil {
			return fmt.Errorf("component response %s: must be an object", name)
		}
		if err := validateResponse(response, "component response "+name); err != nil {
			return err
		}
	}
	for name, raw := range parameters {
		param := asMap(raw)
		if param == nil {
			return fmt.Errorf("component parameter %s: must be an object", name)
		}
		if err := validateParameter(param, "component parameter "+name); err != nil {
			return err
		}
	}
	return nil
}

var httpMethods = map[string]bool{
	"get": true, "put": true, "post": true, "delete": true,
	"options": true, "head": true, "patch": true, "trace": true,
}

func isHTTPMethod(key string) bool { return httpMethods[key] }

func isPathItemKey(key string) bool {
	switch key {
	case "$ref", "summary", "description", "servers", "parameters":
		return true
	}
	return false
}

// validateOperation 校验 operation 的响应、请求体与参数形状。
func validateOperation(op map[string]any, method, path string) error {
	where := method + " " + path
	for key := range op {
		if operationKeys[key] || strings.HasPrefix(key, "x-") {
			continue
		}
		return fmt.Errorf("%s: unknown operation key %q", where, key)
	}
	if err := validateResponses(op["responses"], where); err != nil {
		return err
	}
	if body, ok := op["requestBody"]; ok {
		if err := validateRequestBody(body, where); err != nil {
			return err
		}
	}
	if raw, ok := op["parameters"]; ok {
		if err := validateParameters(raw, where); err != nil {
			return err
		}
	}
	return nil
}

var operationKeys = map[string]bool{
	"tags": true, "summary": true, "description": true, "externalDocs": true,
	"operationId": true, "parameters": true, "requestBody": true, "responses": true,
	"callbacks": true, "deprecated": true, "security": true, "servers": true,
}

// validateResponses 要求每组操作至少一个响应，状态码合法，响应有 description 或 $ref。
func validateResponses(raw any, where string) error {
	responses := asMap(raw)
	if responses == nil {
		return fmt.Errorf("%s: responses required", where)
	}
	if len(responses) == 0 {
		return fmt.Errorf("%s: responses must not be empty", where)
	}
	for status, value := range responses {
		if status != "default" {
			if len(status) != 3 || status[0] < '1' || status[0] > '5' || !isDigits(status) {
				return fmt.Errorf("%s: invalid response status %q", where, status)
			}
			n := 0
			for _, r := range status {
				n = n*10 + int(r-'0')
			}
			if n < 100 || n > 599 {
				return fmt.Errorf("%s: invalid response status %q", where, status)
			}
		}
		response := asMap(value)
		if response == nil {
			return fmt.Errorf("%s: response %s must be an object", where, status)
		}
		if err := validateResponse(response, where+" response "+status); err != nil {
			return err
		}
	}
	return nil
}

// validateResponse 校验 description/$ref 与 content 的 media type。
func validateResponse(response map[string]any, where string) error {
	if _, ok := response["$ref"]; ok {
		return nil
	}
	if s, _ := response["description"].(string); strings.TrimSpace(s) == "" {
		return fmt.Errorf("%s: description required", where)
	}
	if raw, ok := response["content"]; ok {
		if err := validateContent(raw, where); err != nil {
			return err
		}
	}
	return nil
}

// validateRequestBody 要求 requestBody.content 非空，且每个 media type 声明 schema。
func validateRequestBody(raw any, where string) error {
	body := asMap(raw)
	if body == nil {
		return fmt.Errorf("%s: requestBody must be an object", where)
	}
	if _, ok := body["$ref"]; ok {
		return nil
	}
	if err := validateContent(body["content"], where+" requestBody"); err != nil {
		return err
	}
	return nil
}

// validateContent 校验 content 是 map，且每个 media type 对象有 schema 或 $ref。
func validateContent(raw any, where string) error {
	content := asMap(raw)
	if content == nil || len(content) == 0 {
		return fmt.Errorf("%s: content must be a non-empty object", where)
	}
	for media, value := range content {
		mt := asMap(value)
		if mt == nil {
			return fmt.Errorf("%s: content %s must be an object", where, media)
		}
		if _, hasSchema := mt["schema"]; !hasSchema {
			if _, hasRef := mt["$ref"]; !hasRef {
				return fmt.Errorf("%s: content %s requires schema", where, media)
			}
		}
	}
	return nil
}

// validateParameters 校验参数形状、去重与 path 参数覆盖。
func validateParameters(raw any, where string) error {
	list := asList(raw)
	if list == nil {
		return fmt.Errorf("%s: parameters must be a list", where)
	}
	seen := map[string]bool{}
	for i, value := range list {
		param := asMap(value)
		if param == nil {
			return fmt.Errorf("%s: parameters[%d] must be an object", where, i)
		}
		if err := validateParameter(param, fmt.Sprintf("%s parameters[%d]", where, i)); err != nil {
			return err
		}
		name, _ := param["name"].(string)
		location, _ := param["in"].(string)
		if name != "" && location != "" {
			key := location + "\x00" + name
			if seen[key] {
				return fmt.Errorf("%s: duplicate parameter %s in %s", where, name, location)
			}
			seen[key] = true
		}
	}
	return nil
}

func validateParameter(param map[string]any, where string) error {
	if _, ok := param["$ref"]; ok {
		return nil
	}
	name, _ := param["name"].(string)
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s: name required", where)
	}
	location, _ := param["in"].(string)
	switch location {
	case "path", "query", "header":
	default:
		return fmt.Errorf("%s: in must be path/query/header", where)
	}
	if location == "path" {
		if required, _ := param["required"].(bool); !required {
			return fmt.Errorf("%s: path parameter %s must be required", where, name)
		}
	}
	if param["schema"] == nil && param["content"] == nil {
		return fmt.Errorf("%s: schema or content required", where)
	}
	return nil
}

// validatePathParameters 校验路径模板与 path 参数一一对应；$ref 参数无法静态解析时跳过。
func validatePathParameters(item map[string]any, path string) error {
	names := pathParamNames(path)
	pathLevel := map[string]bool{}
	for _, raw := range asList(item["parameters"]) {
		param := asMap(raw)
		if param == nil {
			continue
		}
		if _, ok := param["$ref"]; ok {
			return nil
		}
		if location, _ := param["in"].(string); location == "path" {
			name, _ := param["name"].(string)
			if !names[name] {
				return fmt.Errorf("path %s: parameter %q is not declared in the path template", path, name)
			}
			pathLevel[name] = true
		}
	}
	for method, rawOp := range item {
		if !isHTTPMethod(method) {
			continue
		}
		op := asMap(rawOp)
		if op == nil {
			continue
		}
		seen := map[string]bool{}
		for name := range pathLevel {
			seen[name] = true
		}
		hasRef := false
		for _, raw := range asList(op["parameters"]) {
			param := asMap(raw)
			if param == nil {
				continue
			}
			if _, ok := param["$ref"]; ok {
				hasRef = true
				continue
			}
			if location, _ := param["in"].(string); location == "path" {
				if name, _ := param["name"].(string); name != "" {
					seen[name] = true
					if !names[name] {
						return fmt.Errorf("path %s: %s parameter %q is not declared in the path template", path, method, name)
					}
				}
			}
		}
		if hasRef {
			continue
		}
		for name := range names {
			if !seen[name] {
				return fmt.Errorf("path %s: %s missing path parameter %q", path, method, name)
			}
		}
	}
	return nil
}

func pathParamNames(path string) map[string]bool {
	out := map[string]bool{}
	for {
		start := strings.IndexByte(path, '{')
		if start < 0 {
			return out
		}
		end := strings.IndexByte(path[start:], '}')
		if end < 0 {
			return out
		}
		out[path[start+1:start+end]] = true
		path = path[start+end+1:]
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var jsonSchemaTypes = map[string]bool{
	"object": true, "array": true, "string": true, "number": true,
	"integer": true, "boolean": true, "null": true,
}

// checkSchemaTypes 拒绝未展开的类型名（如本地类型未生成 components.schemas 别名）。
func checkSchemaTypes(schema map[string]any, name string) error {
	if typ, ok := schema["type"].(string); ok && !jsonSchemaTypes[typ] {
		return fmt.Errorf("schema %s: unresolved type %q", name, typ)
	}
	if props := asMap(schema["properties"]); props != nil {
		for field, raw := range props {
			if child := asMap(raw); child != nil {
				if err := checkSchemaTypes(child, name+"."+field); err != nil {
					return err
				}
			}
		}
	}
	if items := asMap(schema["items"]); items != nil {
		if err := checkSchemaTypes(items, name+".items"); err != nil {
			return err
		}
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		for i, raw := range asList(schema[key]) {
			if child := asMap(raw); child != nil {
				if err := checkSchemaTypes(child, fmt.Sprintf("%s.%s[%d]", name, key, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkRequired(schema map[string]any, name string) error {
	props := asMap(schema["properties"])
	for _, raw := range asList(schema["required"]) {
		field, _ := raw.(string)
		if field == "" {
			continue
		}
		if props[field] == nil {
			return fmt.Errorf("schema %s: required field %q not in properties", name, field)
		}
	}
	return nil
}

func checkRefs(v any, schemas, responses, headers, parameters map[string]any) error {
	switch x := v.(type) {
	case map[string]any:
		if ref, _ := x["$ref"].(string); ref != "" {
			switch {
			case strings.HasPrefix(ref, "#/components/schemas/"):
				if schemas[strings.TrimPrefix(ref, "#/components/schemas/")] == nil {
					return fmt.Errorf("unknown schema $ref %s", ref)
				}
			case strings.HasPrefix(ref, "#/components/responses/"):
				if responses[strings.TrimPrefix(ref, "#/components/responses/")] == nil {
					return fmt.Errorf("unknown response $ref %s", ref)
				}
			case strings.HasPrefix(ref, "#/components/headers/"):
				if headers[strings.TrimPrefix(ref, "#/components/headers/")] == nil {
					return fmt.Errorf("unknown header $ref %s", ref)
				}
			case strings.HasPrefix(ref, "#/components/parameters/"):
				if parameters[strings.TrimPrefix(ref, "#/components/parameters/")] == nil {
					return fmt.Errorf("unknown parameter $ref %s", ref)
				}
			default:
				return fmt.Errorf("unsupported $ref %s", ref)
			}
		}
		for _, child := range x {
			if err := checkRefs(child, schemas, responses, headers, parameters); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range x {
			if err := checkRefs(child, schemas, responses, headers, parameters); err != nil {
				return err
			}
		}
	}
	return nil
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

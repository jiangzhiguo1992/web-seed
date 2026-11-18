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
	422: "UnprocessableEntity",
	423: "Locked",
	429: "TooManyRequests",
	500: "Internal",
	503: "ServiceUnavailable",
}

// Validate 检查 spec 的关键结构：operationId、x-service、$ref 与 required。
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
		item := asMap(raw)
		for method, rawOp := range item {
			op := asMap(rawOp)
			if op == nil {
				continue
			}
			id, _ := op["operationId"].(string)
			if id == "" {
				return fmt.Errorf("%s %s: operationId required", method, path)
			}
			if seen[id] {
				return fmt.Errorf("duplicate operationId %s", id)
			}
			seen[id] = true
			if op["responses"] == nil {
				return fmt.Errorf("%s %s: responses required", method, path)
			}
			if op["x-service"] == nil {
				return fmt.Errorf("%s %s: x-service required", method, path)
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
	return nil
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

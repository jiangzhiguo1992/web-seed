// Package spec 校验生成的 OpenAPI 3.1 spec 结构。
package spec

import (
	"fmt"
	"strings"
)

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
			if op["x-service"] == nil && op["x-async"] == nil {
				return fmt.Errorf("%s %s: x-service or x-async required", method, path)
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
		if err := checkRefs(schema, schemas, responses, headers, parameters); err != nil {
			return fmt.Errorf("schema %s: %w", name, err)
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

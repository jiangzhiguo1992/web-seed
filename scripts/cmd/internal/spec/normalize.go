// normalize.go 把源契约的扁平 operation 与直接类型引用投影为 OpenAPI 形状。
// 源契约只写 name/id/service/request/response/errors；x-*、parameters、
// requestBody、responses 由这里生成，保证 checker 与生成器只面对一种中间形状。
package spec

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// TypeResolver 把 _types.<name> 解析成 OpenAPI 标量 schema。
type TypeResolver func(name string) (map[string]any, error)

var statusRe = func(s string) bool {
	if len(s) != 3 {
		return false
	}
	if s[0] < '1' || s[0] > '5' {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// HTTPOperationMeta 是源契约里不属于 OpenAPI 的 operation 元数据。
// audience 与 used_by 在 NormalizeHTTPDocument 之前读取，normalizer 会删除源字段。
type HTTPOperationMeta struct {
	UsedBy []string
}

// CollectHTTPOperationMeta 读取每个 operation 的 audience/used_by，键是契约 ID。
func CollectHTTPOperationMeta(doc map[string]any) map[string]HTTPOperationMeta {
	out := map[string]HTTPOperationMeta{}
	for _, rawItem := range asMap(doc["paths"]) {
		for method, rawOp := range asMap(rawItem) {
			op := asMap(rawOp)
			if op == nil || !isHTTPMethod(method) {
				continue
			}
			id, _ := op["id"].(string)
			if id == "" {
				if contract := asMap(op["contract"]); contract != nil {
					id, _ = contract["id"].(string)
				}
			}
			if id == "" {
				continue
			}
			meta := HTTPOperationMeta{}
			for _, raw := range asList(op["used_by"]) {
				if value, ok := raw.(string); ok {
					meta.UsedBy = append(meta.UsedBy, value)
				}
			}
			out[id] = meta
		}
	}
	return out
}

// HTTPMount 是文件级挂载：base path 与调用端。
type HTTPMount struct {
	Path     string
	Audience []string
}

// CollectHTTPMount 在 NormalizeHTTPDocument 之前读取 mount。
func CollectHTTPMount(doc map[string]any) HTTPMount {
	mount := asMap(doc["mount"])
	if mount == nil {
		return HTTPMount{}
	}
	out := HTTPMount{}
	out.Path, _ = mount["path"].(string)
	for _, raw := range asList(mount["audience"]) {
		if value, ok := raw.(string); ok {
			out.Audience = append(out.Audience, value)
		}
	}
	return out
}

// applyHTTPMount 把 paths 的相对 key 拼接到 mount.path，并删除 mount 源块。
func applyHTTPMount(doc map[string]any) error {
	paths := asMap(doc["paths"])
	if len(paths) == 0 {
		return nil
	}
	mount := asMap(doc["mount"])
	if mount == nil {
		return fmt.Errorf("mount block required when paths is non-empty")
	}
	base, _ := mount["path"].(string)
	if base == "" || !strings.HasPrefix(base, "/") {
		return fmt.Errorf("mount.path must start with /")
	}
	joined := map[string]any{}
	for local, item := range paths {
		if local == "" || !strings.HasPrefix(local, "/") {
			return fmt.Errorf("path %q must start with /", local)
		}
		full := base
		if local != "/" {
			full = strings.TrimSuffix(base, "/") + local
		}
		if _, exists := joined[full]; exists {
			return fmt.Errorf("duplicate path %s after mount", full)
		}
		joined[full] = item
	}
	doc["paths"] = joined
	delete(doc, "mount")
	return nil
}

// NormalizeHTTPDocument 原地把 components.schemas.*.source 与 paths.*.* 的
// name/id/service/request/response 转成 OpenAPI 形状。
func NormalizeHTTPDocument(doc map[string]any, resolve TypeResolver, strict bool) error {
	if err := applyHTTPMount(doc); err != nil {
		return err
	}
	if doc["components"] != nil {
		return fmt.Errorf("components is not a source field; declare shared")
	}
	if shared := asMap(doc["shared"]); shared != nil {
		doc["components"] = shared
		delete(doc, "shared")
	}
	if err := normalizeSharedResponses(doc); err != nil {
		return err
	}
	if err := ensureDirectComponents(doc); err != nil {
		return err
	}
	componentByID, err := normalizeHTTPComponents(doc, strict)
	if err != nil {
		return err
	}
	paths := asMap(doc["paths"])
	for path, rawItem := range paths {
		item := asMap(rawItem)
		for method, rawOp := range item {
			if !isHTTPMethod(method) {
				continue
			}
			op := asMap(rawOp)
			if op == nil {
				continue
			}
			where := method + " " + path
			if op["parameters"] != nil || op["requestBody"] != nil || op["responses"] != nil {
				return fmt.Errorf("%s: legacy parameters/requestBody/responses removed; use request/response", where)
			}
			if err := normalizeOperationMetadata(op, where, strict); err != nil {
				return err
			}
			request := asMap(op["request"])
			if request == nil {
				return fmt.Errorf("%s: request block required", where)
			}
			response := asMap(op["response"])
			if response == nil {
				return fmt.Errorf("%s: response block required", where)
			}
			parameters, requestBody, err := normalizeRequest(request, where, resolve, componentByID, strict)
			if err != nil {
				return err
			}
			if len(parameters) > 0 {
				op["parameters"] = parameters
			}
			if requestBody != nil {
				op["requestBody"] = requestBody
			}
			responses, err := normalizeResponse(response, where, resolve, componentByID, strict)
			if err != nil {
				return err
			}
			op["responses"] = responses
			delete(op, "request")
			delete(op, "response")
		}
	}
	return nil
}

// normalizeOperationMetadata 把 name/id/service/request.mapping/request.idempotency/errors
// 转成生成物需要的 operationId 与 x-* 扩展，并删掉源字段。
// ensureDirectComponents 为 operation 里的直接类型引用补出 components.schemas
// 溯源声明，使账号/鉴权文件不需要再写 components 别名。
func ensureDirectComponents(doc map[string]any) error {
	components := asMap(doc["components"])
	if components == nil {
		components = map[string]any{}
		doc["components"] = components
	}
	schemas := asMap(components["schemas"])
	if schemas == nil {
		schemas = map[string]any{}
		components["schemas"] = schemas
	}
	var add func(string) error
	add = func(id string) error {
		if id == "" || strings.HasPrefix(id, "_types.") {
			return nil
		}
		name, kind := "", ""
		switch {
		case strings.HasPrefix(id, "data.entities."):
			name, kind = pascal(lastSegment(id)), "entity"
		case strings.HasPrefix(id, "data.enums."):
			name, kind = pascal(lastSegment(id)), "enum"
		case strings.HasPrefix(id, "cap.services.") && strings.Contains(id, ".schemas."):
			name, kind = lastSegment(id), "service_type"
		default:
			return nil
		}
		if raw, ok := schemas[name]; ok {
			component := asMap(raw)
			source := asMap(component["source"])
			if got, _ := source[kind].(string); got != "" && got != id {
				return fmt.Errorf("components.schemas.%s already sources %s, cannot also source %s", name, got, id)
			}
			return nil
		}
		schemas[name] = map[string]any{"source": map[string]any{kind: id}}
		return nil
	}
	var walk func(any) error
	walk = func(value any) error {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				if key == "schema" {
					if id, ok := child.(string); ok {
						if err := add(id); err != nil {
							return err
						}
					}
				}
				if id, ok := child.(string); ok {
					if err := add(id); err != nil {
						return err
					}
				}
				if err := walk(child); err != nil {
					return err
				}
			}
		case []any:
			for _, child := range v {
				if err := walk(child); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(doc["paths"])
}

func lastSegment(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

func pascal(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '_' || r == '-' })
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, "")
}

func normalizeOperationMetadata(op map[string]any, where string, strict bool) error {
	for _, legacy := range []string{"x-contract-id", "x-service", "x-timeout-ms", "x-idempotency", "x-service-params", "x-errors", "x-default-errors"} {
		if op[legacy] != nil {
			return fmt.Errorf("%s: legacy %s removed; use name/id/service/request/response", where, legacy)
		}
	}
	if op["errors"] != nil {
		return fmt.Errorf("%s: errors removed; group platform.errors.* under response.<status>.codes", where)
	}
	name, _ := op["name"].(string)
	if name == "" {
		return fmt.Errorf("%s: name required", where)
	}
	op["operationId"] = name
	delete(op, "name")

	contractID, _ := op["id"].(string)
	service, _ := op["service"].(string)
	if contract := asMap(op["contract"]); contract != nil {
		if strict {
			return fmt.Errorf("%s: contract block removed; use flat id/service", where)
		}
		if contractID == "" {
			contractID, _ = contract["id"].(string)
		}
		if service == "" {
			service, _ = contract["service"].(string)
		}
		if timeout, ok := contract["timeout_ms"]; ok {
			if n, ok := toInt(timeout); ok && n > 0 {
				op["x-timeout-ms"] = n
			}
		}
		delete(op, "contract")
	}
	if contractID == "" || service == "" {
		return fmt.Errorf("%s: id and service required", where)
	}
	op["x-contract-id"] = contractID
	op["x-service"] = service
	delete(op, "id")
	delete(op, "service")

	request := asMap(op["request"])
	if mapping, ok := request["mapping"]; ok {
		op["x-service-params"] = mapping
		delete(request, "mapping")
	}
	if idempotency, ok := request["idempotency"]; ok {
		op["x-idempotency"] = idempotency
		delete(request, "idempotency")
	}
	delete(op, "audience")
	delete(op, "used_by")
	return nil
}

// normalizeSharedResponses 把 shared.responses 的直接 header/schema 名转成 $ref。
func normalizeSharedResponses(doc map[string]any) error {
	components := asMap(doc["components"])
	responses := asMap(components["responses"])
	for name, raw := range responses {
		response := asMap(raw)
		if response == nil {
			continue
		}
		headers := asMap(response["headers"])
		for header, value := range headers {
			if ref, ok := value.(string); ok && ref != "" {
				headers[header] = map[string]any{"$ref": "#/components/headers/" + ref}
			}
		}
		content := asMap(response["content"])
		for _, rawMedia := range content {
			media := asMap(rawMedia)
			if media == nil {
				continue
			}
			if ref, ok := media["schema"].(string); ok && ref != "" {
				media["schema"] = map[string]any{"$ref": "#/components/schemas/" + ref}
			}
		}
		_ = name
	}
	return nil
}

// normalizeHTTPComponents 把 components.schemas.*.source 转成 x-* 溯源字段，
// 并返回 type ID → component 名映射，供直接引用解析。
func normalizeHTTPComponents(doc map[string]any, strict bool) (map[string]string, error) {
	byID := map[string]string{}
	components := asMap(doc["components"])
	schemas := asMap(components["schemas"])
	for name, raw := range schemas {
		component := asMap(raw)
		if component == nil {
			continue
		}
		source := asMap(component["source"])
		if source == nil {
			if strict && (component["x-service-type"] != nil || component["x-enum"] != nil || component["x-entity"] != nil || component["x-platform"] != nil) {
				return nil, fmt.Errorf("components.schemas.%s: x-* markers removed; use source block", name)
			}
			continue
		}
		if component["x-service-type"] != nil || component["x-enum"] != nil || component["x-entity"] != nil || component["x-platform"] != nil {
			return nil, fmt.Errorf("components.schemas.%s: source block cannot be combined with x-* markers", name)
		}
		count := 0
		if value, _ := source["service_type"].(string); value != "" {
			count++
			component["x-service-type"] = value
			byID[value] = name
		}
		if value, _ := source["enum"].(string); value != "" {
			count++
			component["x-enum"] = value
			byID[value] = name
		}
		if value, _ := source["entity"].(string); value != "" {
			count++
			component["x-entity"] = value
			byID[value] = name
		}
		if value, _ := source["platform"].(bool); value {
			count++
			component["x-platform"] = true
		}
		if count != 1 {
			return nil, fmt.Errorf("components.schemas.%s: source must declare exactly one of service_type/enum/entity/platform:true", name)
		}
		for key := range source {
			switch key {
			case "service_type", "enum", "entity", "platform":
			default:
				return nil, fmt.Errorf("components.schemas.%s: unknown source key %s", name, key)
			}
		}
		delete(component, "source")
	}
	return byID, nil
}

func normalizeRequest(request map[string]any, where string, resolve TypeResolver, componentByID map[string]string, strict bool) ([]any, map[string]any, error) {
	for key := range request {
		switch key {
		case "path", "query", "headers", "cookies", "body":
		default:
			return nil, nil, fmt.Errorf("%s: unknown request key %s", where, key)
		}
	}
	parameters := []any{}
	for _, section := range []struct {
		name string
		in   string
	}{
		{"path", "path"},
		{"query", "query"},
		{"headers", "header"},
		{"cookies", "cookie"},
	} {
		raw := asMap(request[section.name])
		if raw == nil {
			continue
		}
		for _, name := range sortedKeys(raw) {
			param, err := normalizeParameter(name, section.in, raw[name], where, resolve, componentByID, strict)
			if err != nil {
				return nil, nil, err
			}
			parameters = append(parameters, param)
		}
	}
	body, err := normalizeBody(asMap(request["body"]), where, "requestBody", resolve, componentByID, strict)
	if err != nil {
		return nil, nil, err
	}
	return parameters, body, nil
}

func normalizeParameter(name, in string, raw any, where string, resolve TypeResolver, componentByID map[string]string, strict bool) (map[string]any, error) {
	if ref, ok := raw.(string); ok && ref != "" {
		if in == "header" || in == "cookie" {
			return map[string]any{"$ref": "#/components/parameters/" + ref}, nil
		}
		schema, err := normalizeHTTPSchema(ref, resolve, componentByID, strict)
		if err != nil {
			return nil, fmt.Errorf("%s: %s parameter %s: %w", where, in, name, err)
		}
		return map[string]any{"name": name, "in": in, "required": in == "path", "schema": schema}, nil
	}
	param := asMap(raw)
	if param == nil {
		return nil, fmt.Errorf("%s: %s parameter %s must be an object or type ref", where, in, name)
	}
	if ref, _ := param["$ref"].(string); ref != "" {
		return map[string]any{"$ref": ref}, nil
	}
	schemaRaw, ok := param["schema"]
	if !ok {
		return nil, fmt.Errorf("%s: %s parameter %s requires schema", where, in, name)
	}
	schema, err := normalizeHTTPSchema(schemaRaw, resolve, componentByID, strict)
	if err != nil {
		return nil, fmt.Errorf("%s: %s parameter %s: %w", where, in, name, err)
	}
	out := map[string]any{"name": name, "in": in, "schema": schema}
	if in == "path" {
		out["required"] = true
	} else if required, ok := param["required"].(bool); ok {
		out["required"] = required
	} else {
		out["required"] = false
	}
	if description, _ := param["description"].(string); description != "" {
		out["description"] = description
	}
	return out, nil
}

func normalizeBody(body map[string]any, where, field string, resolve TypeResolver, componentByID map[string]string, strict bool) (map[string]any, error) {
	if body == nil {
		return nil, nil
	}
	contentType, _ := body["content_type"].(string)
	if contentType == "" {
		return nil, fmt.Errorf("%s: %s.content_type required", where, field)
	}
	schema, err := normalizeHTTPSchema(body["schema"], resolve, componentByID, strict)
	if err != nil {
		return nil, fmt.Errorf("%s: %s.schema: %w", where, field, err)
	}
	required := true
	if value, ok := body["required"].(bool); ok {
		required = value
	}
	return map[string]any{
		"required": required,
		"content":  map[string]any{contentType: map[string]any{"schema": schema}},
	}, nil
}

func normalizeResponse(response map[string]any, where string, resolve TypeResolver, componentByID map[string]string, strict bool) (map[string]any, error) {
	out := map[string]any{}
	for status, raw := range response {
		if !statusRe(status) {
			return nil, fmt.Errorf("%s: response status %q must be a 3-digit HTTP status", where, status)
		}
		resp := asMap(raw)
		if resp == nil {
			return nil, fmt.Errorf("%s: response %s must be an object", where, status)
		}
		code, _ := strconv.Atoi(status)
		if code < 200 || code >= 300 {
			codes := asList(resp["codes"])
			if len(codes) == 0 {
				return nil, fmt.Errorf("%s: response %s requires non-empty codes", where, status)
			}
			if resp["description"] != nil || resp["headers"] != nil || resp["body"] != nil {
				return nil, fmt.Errorf("%s: response %s only allows codes for non-2xx", where, status)
			}
			name, ok := DefaultResponseName(code)
			if !ok {
				return nil, fmt.Errorf("%s: response %s has no shared response mapping", where, status)
			}
			for _, rawCode := range codes {
				if value, _ := rawCode.(string); strings.TrimSpace(value) == "" {
					return nil, fmt.Errorf("%s: response %s codes must be non-empty platform.errors.* IDs", where, status)
				}
			}
			out[status] = map[string]any{
				"$ref":          "#/components/responses/" + name,
				"x-error-codes": codes,
			}
			continue
		}
		if resp["codes"] != nil {
			return nil, fmt.Errorf("%s: response %s is 2xx and must not declare codes", where, status)
		}
		description, _ := resp["description"].(string)
		if strings.TrimSpace(description) == "" {
			return nil, fmt.Errorf("%s: response %s description required", where, status)
		}
		normalized := map[string]any{"description": description}
		if headers := asMap(resp["headers"]); len(headers) > 0 {
			normalizedHeaders := map[string]any{}
			for name, rawHeader := range headers {
				if ref, ok := rawHeader.(string); ok && ref != "" {
					normalizedHeaders[name] = map[string]any{"$ref": "#/components/headers/" + ref}
					continue
				}
				normalizedHeaders[name] = rawHeader
			}
			normalized["headers"] = normalizedHeaders
		}
		if body := asMap(resp["body"]); body != nil {
			contentType, _ := body["content_type"].(string)
			if contentType == "" {
				return nil, fmt.Errorf("%s: response %s body.content_type required", where, status)
			}
			schema, err := normalizeHTTPSchema(body["schema"], resolve, componentByID, strict)
			if err != nil {
				return nil, fmt.Errorf("%s: response %s body.schema: %w", where, status, err)
			}
			normalized["content"] = map[string]any{contentType: map[string]any{"schema": schema}}
		}
		out[status] = normalized
	}
	return out, nil
}

// normalizeHTTPSchema 把直接类型引用或 _types 节点转成 OpenAPI schema。
// 旧 $ref/裸 JSON 类型仍接受，供迁移期测试使用；新契约统一用直接引用。
func normalizeHTTPSchema(raw any, resolve TypeResolver, componentByID map[string]string, strict bool) (map[string]any, error) {
	switch value := raw.(type) {
	case string:
		if strings.HasPrefix(value, "_types.") {
			return resolve(value)
		}
		if name, ok := componentByID[value]; ok {
			return map[string]any{"$ref": "#/components/schemas/" + name}, nil
		}
		return nil, fmt.Errorf("unknown schema ref %s", value)
	case map[string]any:
		if ref, _ := value["$ref"].(string); ref != "" {
			if strict {
				return nil, fmt.Errorf("schema $ref removed; use direct type id")
			}
			return value, nil
		}
		typ, _ := value["type"].(string)
		if strings.HasPrefix(typ, "_types.") {
			switch typ {
			case "_types.object":
				properties := map[string]any{}
				rawProperties := asMap(value["properties"])
				for name, rawProperty := range rawProperties {
					child, err := normalizeHTTPSchema(rawProperty, resolve, componentByID, strict)
					if err != nil {
						return nil, fmt.Errorf("properties.%s: %w", name, err)
					}
					properties[name] = child
				}
				out := map[string]any{
					"type":                 "object",
					"additionalProperties": false,
					"properties":           properties,
				}
				if required := asList(value["required"]); len(required) > 0 {
					out["required"] = required
				}
				if description, _ := value["description"].(string); description != "" {
					out["description"] = description
				}
				return out, nil
			case "_types.list":
				item, err := normalizeHTTPSchema(value["items"], resolve, componentByID, strict)
				if err != nil {
					return nil, fmt.Errorf("items: %w", err)
				}
				return map[string]any{"type": "array", "items": item}, nil
			default:
				out, err := resolve(typ)
				if err != nil {
					return nil, err
				}
				applyHTTPScalarParams(out, value)
				if description, _ := value["description"].(string); description != "" {
					out["description"] = description
				}
				return out, nil
			}
		}
		// 迁移期：裸 JSON 类型与标准 object 仍接受。
		if typ == "object" && value["properties"] != nil {
			if strict {
				return nil, fmt.Errorf("standard object schema removed; use type: _types.object")
			}
			properties := map[string]any{}
			for name, rawProperty := range asMap(value["properties"]) {
				child, err := normalizeHTTPSchema(rawProperty, resolve, componentByID, strict)
				if err != nil {
					return nil, fmt.Errorf("properties.%s: %w", name, err)
				}
				properties[name] = child
			}
			out := map[string]any{"type": "object", "additionalProperties": false, "properties": properties}
			if required := asList(value["required"]); len(required) > 0 {
				out["required"] = required
			}
			return out, nil
		}
		if strict {
			return nil, fmt.Errorf("schema must be a direct type id or _types node")
		}
		return value, nil
	}
	return nil, fmt.Errorf("schema must be a direct type ref or _types node")
}

func applyHTTPScalarParams(out map[string]any, node map[string]any) {
	for key, value := range node {
		switch key {
		case "min_len":
			out["minLength"] = value
		case "max_len":
			out["maxLength"] = value
		case "min":
			out["minimum"] = value
		case "max":
			out["maximum"] = value
		case "pattern":
			out["pattern"] = value
		case "default":
			out["default"] = value
		}
	}
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for key := range m {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case string:
		parsed, err := strconv.Atoi(n)
		return parsed, err == nil
	}
	return 0, false
}

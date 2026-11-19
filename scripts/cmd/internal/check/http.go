// http.go 校验 cap.protocols.http 的 OpenAPI 绑定。
package check

import (
	"fmt"
	"path/filepath"
	"strings"

	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
)

// allowedOperationExtensions 是 operation 允许的 x-* 扩展键。
var allowedOperationExtensions = map[string]bool{
	"x-contract-id": true, "x-service": true,
	"x-timeout-ms": true, "x-default-errors": true,
}

func (c *Checker) checkHTTPBindings(schema *Schema, serviceMeta map[string]serviceMeta, typeNames map[string]bool, jobServices map[string]bool) {
	httpFiles := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
	httpSchemas := map[string]bool{}
	httpHeaders := map[string]bool{}
	httpResponses := map[string]bool{}
	var serviceUnavailable map[string]any
	hasDefault503 := false
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkTargetFileName(schema, filepath.Base(rel), rel)
		c.checkStability(doc, rel)
		if want := schema.Targets.FileID(filepath.Base(rel)); doc["id"] != want {
			c.rulef("http-file", "%s: id %q must be %q", rel, doc["id"], want)
		}
		components, _ := asMap(doc["components"])
		headers, _ := asMap(components["headers"])
		for name := range headers {
			httpHeaders[name] = true
		}
		responses, _ := asMap(components["responses"])
		for name := range responses {
			httpResponses[name] = true
		}
		if raw, ok := responses["ServiceUnavailable"]; ok {
			serviceUnavailable, _ = asMap(raw)
		}
		for _, raw := range asList(doc["default_error_responses"]) {
			if n, ok := toInt(raw); ok && n == 503 {
				hasDefault503 = true
			}
		}
		schemas, _ := asMap(components["schemas"])
		for name, raw := range schemas {
			httpSchemas[name] = true
			s, _ := asMap(raw)
			if ref, _ := s["x-service-type"].(string); ref != "" {
				if !match(`^cap\.services\.[a-z][a-z0-9_]*\.schemas\.[A-Za-z][A-Za-z0-9]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.x-service-type must match cap.services.<file>.schemas.<Type>", rel, name)
				} else if typ, ok := serviceTypeRef(ref); !ok || !typeNames[typ] {
					c.rulef("http-components", "%s: components.schemas.%s.x-service-type references unknown %s", rel, name, ref)
				} else if props, ok := asMap(s["properties"]); ok {
					if def := findTypeSchema(serviceMeta, typ); def != nil {
						if !sameSchemaFields(props, schemaPropertyMap(def["properties"])) {
							c.rulef("http-service-type", "%s: components.schemas.%s fields differ from %s", rel, name, ref)
						}
					}
				}
			}
			if ref, _ := s["x-enum"].(string); ref != "" {
				if !match(`^data\.enums\.[a-z][a-z0-9_]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.x-enum must match data.enums.<name>", rel, name)
				} else if !c.enumIDs[ref] {
					c.rulef("http-components", "%s: components.schemas.%s.x-enum references unknown enum %s", rel, name, ref)
				}
			}
			if ref, _ := s["x-entity"].(string); ref != "" {
				if !match(`^data\.entities\.[a-z][a-z0-9_]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.x-entity must match data.entities.<name>", rel, name)
				} else if _, ok := c.entities[ref]; !ok {
					c.rulef("http-components", "%s: components.schemas.%s.x-entity references unknown entity %s", rel, name, ref)
				}
			}
			c.checkClosedObjects(s, rel, "components.schemas."+name)
		}
	}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		rel := relPath(c.root, file)
		collectRefs(doc, func(ref string) {
			switch {
			case strings.HasPrefix(ref, "#/components/schemas/"):
				if name := strings.TrimPrefix(ref, "#/components/schemas/"); !httpSchemas[name] {
					c.rulef("http-components", "%s: unknown schema $ref %s", rel, ref)
				}
			case strings.HasPrefix(ref, "#/components/headers/"):
				if name := strings.TrimPrefix(ref, "#/components/headers/"); !httpHeaders[name] {
					c.rulef("http-components", "%s: unknown header $ref %s", rel, ref)
				}
			case strings.HasPrefix(ref, "#/components/responses/"):
				if name := strings.TrimPrefix(ref, "#/components/responses/"); !httpResponses[name] {
					c.rulef("http-components", "%s: unknown response $ref %s", rel, ref)
				}
			default:
				c.rulef("http-components", "%s: unsupported $ref %s; use #/components/{schemas,headers,responses}/<Name>", rel, ref)
			}
		})
	}
	if hasDefault503 {
		if !httpHeaders["RetryAfter"] {
			c.rulef("http-retry-after", "default_error_responses includes 503 but components.headers.RetryAfter is missing")
		}
		if serviceUnavailable == nil {
			c.rulef("http-retry-after", "default_error_responses includes 503 but components.responses.ServiceUnavailable is missing")
		} else if !retryAfterHeader(serviceUnavailable) {
			c.rulef("http-retry-after", "components.responses.ServiceUnavailable must declare Retry-After")
		}
	}
	contractIDs := map[string]string{}
	operationIDs := map[string]string{}
	operations := map[string]string{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		fileID, _ := doc["id"].(string)
		components, _ := asMap(doc["components"])
		schemas, _ := asMap(components["schemas"])
		defaultErrors := map[string]bool{}
		for _, raw := range asList(doc["default_error_responses"]) {
			n, ok := toInt(raw)
			if !ok || n < 100 || n > 599 {
				continue // 范围/类型由 _meta 的 min/max 与 integer 报出
			}
			name, ok := openapispec.DefaultResponseName(n)
			if !ok {
				c.rulef("http-default-errors", "%s: default status %d has no components.responses mapping", rel, n)
			} else if !httpResponses[name] {
				c.rulef("http-default-errors", "%s: default status %d requires components.responses.%s", rel, n, name)
			}
			defaultErrors[fmt.Sprint(raw)] = true
		}
		paths, _ := asMap(doc["paths"])
		for path, raw := range paths {
			p, _ := asMap(raw)
			for method, opRaw := range p {
				op, ok := asMap(opRaw)
				if !ok {
					continue
				}
				for i, rawParam := range asList(op["parameters"]) {
					param, _ := asMap(rawParam)
					if s, ok := asMap(param["schema"]); ok {
						c.checkClosedObjects(s, rel, fmt.Sprintf("%s %s parameters[%d]", method, path, i))
					}
				}
				for key := range op {
					if !strings.HasPrefix(key, "x-") || key == "x-flow" {
						continue
					}
					if !allowedOperationExtensions[key] {
						c.rulef("http-operation", "%s %s %s: unknown operation extension %s", rel, method, path, key)
					}
				}
				if prev, exists := operations[path+"\x00"+method]; exists {
					c.rulef("http-operation", "%s %s %s: duplicate operation (%s)", rel, method, path, prev)
				} else {
					operations[path+"\x00"+method] = rel
				}
				opID, _ := op["operationId"].(string)
				if opID == "" {
					c.rulef("http-operation", "%s %s %s: operationId required", rel, method, path)
				} else if prev, exists := operationIDs[opID]; exists {
					c.rulef("http-operation", "%s %s %s: duplicate operationId %s (also in %s)", rel, method, path, opID, prev)
				} else {
					operationIDs[opID] = rel
				}
				contractID, _ := op["x-contract-id"].(string)
				switch {
				case contractID == "":
					c.rulef("http-operation", "%s %s %s: x-contract-id required", rel, method, path)
				case !match(`^cap\.protocols\.http\.[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`, contractID):
					c.rulef("http-operation", "%s %s %s: x-contract-id %q must match cap.protocols.http.<file>.<operation>", rel, method, path, contractID)
				default:
					if prev, exists := contractIDs[contractID]; exists {
						c.rulef("http-operation", "%s %s %s: duplicate x-contract-id %s (also in %s)", rel, method, path, contractID, prev)
					} else {
						contractIDs[contractID] = rel
					}
					if fileID != "" && !strings.HasPrefix(contractID, fileID+".") {
						c.rulef("http-operation", "%s %s %s: x-contract-id %s must start with %s.", rel, method, path, contractID, fileID)
					}
				}
				if raw, ok := op["x-timeout-ms"]; ok {
					if n, ok := toInt(raw); !ok || n <= 0 {
						c.rulef("http-operation", "%s %s %s: x-timeout-ms must be a positive integer", rel, method, path)
					}
				}
				effectiveDefaults := defaultErrors
				if raw, ok := op["x-default-errors"]; ok {
					effectiveDefaults = map[string]bool{}
					for _, item := range asList(raw) {
						n, ok := toInt(item)
						if !ok || n < 100 || n > 599 {
							c.rulef("http-default-errors", "%s %s %s: x-default-errors must contain HTTP status codes 100-599, got %v", rel, method, path, item)
							continue
						}
						name, ok := openapispec.DefaultResponseName(n)
						if !ok {
							c.rulef("http-default-errors", "%s %s %s: x-default-errors status %d has no components.responses mapping", rel, method, path, n)
						} else if !httpResponses[name] {
							c.rulef("http-default-errors", "%s %s %s: x-default-errors status %d requires components.responses.%s", rel, method, path, n, name)
						}
						effectiveDefaults[fmt.Sprint(n)] = true
					}
				}
				responses, _ := asMap(op["responses"])
				for status, rawResp := range responses {
					if effectiveDefaults[status] {
						c.rulef("http-default-errors", "%s %s %s: status %s is injected by default errors; do not repeat it", rel, method, path, status)
					}
					resp, _ := asMap(rawResp)
					if content, ok := asMap(resp["content"]); ok {
						for _, rawMedia := range content {
							media, _ := asMap(rawMedia)
							mediaSchema, _ := asMap(media["schema"])
							c.checkClosedObjects(mediaSchema, rel, method+" "+path+" responses")
							ret, _ := mediaSchema["x-service-return"].(string)
							if ret == "" {
								continue
							}
							if !match(`^(data\.entities\.[a-z][a-z0-9_]*|cap\.services\.[a-z][a-z0-9_]*\.schemas\.[A-Za-z][A-Za-z0-9]*)$`, ret) {
								c.rulef("http-service-return", "%s %s %s: x-service-return %s must be data.entities.* or cap.services.<file>.schemas.<Type>", rel, method, path, ret)
								continue
							}
							ref, _ := op["x-service"].(string)
							target, foundOK := serviceMeta[ref]
							if !foundOK {
								continue
							}
							found := false
							for _, rt := range target.returns {
								if rt.typ == ret {
									found = true
									break
								}
							}
							if !found {
								c.rulef("http-service-return", "%s %s %s: x-service-return %s is not a return of %s", rel, method, path, ret, ref)
							}
						}
					}
				}
				if raw, ok := responses["503"]; ok {
					resp, _ := asMap(raw)
					if !retryAfterHeader(resp) {
						c.rulef("http-retry-after", "%s %s %s: 503 response must declare Retry-After", rel, method, path)
					}
				}
				if op["x-flow"] != nil {
					c.rulef("http-operation", "%s %s %s: x-flow removed; use x-service", rel, method, path)
				}
				collectRefs(op, func(ref string) {
					if strings.HasPrefix(ref, "#/components/schemas/") {
						name := strings.TrimPrefix(ref, "#/components/schemas/")
						if !httpSchemas[name] {
							c.rulef("http-components", "%s %s %s: unknown schema $ref %s", rel, method, path, ref)
						}
					}
				})
				serviceRef, _ := op["x-service"].(string)
				if serviceRef == "" {
					c.rulef("http-operation", "%s %s %s: x-service required", rel, method, path)
					continue
				}
				target, ok := serviceMeta[serviceRef]
				if !ok {
					c.rulef("http-operation", "%s %s %s: unknown service %s", rel, method, path, serviceRef)
					continue
				}
				if jobServices[serviceRef] {
					c.rulef("async-service", "%s %s %s: service %s is used by a cap.events job and cannot be bound to http", rel, method, path, serviceRef)
					continue
				}
				if target.visibility != "public" {
					c.rulef("service-visibility", "%s %s %s: service %s is not public", rel, method, path, serviceRef)
				}
				if rb, ok := asMap(op["requestBody"]); ok {
					if content, ok := asMap(rb["content"]); ok {
						for _, rawMedia := range content {
							media, _ := asMap(rawMedia)
							mediaSchema, _ := asMap(media["schema"])
							c.checkClosedObjects(mediaSchema, rel, method+" "+path+" requestBody")
							if ref, _ := mediaSchema["$ref"].(string); ref != "" {
								name := strings.TrimPrefix(ref, "#/components/schemas/")
								comp, hasComp := asMap(schemas[name])
								xt, _ := comp["x-service-type"].(string)
								if !hasComp {
									c.rulef("http-components", "%s %s %s: requestBody $ref %s points to a missing component", rel, method, path, name)
									continue
								}
								if xt == "" {
									c.rulef("http-service-type", "%s %s %s: requestBody $ref %s is not a param of %s", rel, method, path, name, serviceRef)
									continue
								}
								if !target.paramTypes[xt] {
									c.rulef("http-service-type", "%s %s %s: requestBody $ref %s is not a param of %s", rel, method, path, name, serviceRef)
								}
								fileRef := serviceRef
								if i := strings.LastIndex(serviceRef, "."); i > 0 {
									fileRef = serviceRef[:i]
								}
								if !strings.HasPrefix(xt, fileRef+".schemas.") {
									c.rulef("http-service-type", "%s %s %s: requestBody $ref %s points at %s, not %s.schemas.*", rel, method, path, name, xt, fileRef)
								}
							}
						}
					}
				}
			}
		}
	}
}

// checkClosedObjects 递归校验 schema 中的 object 均关闭额外字段。
func (c *Checker) checkClosedObjects(v any, rel, where string) {
	m, ok := asMap(v)
	if !ok {
		return
	}
	typ, _ := m["type"].(string)
	if typ == "object" || (typ == "" && m["properties"] != nil) {
		if ap, ok := m["additionalProperties"]; !ok || ap != false {
			c.rulef("http-components", "%s: %s must set additionalProperties: false", rel, where)
		}
	}
	if props, ok := asMap(m["properties"]); ok {
		for name, raw := range props {
			c.checkClosedObjects(raw, rel, where+"."+name)
		}
	}
	if items, ok := asMap(m["items"]); ok {
		c.checkClosedObjects(items, rel, where+".items")
	}
	if ap, ok := asMap(m["additionalProperties"]); ok {
		c.checkClosedObjects(ap, rel, where+".additionalProperties")
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf", "prefixItems"} {
		for i, raw := range asList(m[key]) {
			c.checkClosedObjects(raw, rel, fmt.Sprintf("%s.%s[%d]", where, key, i))
		}
	}
}

func retryAfterHeader(resp map[string]any) bool {
	headers, _ := asMap(resp["headers"])
	ra, _ := asMap(headers["Retry-After"])
	if ra == nil {
		return false
	}
	if ref, _ := ra["$ref"].(string); ref != "" {
		return ref == "#/components/headers/RetryAfter"
	}
	_, ok := asMap(ra["schema"])
	return ok
}

func findTypeSchema(meta map[string]serviceMeta, typ string) map[string]any {
	for _, m := range meta {
		if def := m.typeSchemas[typ]; def != nil {
			return def
		}
	}
	return nil
}

func sameSchemaFields(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, rawA := range a {
		rawB, ok := b[k]
		if !ok {
			return false
		}
		ma, _ := asMap(rawA)
		mb, _ := asMap(rawB)
		ta, _ := ma["type"].(string)
		tb, _ := mb["type"].(string)
		if ta != tb {
			return false
		}
	}
	return true
}

// schemaPropertyMap 把 DSL 的 property 条目列表转成 name → schema 映射，
// 供 HTTP components（标准 OpenAPI map）与 service schema 比较字段。
func schemaPropertyMap(v any) map[string]any {
	if m, ok := asMap(v); ok {
		return m
	}
	out := map[string]any{}
	for _, raw := range asList(v) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		if schema, ok := asMap(item["schema"]); ok {
			out[name] = schema
		}
	}
	return out
}

func serviceTypeRef(ref string) (string, bool) {
	if !strings.HasPrefix(ref, "cap.services.") {
		return "", false
	}
	rest := strings.TrimPrefix(ref, "cap.services.")
	parts := strings.Split(rest, ".schemas.")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return ref, true
}

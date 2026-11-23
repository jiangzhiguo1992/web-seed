// http.go 校验 cap.protocols.http 的 OpenAPI 绑定。
package check

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
)

// allowedOperationExtensions 是 operation 允许的 x-* 扩展键。
var allowedOperationExtensions = map[string]bool{
	"x-contract-id": true, "x-service": true,
	"x-timeout-ms": true, "x-default-errors": true,
	"x-service-params": true, "x-errors": true, "x-idempotency": true,
}

// checkProjectAPIVersion 校验 project.yaml 的 api app 都显式声明 v<数字> 版本。
func (c *Checker) checkProjectAPIVersion() {
	doc, err := loadYAML(filepath.Join(c.root, "contracts/project.yaml"))
	if err != nil {
		c.rulef("schema.load", "contracts/project.yaml: %v", err)
		return
	}
	apps, _ := asMap(doc["apps"])
	for name, raw := range apps {
		app, _ := asMap(raw)
		if kind, _ := app["kind"].(string); kind != "api" {
			continue
		}
		generated, _ := asMap(app["generated"])
		if spec, _ := generated["spec"].(string); spec == "" {
			continue
		}
		version, _ := app["api_version"].(string)
		if version == "" {
			c.rulef("http-version", "contracts/project.yaml: app %s requires api_version", name)
			continue
		}
		if !match(`^v[0-9]+$`, version) {
			c.rulef("http-version", "contracts/project.yaml: app %s api_version %q must match v<digits>", name, version)
		}
		timeout, ok := toInt(app["timeout_ms"])
		if !ok || timeout <= 0 {
			c.rulef("http-version", "contracts/project.yaml: app %s timeout_ms must be a positive integer", name)
		}
	}
}

// checkHTTPShared 校验源契约 shared.securitySchemes 与 shared.responses 的外层结构；
// 具体 OpenAPI 值域仍由生成器和 http-components 处理。
func (c *Checker) checkHTTPShared(doc map[string]any, rel string) {
	shared := asMapValue(doc["shared"])
	if shared == nil {
		return
	}
	schemes := asMapValue(shared["securitySchemes"])
	for name, raw := range schemes {
		scheme := asMapValue(raw)
		if scheme == nil {
			c.rulef("http-shared", "%s: shared.securitySchemes.%s must be an object", rel, name)
			continue
		}
		typ, _ := scheme["type"].(string)
		if typ == "" {
			c.rulef("http-shared", "%s: shared.securitySchemes.%s.type required", rel, name)
			continue
		}
		switch typ {
		case "http":
			if v, _ := scheme["scheme"].(string); strings.TrimSpace(v) == "" {
				c.rulef("http-shared", "%s: shared.securitySchemes.%s.scheme required for http", rel, name)
			}
		case "apiKey":
			if v, _ := scheme["name"].(string); strings.TrimSpace(v) == "" {
				c.rulef("http-shared", "%s: shared.securitySchemes.%s.name required for apiKey", rel, name)
			}
			if v, _ := scheme["in"].(string); v != "header" && v != "query" && v != "cookie" {
				c.rulef("http-shared", "%s: shared.securitySchemes.%s.in must be header/query/cookie", rel, name)
			}
		case "oauth2":
			flows := asMapValue(scheme["flows"])
			if flows == nil {
				c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows required for oauth2", rel, name)
				continue
			}
			for flowName, rawFlow := range flows {
				flow := asMapValue(rawFlow)
				if flow == nil {
					c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s must be an object", rel, name, flowName)
					continue
				}
				switch flowName {
				case "implicit":
					if v, _ := flow["authorizationUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s.authorizationUrl required", rel, name, flowName)
					}
				case "password", "clientCredentials":
					if v, _ := flow["tokenUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s.tokenUrl required", rel, name, flowName)
					}
				case "authorizationCode":
					if v, _ := flow["authorizationUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s.authorizationUrl required", rel, name, flowName)
					}
					if v, _ := flow["tokenUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s.tokenUrl required", rel, name, flowName)
					}
				default:
					c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s is not an oauth2 flow", rel, name, flowName)
				}
				if flow["scopes"] == nil {
					c.rulef("http-shared", "%s: shared.securitySchemes.%s.flows.%s.scopes required", rel, name, flowName)
				}
			}
		case "openIdConnect":
			if v, _ := scheme["openIdConnectUrl"].(string); strings.TrimSpace(v) == "" {
				c.rulef("http-shared", "%s: shared.securitySchemes.%s.openIdConnectUrl required", rel, name)
			}
		case "mutualTLS":
		default:
			c.rulef("http-shared", "%s: shared.securitySchemes.%s.type %q is not supported", rel, name, typ)
		}
	}
	responses := asMapValue(shared["responses"])
	for name, raw := range responses {
		response := asMapValue(raw)
		if response == nil {
			c.rulef("http-shared", "%s: shared.responses.%s must be an object", rel, name)
			continue
		}
		if v, _ := response["description"].(string); strings.TrimSpace(v) == "" {
			c.rulef("http-shared", "%s: shared.responses.%s.description required", rel, name)
		}
		for header, rawHeader := range asMapValue(response["headers"]) {
			switch value := rawHeader.(type) {
			case string:
				if strings.TrimSpace(value) == "" {
					c.rulef("http-shared", "%s: shared.responses.%s.headers.%s must not be empty", rel, name, header)
				}
			case map[string]any:
				if value["schema"] == nil {
					c.rulef("http-shared", "%s: shared.responses.%s.headers.%s.schema required", rel, name, header)
				}
			default:
				c.rulef("http-shared", "%s: shared.responses.%s.headers.%s must be a name or object", rel, name, header)
			}
		}
		for contentType, rawMedia := range asMapValue(response["content"]) {
			media := asMapValue(rawMedia)
			if media == nil || media["schema"] == nil {
				c.rulef("http-shared", "%s: shared.responses.%s.content.%s.schema required", rel, name, contentType)
			}
		}
	}
	for name, raw := range asMapValue(shared["parameters"]) {
		parameter := asMapValue(raw)
		if parameter == nil {
			c.rulef("http-shared", "%s: shared.parameters.%s must be an object", rel, name)
			continue
		}
		if v, _ := parameter["name"].(string); strings.TrimSpace(v) == "" {
			c.rulef("http-shared", "%s: shared.parameters.%s.name required", rel, name)
		}
		in, _ := parameter["in"].(string)
		if in != "path" && in != "query" && in != "header" && in != "cookie" {
			c.rulef("http-shared", "%s: shared.parameters.%s.in must be path/query/header/cookie", rel, name)
		}
		if parameter["schema"] == nil {
			c.rulef("http-shared", "%s: shared.parameters.%s.schema required", rel, name)
		}
		if in == "path" {
			if required, _ := parameter["required"].(bool); !required {
				c.rulef("http-shared", "%s: shared.parameters.%s.required must be true for path", rel, name)
			}
		}
	}
	for name, raw := range asMapValue(shared["headers"]) {
		header := asMapValue(raw)
		if header == nil {
			c.rulef("http-shared", "%s: shared.headers.%s must be an object", rel, name)
			continue
		}
		if header["schema"] == nil {
			c.rulef("http-shared", "%s: shared.headers.%s.schema required", rel, name)
		}
	}
	for name, raw := range asMapValue(shared["schemas"]) {
		schema := asMapValue(raw)
		if schema == nil {
			c.rulef("http-shared", "%s: shared.schemas.%s must be an object", rel, name)
			continue
		}
		source := asMapValue(schema["source"])
		if source == nil {
			c.rulef("http-shared", "%s: shared.schemas.%s.source required", rel, name)
			continue
		}
		count := 0
		for key, value := range source {
			switch key {
			case "service_type", "enum", "entity":
				if v, _ := value.(string); strings.TrimSpace(v) == "" {
					c.rulef("http-shared", "%s: shared.schemas.%s.source.%s must not be empty", rel, name, key)
				}
				count++
			case "platform":
				if v, _ := value.(bool); !v {
					c.rulef("http-shared", "%s: shared.schemas.%s.source.platform must be true", rel, name)
				}
				count++
			default:
				c.rulef("http-shared", "%s: shared.schemas.%s.source.%s is not a source key", rel, name, key)
			}
		}
		if count != 1 {
			c.rulef("http-shared", "%s: shared.schemas.%s.source must declare exactly one source key", rel, name)
		}
		if schema["properties"] != nil {
			if platform, _ := source["platform"].(bool); !platform {
				c.rulef("http-shared", "%s: shared.schemas.%s.properties only allowed for platform schemas", rel, name)
			}
		}
	}
}

func asMapValue(v any) map[string]any {
	m, _ := asMap(v)
	return m
}

func (c *Checker) checkHTTPBindings(schema *Schema, serviceMeta map[string]serviceMeta, typeNames map[string]bool, jobServices map[string]bool) {
	httpFiles := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
	httpSchemas := map[string]bool{}
	httpHeaders := map[string]bool{}
	httpParameters := map[string]bool{}
	httpParameterDefs := map[string]map[string]any{}
	httpSecuritySchemes := map[string]bool{}
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
		if c.strictHTTP {
			c.validateDoc(doc, schema.Fields, rel)
		}
		c.checkHTTPShared(doc, rel)
		if err := openapispec.NormalizeHTTPDocument(doc, c.httpTypeResolver, c.strictHTTP); err != nil {
			c.rulef("http-shape", "%s: %v", rel, err)
			continue
		}
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
		parameters, _ := asMap(components["parameters"])
		for name, raw := range parameters {
			httpParameters[name] = true
			if def, ok := asMap(raw); ok {
				httpParameterDefs[name] = def
			}
		}
		securitySchemes, _ := asMap(components["securitySchemes"])
		for name := range securitySchemes {
			httpSecuritySchemes[name] = true
		}
		responses, _ := asMap(components["responses"])
		for name := range responses {
			httpResponses[name] = true
		}
		if rawPaths, ok := asMap(doc["paths"]); ok && len(rawPaths) > 0 {
			for path := range rawPaths {
				if match(`^/v[0-9]+(/|$)`, path) {
					c.rulef("http-version", "%s: path %s must not carry a version prefix; use app.api_version", rel, path)
				}
			}
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
			platform, _ := s["x-platform"].(bool)
			sources := 0
			if ref, _ := s["x-service-type"].(string); ref != "" {
				sources++
				if !match(`^cap\.services\.[a-z][a-z0-9_]*\.schemas\.[A-Za-z][A-Za-z0-9]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.source.service_type must match cap.services.<file>.schemas.<Type>", rel, name)
				} else if typ, ok := serviceTypeRef(ref); !ok || !typeNames[typ] {
					c.rulef("http-components", "%s: components.schemas.%s.source.service_type references unknown %s", rel, name, ref)
				}
			}
			if ref, _ := s["x-enum"].(string); ref != "" {
				sources++
				if !match(`^data\.enums\.[a-z][a-z0-9_]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.source.enum must match data.enums.<name>", rel, name)
				} else if !c.enumIDs[ref] {
					c.rulef("http-components", "%s: components.schemas.%s.source.enum references unknown enum %s", rel, name, ref)
				}
			}
			if ref, _ := s["x-entity"].(string); ref != "" {
				sources++
				if !match(`^data\.entities\.[a-z][a-z0-9_]*$`, ref) {
					c.rulef("http-components", "%s: components.schemas.%s.source.entity must match data.entities.<name>", rel, name)
				} else if _, ok := c.entities[ref]; !ok {
					c.rulef("http-components", "%s: components.schemas.%s.source.entity references unknown entity %s", rel, name, ref)
				}
			}
			switch {
			case platform && sources > 0:
				c.rulef("http-components", "%s: components.schemas.%s cannot combine source.platform with a type source", rel, name)
			case !platform && sources == 0:
				c.rulef("http-components", "%s: components.schemas.%s must declare exactly one of source.service_type/source.enum/source.entity/source.platform", rel, name)
			case !platform && sources > 1:
				c.rulef("http-components", "%s: components.schemas.%s declares more than one type source", rel, name)
			}
			if !platform {
				if _, ok := s["properties"]; ok {
					c.rulef("http-components", "%s: components.schemas.%s must not define properties; the type source is the single truth", rel, name)
				}
			} else if ap, ok := s["additionalProperties"].(bool); !ok || ap {
				c.rulef("http-components", "%s: components.schemas.%s platform schema must set additionalProperties:false", rel, name)
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
			case strings.HasPrefix(ref, "#/components/parameters/"):
				if name := strings.TrimPrefix(ref, "#/components/parameters/"); !httpParameters[name] {
					c.rulef("http-components", "%s: unknown parameter $ref %s", rel, ref)
				}
			case strings.HasPrefix(ref, "#/components/responses/"):
				if name := strings.TrimPrefix(ref, "#/components/responses/"); !httpResponses[name] {
					c.rulef("http-components", "%s: unknown response $ref %s", rel, ref)
				}
			default:
				c.rulef("http-components", "%s: unsupported $ref %s; use #/components/{schemas,headers,parameters,responses}/<Name>", rel, ref)
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
		if err := openapispec.NormalizeHTTPDocument(doc, c.httpTypeResolver, c.strictHTTP); err != nil {
			continue
		}
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
							if mediaSchema["x-service-return"] != nil {
								c.rulef("http-service-return", "%s %s %s: x-service-return removed; derive the type from the component", rel, method, path)
							}
						}
					}
					c.checkHTTPResponse(rel, method, path, status, resp, op, schemas, serviceMeta)
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
				c.checkHTTPErrors(rel, method, path, op, target, effectiveDefaults, responses)
				c.checkHTTPOperationHeaders(rel, method, path, op, httpSecuritySchemes, serviceRef)
				c.checkHTTPRequest(rel, method, path, op, target, schemas, httpParameters, httpParameterDefs)
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

// httpComponentSource 返回 component 的溯源种类与类型 ID。
func httpComponentSource(comp map[string]any) (string, string) {
	if comp == nil {
		return "", ""
	}
	if v, _ := comp["x-service-type"].(string); v != "" {
		return "service", v
	}
	if v, _ := comp["x-enum"].(string); v != "" {
		return "enum", v
	}
	if v, _ := comp["x-entity"].(string); v != "" {
		return "entity", v
	}
	if v, _ := comp["x-platform"].(bool); v {
		return "platform", ""
	}
	return "", ""
}

// checkHTTPOperationHeaders 校验 security、幂等键与平台请求头声明。
func (c *Checker) checkHTTPOperationHeaders(rel, method, path string, op map[string]any, schemes map[string]bool, serviceRef string) {
	refs := map[string]bool{}
	for _, raw := range asList(op["parameters"]) {
		param, _ := asMap(raw)
		if ref, _ := param["$ref"].(string); ref != "" {
			refs[ref] = true
		}
	}
	securityDeclared := false
	if raw, ok := op["security"]; ok {
		for _, item := range asList(raw) {
			requirement, _ := asMap(item)
			if len(requirement) > 0 {
				securityDeclared = true
			}
			for name := range requirement {
				if !schemes[name] {
					c.rulef("http-security", "%s %s %s: unknown security scheme %s", rel, method, path, name)
				}
			}
		}
	}
	if len(c.accessBindings[serviceRef].permissions) > 0 && !securityDeclared {
		c.rulef("http-security", "%s %s %s: service %s has permissions and requires security", rel, method, path, serviceRef)
	}
	raw, ok := op["x-idempotency"]
	if !ok {
		c.rulef("http-idempotency", "%s %s %s: x-idempotency required (none/optional/required)", rel, method, path)
		return
	}
	mode, _ := raw.(string)
	switch mode {
	case "none", "optional", "required":
	default:
		c.rulef("http-idempotency", "%s %s %s: x-idempotency must be none/optional/required, got %v", rel, method, path, raw)
		return
	}
	hasOptional := refs["#/components/parameters/IdempotencyKey"]
	hasRequired := refs["#/components/parameters/IdempotencyKeyRequired"]
	switch {
	case mode == "none" && (hasOptional || hasRequired):
		c.rulef("http-idempotency", "%s %s %s: x-idempotency=none must not declare Idempotency-Key", rel, method, path)
	case mode == "optional" && !hasOptional:
		c.rulef("http-idempotency", "%s %s %s: x-idempotency=optional requires IdempotencyKey", rel, method, path)
	case mode == "required" && !hasRequired:
		c.rulef("http-idempotency", "%s %s %s: x-idempotency=required requires IdempotencyKeyRequired", rel, method, path)
	case mode != "required" && hasRequired:
		c.rulef("http-idempotency", "%s %s %s: IdempotencyKeyRequired only allowed with x-idempotency=required", rel, method, path)
	}
	if !refs["#/components/parameters/RequestId"] {
		c.rulef("http-headers", "%s %s %s: X-Request-Id request header ($ref RequestId) is required", rel, method, path)
	}
	params, _ := asMap(op["x-service-params"])
	if params != nil {
		if _, ok := asMap(params["cookie"]); ok && !refs["#/components/parameters/CSRFToken"] {
			c.rulef("http-headers", "%s %s %s: cookie-based operation must declare X-CSRF-Token ($ref CSRFToken)", rel, method, path)
		}
	}
}

// checkHTTPResponse 校验 2xx 响应体与 service 业务返回值的一一对应。
func (c *Checker) checkHTTPResponse(rel, method, path, status string, resp map[string]any, op map[string]any, schemas map[string]any, serviceMeta map[string]serviceMeta) {
	if _, isRef := resp["$ref"]; !isRef {
		headers, _ := asMap(resp["headers"])
		if _, ok := headers["X-Request-Id"]; !ok {
			c.rulef("http-headers", "%s %s %s: %s response must declare X-Request-Id header", rel, method, path, status)
		}
	}
	code, err := strconv.Atoi(status)
	if err != nil || code < 200 || code >= 300 {
		return
	}
	serviceRef, _ := op["x-service"].(string)
	target, ok := serviceMeta[serviceRef]
	if !ok {
		return
	}
	content, _ := asMap(resp["content"])
	media, _ := asMap(content["application/json"])
	schema, _ := asMap(media["schema"])
	if code == 204 {
		if len(target.returns) != 0 {
			c.rulef("http-service-return", "%s %s %s: 204 requires service %s without business returns", rel, method, path, serviceRef)
		}
		if schema != nil {
			c.rulef("http-service-return", "%s %s %s: 204 must not declare a response body", rel, method, path)
		}
		return
	}
	if schema == nil {
		c.rulef("http-service-return", "%s %s %s: %s requires an application/json response body", rel, method, path, status)
		return
	}
	ref, _ := schema["$ref"].(string)
	if !strings.HasPrefix(ref, "#/components/schemas/") {
		c.rulef("http-service-return", "%s %s %s: %s response schema must be a $ref to components.schemas", rel, method, path, status)
		return
	}
	name := strings.TrimPrefix(ref, "#/components/schemas/")
	comp, ok := asMap(schemas[name])
	if !ok {
		c.rulef("http-components", "%s %s %s: %s response $ref %s points to a missing component", rel, method, path, status, name)
		return
	}
	kind, typeID := httpComponentSource(comp)
	if kind == "" || kind == "platform" {
		c.rulef("http-service-return", "%s %s %s: %s response component %s must be source.service_type/source.enum/source.entity", rel, method, path, status, name)
		return
	}
	found := false
	for _, rt := range target.returns {
		if rt.typ == typeID {
			found = true
			break
		}
	}
	if !found {
		c.rulef("http-service-return", "%s %s %s: %s response type %s is not a return of %s", rel, method, path, status, typeID, serviceRef)
	}
}

// checkHTTPErrors 校验 operation 的 x-errors 与 service 错误集精确一致。
func (c *Checker) checkHTTPErrors(rel, method, path string, op map[string]any, target serviceMeta, effectiveDefaults map[string]bool, responses map[string]any) {
	raw, ok := op["x-errors"]
	if !ok {
		c.rulef("http-errors", "%s %s %s: x-errors required (may be empty)", rel, method, path)
		return
	}
	got := map[string]bool{}
	for _, item := range asList(raw) {
		code, _ := item.(string)
		if code == "" {
			c.rulef("http-errors", "%s %s %s: x-errors entries must be non-empty platform.errors.* IDs", rel, method, path)
			continue
		}
		got[code] = true
		short := strings.TrimPrefix(code, "platform.errors.")
		if !c.errorCodes()[short] {
			c.rulef("http-errors", "%s %s %s: unknown error %s", rel, method, path, code)
			continue
		}
		if strings.HasPrefix(short, "COMMON_") {
			c.rulef("http-errors", "%s %s %s: %s is a platform error; do not list COMMON_* here", rel, method, path, code)
			continue
		}
		if !target.errors[code] {
			c.rulef("http-errors", "%s %s %s: %s is not declared by service %s", rel, method, path, code, op["x-service"])
		}
		status := c.errorStatus[short]
		if status == 0 {
			continue
		}
		if !effectiveDefaults[fmt.Sprint(status)] {
			if _, ok := responses[fmt.Sprint(status)]; !ok {
				c.rulef("http-errors", "%s %s %s: %s status %d is not in effective responses", rel, method, path, code, status)
			}
		}
	}
	want := map[string]bool{}
	for code := range target.errors {
		short := strings.TrimPrefix(code, "platform.errors.")
		if !strings.HasPrefix(short, "COMMON_") {
			want[code] = true
		}
	}
	for code := range want {
		if !got[code] {
			c.rulef("http-errors", "%s %s %s: missing error %s from x-errors", rel, method, path, code)
		}
	}
	for code := range got {
		if !want[code] {
			c.rulef("http-errors", "%s %s %s: unexpected error %s in x-errors", rel, method, path, code)
		}
	}
}

// platformHeaders 是 HTTP 层平台头，不映射到 service 参数。
var platformHeaders = map[string]bool{
	"X-Request-Id": true, "Idempotency-Key": true, "X-CSRF-Token": true,
}

// checkHTTPRequest 校验请求体与 HTTP 参数都来自 x-service 的 params，且覆盖全部必填参数。
func (c *Checker) checkHTTPRequest(rel, method, path string, op map[string]any, target serviceMeta, schemas map[string]any, httpParameters map[string]bool, httpParameterDefs map[string]map[string]any) {
	params, ok := asMap(op["x-service-params"])
	if !ok {
		c.rulef("http-service-params", "%s %s %s: x-service-params required", rel, method, path)
		return
	}
	for key := range params {
		switch key {
		case "body", "path", "query", "header", "cookie":
		default:
			c.rulef("http-service-params", "%s %s %s: unknown x-service-params key %s", rel, method, path, key)
		}
	}
	rb, hasRB := asMap(op["requestBody"])
	var media map[string]any
	var bodySchema map[string]any
	if hasRB {
		content, _ := asMap(rb["content"])
		media, _ = asMap(content["application/json"])
		bodySchema, _ = asMap(media["schema"])
		c.checkClosedObjects(bodySchema, rel, method+" "+path+" requestBody")
	}
	body, hasBody := params["body"]
	if hasBody && !hasRB {
		c.rulef("http-service-type", "%s %s %s: x-service-params.body declared but requestBody is missing", rel, method, path)
	}
	if !hasBody && hasRB {
		c.rulef("http-service-type", "%s %s %s: requestBody declared but x-service-params.body is missing", rel, method, path)
	}
	covered := map[string]bool{}
	if hasBody {
		switch b := body.(type) {
		case string:
			decl, exists := target.paramDecls[b]
			if !exists {
				c.rulef("http-service-type", "%s %s %s: body maps to unknown service parameter %s", rel, method, path, b)
				break
			}
			covered[b] = true
			ref, _ := bodySchema["$ref"].(string)
			if !strings.HasPrefix(ref, "#/components/schemas/") {
				c.rulef("http-service-type", "%s %s %s: body for %s must be a $ref to its service schema", rel, method, path, b)
				break
			}
			name := strings.TrimPrefix(ref, "#/components/schemas/")
			comp, ok := asMap(schemas[name])
			if !ok {
				c.rulef("http-components", "%s %s %s: requestBody $ref %s points to a missing component", rel, method, path, name)
				break
			}
			kind, typeID := httpComponentSource(comp)
			if kind == "" || kind == "platform" {
				c.rulef("http-service-type", "%s %s %s: requestBody component %s must be source.service_type/source.enum/source.entity", rel, method, path, name)
				break
			}
			if typeID != decl {
				c.rulef("http-service-type", "%s %s %s: requestBody type %s is not the type of %s (%s)", rel, method, path, typeID, b, decl)
			}
		case map[string]any:
			props, _ := asMap(bodySchema["properties"])
			if props == nil {
				c.rulef("http-service-type", "%s %s %s: anonymous body must declare object properties", rel, method, path)
				break
			}
			for field, rawParam := range b {
				paramName, _ := rawParam.(string)
				if paramName == "" {
					c.rulef("http-service-type", "%s %s %s: body field %s must map to a service parameter", rel, method, path, field)
					continue
				}
				decl, exists := target.paramDecls[paramName]
				if !exists {
					c.rulef("http-service-type", "%s %s %s: body field %s maps to unknown service parameter %s", rel, method, path, field, paramName)
					continue
				}
				covered[paramName] = true
				prop, ok := asMap(props[field])
				if !ok {
					c.rulef("http-service-type", "%s %s %s: body field %s is not declared in the request schema", rel, method, path, field)
					continue
				}
				if !familiesCompatible(c.httpSchemaFamily(prop, schemas), c.httpServiceFamily(decl)) {
					c.rulef("http-service-type", "%s %s %s: body field %s type differs from service parameter %s (%s)", rel, method, path, field, paramName, decl)
				}
			}
			for field := range props {
				if _, ok := b[field]; !ok {
					c.rulef("http-service-type", "%s %s %s: request body field %s has no x-service-params.body mapping", rel, method, path, field)
				}
			}
		default:
			c.rulef("http-service-type", "%s %s %s: x-service-params.body must be a parameter name or a field map", rel, method, path)
		}
	}

	httpParams := map[string]map[string]any{}
	for _, raw := range asList(op["parameters"]) {
		param, _ := asMap(raw)
		if param == nil {
			continue
		}
		if ref, _ := param["$ref"].(string); ref != "" {
			name := strings.TrimPrefix(ref, "#/components/parameters/")
			def, ok := httpParameterDefs[name]
			if !ok {
				c.rulef("http-service-params", "%s %s %s: unknown parameter $ref %s", rel, method, path, ref)
				continue
			}
			if !httpParameters[name] {
				c.rulef("http-service-params", "%s %s %s: parameter $ref %s is not declared in components.parameters", rel, method, path, ref)
			}
			param = def
		}
		in, _ := param["in"].(string)
		name, _ := param["name"].(string)
		if in == "header" && platformHeaders[name] {
			continue
		}
		if in == "path" || in == "query" || in == "header" || in == "cookie" {
			httpParams[in+"\x00"+name] = param
		}
		if in == "header" {
			rawMap, _ := asMap(raw)
			if _, ok := rawMap["$ref"]; !ok {
				c.rulef("http-service-params", "%s %s %s: header %s must be a $ref to components.parameters", rel, method, path, name)
			}
		}
	}
	mapped := map[string]bool{}
	for _, location := range []string{"path", "query", "header", "cookie"} {
		rawMap, ok := asMap(params[location])
		if !ok {
			continue
		}
		for httpName, rawServicePath := range rawMap {
			servicePath, _ := rawServicePath.(string)
			if servicePath == "" {
				c.rulef("http-service-params", "%s %s %s: %s.%s must map to a service parameter path", rel, method, path, location, httpName)
				continue
			}
			param, ok := httpParams[location+"\x00"+httpName]
			if !ok {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s is not declared in parameters", rel, method, path, location, httpName)
				continue
			}
			mapped[location+"\x00"+httpName] = true
			covered[strings.Split(servicePath, ".")[0]] = true
			decl, ok := c.serviceParamType(target, servicePath)
			if !ok {
				c.rulef("http-service-params", "%s %s %s: %s.%s maps to unknown service field %s", rel, method, path, location, httpName, servicePath)
				continue
			}
			schema, _ := asMap(param["schema"])
			if schema == nil {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s requires a schema", rel, method, path, location, httpName)
				continue
			}
			if !familiesCompatible(c.httpSchemaFamily(schema, schemas), c.httpServiceFamily(decl)) {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s type differs from service field %s (%s)", rel, method, path, location, httpName, servicePath, decl)
			}
		}
	}
	for key := range httpParams {
		if !mapped[key] {
			c.rulef("http-service-params", "%s %s %s: HTTP parameter %s has no x-service-params mapping", rel, method, path, strings.ReplaceAll(key, "\x00", " "))
		}
	}
	for name := range target.requiredParams {
		if !covered[name] {
			c.rulef("http-service-params", "%s %s %s: required service parameter %s is not covered", rel, method, path, name)
		}
	}
}

// serviceParamType 解析 service 参数路径（param 或 param.field）的静态类型。
func (c *Checker) serviceParamType(target serviceMeta, path string) (string, bool) {
	parts := strings.Split(path, ".")
	base, ok := target.paramDecls[parts[0]]
	if !ok {
		return "", false
	}
	if len(parts) == 1 {
		return base, true
	}
	typ := strings.TrimPrefix(base, "*")
	def := target.typeSchemas[typ]
	if def == nil {
		return "", false
	}
	props := schemaPropertyMap(def["properties"])
	for i, part := range parts[1:] {
		node, ok := asMap(props[part])
		if !ok {
			return "", false
		}
		if i == len(parts[1:])-1 {
			if ref, _ := node["ref"].(string); ref != "" {
				return ref, true
			}
			if t, _ := node["type"].(string); t != "" {
				return t, true
			}
			return "", false
		}
		if t, _ := node["type"].(string); t == "_types.object" {
			props = schemaPropertyMap(node["properties"])
			continue
		}
		return "", false
	}
	return "", false
}

// httpServiceFamily 把契约类型归一为可比较的类型族。
func (c *Checker) httpServiceFamily(t string) string {
	t = strings.TrimPrefix(strings.TrimSpace(t), "*")
	if strings.HasPrefix(t, "_types.list<") {
		return "array"
	}
	t = strings.TrimPrefix(t, "_types.")
	if custom, ok := c.valueTypes.CustomTypes[t]; ok {
		return c.httpServiceFamily(custom.Family)
	}
	switch t {
	case "string", "timestamp", "datetime", "bytes":
		return "string"
	case "integer":
		return "integer"
	case "boolean":
		return "boolean"
	case "float", "decimal", "number":
		return "number"
	case "json":
		return "object"
	}
	switch {
	case strings.HasPrefix(t, "data.entities."):
		return "entity:" + t
	case strings.HasPrefix(t, "data.enums."):
		return "enum:" + t
	case strings.HasPrefix(t, "cap.services.") && strings.Contains(t, ".schemas."):
		return "schema:" + t
	}
	return "object"
}

// httpSchemaFamily 把 OpenAPI schema 归一为与 httpServiceFamily 可比较的类型族。
func (c *Checker) httpSchemaFamily(schema map[string]any, schemas map[string]any) string {
	if schema == nil {
		return "unknown"
	}
	if ref, _ := schema["$ref"].(string); ref != "" {
		name := strings.TrimPrefix(ref, "#/components/schemas/")
		comp, _ := asMap(schemas[name])
		switch kind, typeID := httpComponentSource(comp); kind {
		case "service":
			return "schema:" + typeID
		case "enum":
			return "enum:" + typeID
		case "entity":
			return "entity:" + typeID
		case "platform":
			return "object"
		}
		return "unknown"
	}
	switch t, _ := schema["type"].(string); t {
	case "string":
		return "string"
	case "integer":
		return "integer"
	case "boolean":
		return "boolean"
	case "number":
		return "number"
	case "array":
		return "array"
	case "object":
		return "object"
	}
	return "unknown"
}

func familiesCompatible(httpFamily, serviceFamily string) bool {
	if httpFamily == serviceFamily {
		return true
	}
	return httpFamily == "string" && strings.HasPrefix(serviceFamily, "enum:")
}

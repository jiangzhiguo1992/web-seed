// http.go 校验 cap.protocols.http 的 OpenAPI 绑定。
package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
)

// allowedOperationExtensions 是 operation 允许的 x-* 扩展键。
var allowedOperationExtensions = map[string]bool{
	"x-contract-id": true, "x-service": true,
	"x-timeout-ms":     true,
	"x-service-params": true,
}

// httpOpInfo 是 scenario 校验所需的 operation 摘要。
type httpOpInfo struct {
	Path     string
	Method   string
	Service  string
	Statuses map[int]bool
	Codes    map[string]bool
}

// httpPolicy 是 platform/http_conventions.yaml 中与 HTTP 绑定相关的硬约束。
type httpPolicy = openapispec.HTTPPolicy

// loadHTTPPolicy 读取平台基线错误状态与调用端；解析只在 internal/spec 一处。
func (c *Checker) loadHTTPPolicy() (httpPolicy, error) {
	return openapispec.LoadHTTPPolicy(c.root)
}

// apiApp 是 project.yaml 中参与 OpenAPI 生成的 api 应用。
type apiApp struct {
	Audience string
	Includes map[string]bool
}

// loadAPIApps 读取 project.yaml 的 api app；文件缺失时返回空集合。
func (c *Checker) loadAPIApps() map[string]apiApp {
	doc, err := loadYAML(filepath.Join(c.root, "contracts/project.yaml"))
	if err != nil {
		return nil
	}
	out := map[string]apiApp{}
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
		audience, _ := app["audience"].(string)
		includes := map[string]bool{}
		for _, rawInclude := range asList(app["includes"]) {
			if include, ok := rawInclude.(string); ok {
				includes[include] = true
			}
		}
		out[name] = apiApp{Audience: audience, Includes: includes}
	}
	return out
}

// pageServices 返回 service ID → 引用它的页面集合。
func (c *Checker) pageServices() map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, file := range glob(c.root, "contracts/features/pages/*.yaml") {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		pageID, _ := doc["id"].(string)
		if pageID == "" {
			continue
		}
		add := func(service string) {
			if service == "" {
				return
			}
			if out[service] == nil {
				out[service] = map[string]bool{}
			}
			out[service][pageID] = true
		}
		data, _ := asMap(doc["data"])
		for _, raw := range data {
			item, _ := asMap(raw)
			service, _ := item["capability"].(string)
			add(service)
		}
		actions, _ := asMap(doc["actions"])
		for _, raw := range actions {
			item, _ := asMap(raw)
			service, _ := item["capability"].(string)
			add(service)
		}
	}
	return out
}

// validAudiences 把 policy 的 audience 列表转成集合。
func validAudiences(policy httpPolicy) map[string]bool {
	out := map[string]bool{}
	for _, value := range policy.Audiences {
		out[value] = true
	}
	return out
}

// containsInt 判断状态码是否在列表中。
func containsInt(list []int, value int) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

// checkProjectApps 校验 project.yaml 的 api app 声明了合法的 audience 与 timeout_ms。
// API 版本由每个 HTTP 文件的 mount.api_version 声明。
func (c *Checker) checkProjectApps() {
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
		timeout, ok := toInt(app["timeout_ms"])
		if !ok || timeout <= 0 {
			c.rulef("http-version", "contracts/project.yaml: app %s timeout_ms must be a positive integer", name)
		}
		audience, _ := app["audience"].(string)
		if audience == "" {
			c.rulef("http-audience", "contracts/project.yaml: app %s requires audience", name)
			continue
		}
		if policy, err := c.loadHTTPPolicy(); err == nil && !validAudiences(policy)[audience] {
			c.rulef("http-audience", "contracts/project.yaml: app %s audience %q is not declared in platform.http_conventions.audiences", name, audience)
		}
	}
}

// checkHTTPComponents 校验平台 HTTP 组件的外层结构；
// schemas 在平台侧是普通 OpenAPI 对象，在文件侧必须是单一 source 溯源。

func restoreJSONTypes(v any) {
	switch x := v.(type) {
	case map[string]any:
		if jt, ok := x["json_type"]; ok {
			if _, exists := x["type"]; !exists {
				x["type"] = jt
			}
			delete(x, "json_type")
		}
		for _, child := range x {
			restoreJSONTypes(child)
		}
	case []any:
		for _, child := range x {
			restoreJSONTypes(child)
		}
	}
}

func (c *Checker) checkHTTPComponents(components map[string]any, rel string, isPlatform bool) {
	if components == nil {
		return
	}
	restoreJSONTypes(components)
	sections := []string{"schemas", "responses", "parameters", "examples", "requestBodies", "headers", "securitySchemes", "links", "callbacks", "pathItems"}
	names := map[string]map[string]bool{}
	for _, section := range sections {
		names[section] = map[string]bool{}
		for name := range asMapValue(components[section]) {
			names[section][name] = true
		}
	}
	schemes := asMapValue(components["securitySchemes"])
	for name, raw := range schemes {
		scheme := asMapValue(raw)
		if scheme == nil {
			c.rulef("http-shared", "%s: components.securitySchemes.%s must be an object", rel, name)
			continue
		}
		if _, ref := scheme["$ref"]; ref {
			continue
		}
		typ, _ := scheme["type"].(string)
		if typ == "" {
			c.rulef("http-shared", "%s: components.securitySchemes.%s.type required", rel, name)
			continue
		}
		switch typ {
		case "http":
			if v, _ := scheme["scheme"].(string); strings.TrimSpace(v) == "" {
				c.rulef("http-shared", "%s: components.securitySchemes.%s.scheme required for http", rel, name)
			}
		case "apiKey":
			if v, _ := scheme["name"].(string); strings.TrimSpace(v) == "" {
				c.rulef("http-shared", "%s: components.securitySchemes.%s.name required for apiKey", rel, name)
			}
			if v, _ := scheme["in"].(string); v != "header" && v != "query" {
				c.rulef("http-shared", "%s: components.securitySchemes.%s.in must be header/query", rel, name)
			}
		case "oauth2":
			flows := asMapValue(scheme["flows"])
			if flows == nil {
				c.rulef("http-shared", "%s: components.securitySchemes.%s.flows required for oauth2", rel, name)
				continue
			}
			for flowName, rawFlow := range flows {
				flow := asMapValue(rawFlow)
				if flow == nil {
					c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s must be an object", rel, name, flowName)
					continue
				}
				switch flowName {
				case "implicit":
					if v, _ := flow["authorizationUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s.authorizationUrl required", rel, name, flowName)
					}
				case "password", "clientCredentials":
					if v, _ := flow["tokenUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s.tokenUrl required", rel, name, flowName)
					}
				case "authorizationCode":
					if v, _ := flow["authorizationUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s.authorizationUrl required", rel, name, flowName)
					}
					if v, _ := flow["tokenUrl"].(string); strings.TrimSpace(v) == "" {
						c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s.tokenUrl required", rel, name, flowName)
					}
				default:
					c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s is not an oauth2 flow", rel, name, flowName)
				}
				if flow["scopes"] == nil {
					c.rulef("http-shared", "%s: components.securitySchemes.%s.flows.%s.scopes required", rel, name, flowName)
				}
			}
		case "openIdConnect":
			if v, _ := scheme["openIdConnectUrl"].(string); strings.TrimSpace(v) == "" {
				c.rulef("http-shared", "%s: components.securitySchemes.%s.openIdConnectUrl required", rel, name)
			}
		case "mutualTLS":
		default:
			c.rulef("http-shared", "%s: components.securitySchemes.%s.type %q is not supported", rel, name, typ)
		}
	}
	responses := asMapValue(components["responses"])
	for name, raw := range responses {
		response := asMapValue(raw)
		if response == nil {
			c.rulef("http-shared", "%s: components.responses.%s must be an object", rel, name)
			continue
		}
		if _, ref := response["$ref"]; ref {
			continue
		}
		if v, _ := response["description"].(string); strings.TrimSpace(v) == "" {
			c.rulef("http-shared", "%s: components.responses.%s.description required", rel, name)
		}
		for header, rawHeader := range asMapValue(response["headers"]) {
			switch value := rawHeader.(type) {
			case string:
				if strings.TrimSpace(value) == "" {
					c.rulef("http-shared", "%s: components.responses.%s.headers.%s must not be empty", rel, name, header)
				}
			case map[string]any:
				if _, isRef := value["$ref"]; isRef {
					continue
				}
				if value["schema"] == nil {
					c.rulef("http-shared", "%s: components.responses.%s.headers.%s.schema required", rel, name, header)
				}
			default:
				c.rulef("http-shared", "%s: components.responses.%s.headers.%s must be a name or object", rel, name, header)
			}
		}
		for contentType, rawMedia := range asMapValue(response["content"]) {
			media := asMapValue(rawMedia)
			if media == nil || media["schema"] == nil {
				c.rulef("http-shared", "%s: components.responses.%s.content.%s.schema required", rel, name, contentType)
			}
		}
	}
	for name, raw := range asMapValue(components["parameters"]) {
		parameter := asMapValue(raw)
		if parameter == nil {
			c.rulef("http-shared", "%s: components.parameters.%s must be an object", rel, name)
			continue
		}
		if _, ref := parameter["$ref"]; ref {
			continue
		}
		if v, _ := parameter["name"].(string); strings.TrimSpace(v) == "" {
			c.rulef("http-shared", "%s: components.parameters.%s.name required", rel, name)
		}
		in, _ := parameter["in"].(string)
		if in != "path" && in != "query" && in != "header" {
			c.rulef("http-shared", "%s: components.parameters.%s.in must be path/query/header", rel, name)
		}
		if parameter["schema"] == nil {
			c.rulef("http-shared", "%s: components.parameters.%s.schema required", rel, name)
		}
		if in == "path" {
			if required, _ := parameter["required"].(bool); !required {
				c.rulef("http-shared", "%s: components.parameters.%s.required must be true for path", rel, name)
			}
		}
	}
	for name, raw := range asMapValue(components["headers"]) {
		header := asMapValue(raw)
		if header == nil {
			c.rulef("http-shared", "%s: components.headers.%s must be an object", rel, name)
			continue
		}
		if _, ref := header["$ref"]; ref {
			continue
		}
		if header["schema"] == nil {
			c.rulef("http-shared", "%s: components.headers.%s.schema required", rel, name)
		}
	}
	for name, raw := range asMapValue(components["schemas"]) {
		schema := asMapValue(raw)
		if schema == nil {
			c.rulef("http-shared", "%s: components.schemas.%s must be an object", rel, name)
			continue
		}
		if _, ref := schema["$ref"]; ref {
			continue
		}
		if isPlatform {
			if typ, _ := schema["type"].(string); typ != "object" {
				c.rulef("http-shared", "%s: components.schemas.%s must be type object", rel, name)
			}
			c.checkClosedObjects(schema, rel, "components.schemas."+name)
			continue
		}
		source := asMapValue(schema["source"])
		if source == nil {
			c.rulef("http-shared", "%s: components.schemas.%s.source required", rel, name)
			continue
		}
		count := 0
		for key, value := range source {
			switch key {
			case "service_type", "enum", "entity":
				if v, _ := value.(string); strings.TrimSpace(v) == "" {
					c.rulef("http-shared", "%s: components.schemas.%s.source.%s must not be empty", rel, name, key)
				}
				count++
			case "platform":
				if v, _ := value.(bool); !v {
					c.rulef("http-shared", "%s: components.schemas.%s.source.platform must be true", rel, name)
				}
				count++
			default:
				c.rulef("http-shared", "%s: components.schemas.%s.source.%s is not a source key", rel, name, key)
			}
		}
		if count != 1 {
			c.rulef("http-shared", "%s: components.schemas.%s.source must declare exactly one source key", rel, name)
		}
		if schema["properties"] != nil {
			if platform, _ := source["platform"].(bool); !platform {
				c.rulef("http-shared", "%s: components.schemas.%s.properties only allowed for platform schemas", rel, name)
			}
		}
	}
	for name, raw := range asMapValue(components["examples"]) {
		example := asMapValue(raw)
		if example == nil {
			c.rulef("http-shared", "%s: components.examples.%s must be an object", rel, name)
			continue
		}
		if _, ref := example["$ref"]; ref {
			continue
		}
		_, hasValue := example["value"]
		_, hasExternal := example["externalValue"]
		if hasValue == hasExternal {
			c.rulef("http-shared", "%s: components.examples.%s must declare exactly one of value/externalValue", rel, name)
		}
	}
	for name, raw := range asMapValue(components["requestBodies"]) {
		body := asMapValue(raw)
		if body == nil {
			c.rulef("http-shared", "%s: components.requestBodies.%s must be an object", rel, name)
			continue
		}
		if _, ref := body["$ref"]; ref {
			continue
		}
		content := asMapValue(body["content"])
		if len(content) == 0 {
			c.rulef("http-shared", "%s: components.requestBodies.%s.content required", rel, name)
		}
		for contentType, rawMedia := range content {
			media := asMapValue(rawMedia)
			if media == nil || media["schema"] == nil {
				c.rulef("http-shared", "%s: components.requestBodies.%s.content.%s.schema required", rel, name, contentType)
			}
		}
		if v, ok := body["required"]; ok {
			if _, ok := v.(bool); !ok {
				c.rulef("http-shared", "%s: components.requestBodies.%s.required must be a boolean", rel, name)
			}
		}
	}
	for name, raw := range asMapValue(components["links"]) {
		link := asMapValue(raw)
		if link == nil {
			c.rulef("http-shared", "%s: components.links.%s must be an object", rel, name)
			continue
		}
		if _, ref := link["$ref"]; ref {
			continue
		}
		opRef, _ := link["operationRef"].(string)
		opID, _ := link["operationId"].(string)
		if (opRef == "") == (opID == "") {
			c.rulef("http-shared", "%s: components.links.%s must declare exactly one of operationRef/operationId", rel, name)
		}
	}
	for name, raw := range asMapValue(components["callbacks"]) {
		callback := asMapValue(raw)
		if callback == nil {
			c.rulef("http-shared", "%s: components.callbacks.%s must be an object", rel, name)
			continue
		}
		if _, ref := callback["$ref"]; ref {
			continue
		}
		if len(callback) == 0 {
			c.rulef("http-shared", "%s: components.callbacks.%s must not be empty", rel, name)
		}
		for expr, rawItem := range callback {
			item := asMapValue(rawItem)
			if item == nil {
				c.rulef("http-shared", "%s: components.callbacks.%s.%s must be an object", rel, name, expr)
				continue
			}
			if !hasOperation(item) {
				c.rulef("http-shared", "%s: components.callbacks.%s.%s must contain an operation or $ref", rel, name, expr)
			}
		}
	}
	for name, raw := range asMapValue(components["pathItems"]) {
		item := asMapValue(raw)
		if item == nil {
			c.rulef("http-shared", "%s: components.pathItems.%s must be an object", rel, name)
			continue
		}
		if !hasOperation(item) {
			c.rulef("http-shared", "%s: components.pathItems.%s must contain an operation or $ref", rel, name)
		}
	}
	if isPlatform {
		for section, raw := range components {
			c.checkComponentRefs(rel, "components."+section, raw, names)
		}
	}
}

// hasOperation 判断 Path Item 是否声明了至少一个 HTTP operation 或 $ref。
func hasOperation(item map[string]any) bool {
	if _, ok := item["$ref"]; ok {
		return true
	}
	for _, method := range []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"} {
		if _, ok := item[method]; ok {
			return true
		}
	}
	return false
}

// isDataNode 判断字段值是否为任意 JSON 数据，其中的 $ref 只是数据而非引用。
func isDataNode(key string, child any) bool {
	if strings.HasPrefix(key, "x-") {
		return true
	}
	switch key {
	case "value", "example", "default", "enum", "const":
		return true
	case "examples":
		_, list := child.([]any)
		return list
	}
	return false
}

// checkComponentRefs 递归校验平台组件内的本地 $ref 目标存在。
func (c *Checker) checkComponentRefs(rel, where string, v any, names map[string]map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		if ref, _ := x["$ref"].(string); strings.HasPrefix(ref, "#/components/") {
			parts := strings.SplitN(strings.TrimPrefix(ref, "#/components/"), "/", 2)
			if len(parts) != 2 || names[parts[0]] == nil || !names[parts[0]][parts[1]] {
				c.rulef("http-shared", "%s: %s $ref %s does not resolve", rel, where, ref)
			}
		}
		for key, child := range x {
			if key == "$ref" || isDataNode(key, child) {
				continue
			}
			c.checkComponentRefs(rel, where+"."+key, child, names)
		}
	case []any:
		for i, child := range x {
			c.checkComponentRefs(rel, fmt.Sprintf("%s[%d]", where, i), child, names)
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
	httpResponseDefs := map[string]map[string]any{}
	if c.httpOps == nil {
		c.httpOps = map[string]httpOpInfo{}
	}
	policy, policyErr := c.loadHTTPPolicy()
	if policyErr != nil && c.strictHTTP {
		c.rulef("schema.load", "contracts/platform/http_conventions.yaml: %v", policyErr)
	}
	if policyErr == nil {
		c.checkHTTPBodyTypes(schema, policy.BodyTypes)
	}
	platformComponents := asMapValue(policy.Components)
	if platformComponents != nil {
		c.checkHTTPComponents(platformComponents, "contracts/platform/http_conventions.yaml", true)
		for name := range asMapValue(platformComponents["schemas"]) {
			httpSchemas[name] = true
		}
		for name := range asMapValue(platformComponents["headers"]) {
			httpHeaders[name] = true
		}
		for name, raw := range asMapValue(platformComponents["parameters"]) {
			httpParameters[name] = true
			if def, ok := asMap(raw); ok {
				httpParameterDefs[name] = def
			}
		}
		for name := range asMapValue(platformComponents["securitySchemes"]) {
			httpSecuritySchemes[name] = true
		}
		for name, raw := range asMapValue(platformComponents["responses"]) {
			httpResponses[name] = true
			if def, ok := asMap(raw); ok {
				httpResponseDefs[name] = def
			}
		}
	}
	audiences := validAudiences(policy)
	apiApps := c.loadAPIApps()
	pagesByService := c.pageServices()
	seenAudience := map[string]map[string]bool{}
	httpVersions := map[string]string{}
	declaredHTTP := map[string]bool{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		if c.strictHTTP {
			applyHTTPRequiredDefaults(doc)
			c.validateDoc(doc, schema.Fields, rel)
			c.checkHTTPHeaderKinds(rel, doc)
			c.checkFieldOrder(rel, schema.Fields, orderRules{})
		}
		mount := openapispec.CollectHTTPMount(doc)
		rawPaths, _ := asMap(doc["paths"])
		for path := range rawPaths {
			if match(`^/v[0-9]+(/|$)`, path) {
				c.rulef("http-version", "%s: path %s must not carry a version prefix; use mount.api_version", rel, path)
			}
		}
		if c.strictHTTP {
			c.checkHTTPMount(rel, mount, audiences)
		}
		c.checkTargetFileName(schema, filepath.Base(rel), rel)
		c.checkStability(doc, rel)
		if want := schema.Targets.FileID(filepath.Base(rel)); doc["id"] != want {
			c.rulef("http-file", "%s: id %q must be %q", rel, doc["id"], want)
		}
		if err := openapispec.NormalizeHTTPDocument(doc, c.httpTypeResolver, c.strictHTTP, openapispec.HTTPNormalizeOptions{TransportHeaders: policy.TransportHeaders, BodyTypes: policy.BodyTypes}); err != nil {
			if strings.Contains(err.Error(), "transport-managed") {
				c.rulef("http-headers", "%s: %v", rel, err)
			} else {
				c.rulef("http-shape", "%s: %v", rel, err)
			}
			continue
		}
		fileID, _ := doc["id"].(string)
		httpVersions[fileID] = mount.APIVersion
		if fileID != "" {
			declaredHTTP[fileID] = true
		}
		if seenAudience[fileID] == nil {
			seenAudience[fileID] = map[string]bool{}
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
		for name, raw := range responses {
			httpResponses[name] = true
			if def, ok := asMap(raw); ok {
				httpResponseDefs[name] = def
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
				} else if !c.enumDeclared(ref) {
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
	if c.strictHTTP {
		if !httpHeaders["RetryAfter"] {
			c.rulef("http-retry-after", "components.headers.RetryAfter is required for 429/503 responses")
		}
		for _, status := range append(append(append(append([]int{}, policy.Always...), policy.Secured...), policy.Body...), policy.Deprecated...) {
			name, ok := openapispec.DefaultResponseName(status)
			if !ok {
				c.rulef("http-default-errors", "platform baseline status %d has no shared response mapping", status)
				continue
			}
			if !httpResponses[name] {
				c.rulef("http-default-errors", "platform baseline status %d requires components.responses.%s", status, name)
			}
		}
		for _, status := range []int{429, 503} {
			name, _ := openapispec.DefaultResponseName(status)
			def, ok := httpResponseDefs[name]
			if ok && !retryAfterHeader(def) {
				c.rulef("http-retry-after", "components.responses.%s must declare Retry-After", name)
			}
		}
	}
	contractIDs := map[string]string{}
	operationIDs := map[string]string{}
	operations := map[string]string{}
	exposedServices := map[string]bool{}
	serviceOps := map[string][]string{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", relPath(c.root, file), err)
			continue
		}
		rel := relPath(c.root, file)
		opMeta := openapispec.CollectHTTPOperationMeta(doc)
		fileAudience := openapispec.CollectHTTPMount(doc).Audience
		fileStability, _ := doc["stability"].(string)
		if err := openapispec.NormalizeHTTPDocument(doc, c.httpTypeResolver, c.strictHTTP, openapispec.HTTPNormalizeOptions{TransportHeaders: policy.TransportHeaders, BodyTypes: policy.BodyTypes}); err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		components, _ := asMap(doc["components"])
		schemas, _ := asMap(components["schemas"])
		paths, _ := asMap(doc["paths"])
		for path, raw := range paths {
			p, _ := asMap(raw)
			for method, opRaw := range p {
				op, ok := asMap(opRaw)
				if !ok {
					continue
				}
				c.checkHTTPPathParams(rel, method, path, op)
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
				meta := opMeta[contractID]
				if c.strictHTTP {
					c.checkHTTPAudience(rel, method, path, contractID, fileAudience, audiences)
					for _, audience := range fileAudience {
						if seenAudience[fileID] != nil {
							seenAudience[fileID][audience] = true
						}
					}
				}
				responses, _ := asMap(op["responses"])
				c.checkHTTPStatusCodes(rel, method, path, responses, policy)
				serviceID, _ := op["x-service"].(string)
				if serviceID != "" {
					serviceOps[serviceID] = append(serviceOps[serviceID], method+" "+path)
				}
				info := httpOpInfo{Path: path, Method: method, Service: serviceID, Statuses: map[int]bool{}, Codes: map[string]bool{}}
				for status, rawResp := range responses {
					if n, err := strconv.Atoi(status); err == nil {
						info.Statuses[n] = true
						respMap, _ := asMap(rawResp)
						for _, code := range asList(respMap["x-error-codes"]) {
							if value, ok := code.(string); ok {
								info.Codes[value] = true
							}
						}
					}
				}
				for _, n := range policy.Always {
					info.Statuses[n] = true
				}
				if len(asList(op["security"])) > 0 {
					for _, n := range policy.Secured {
						info.Statuses[n] = true
					}
				}
				if op["requestBody"] != nil {
					for _, n := range policy.Body {
						info.Statuses[n] = true
					}
				}
				if fileStability == "deprecated" {
					for _, n := range policy.Deprecated {
						info.Statuses[n] = true
					}
				}
				if contractID != "" {
					c.httpOps[contractID] = info
				}
				for status, rawResp := range responses {
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
					c.checkHTTPResponse(rel, method, path, status, resp, op, schemas, serviceMeta, policy.BodyTypes)
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
				if !servicePublic(target, "") {
					c.rulef("service-visibility", "%s %s %s: service %s is not public", rel, method, path, serviceRef)
				}
				exposedServices[serviceRef] = true
				if c.strictHTTP {
					c.checkHTTPConsumer(rel, method, path, contractID, serviceRef, meta.UsedBy, pagesByService)
				}
				c.checkHTTPErrors(rel, method, path, op, target, responses, policy, fileStability == "deprecated")
				c.checkHTTPOperationSecurity(rel, method, path, op, httpSecuritySchemes, serviceRef)
				c.checkHTTPHeaders(rel, method, path, op)
				c.checkHTTPRequest(rel, method, path, op, target, schemas, httpParameters, httpParameterDefs, policy.BodyTypes)
			}
		}
	}
	if c.strictHTTP {
		includedHTTP := map[string]bool{}
		for appName, app := range apiApps {
			appVersion := ""
			for include := range app.Includes {
				includedHTTP[include] = true
				version := httpVersions[include]
				switch {
				case version == "":
					c.rulef("http-version", "project.yaml: app %s includes %s without mount.api_version", appName, include)
				case appVersion == "":
					appVersion = version
				case appVersion != version:
					c.rulef("http-version", "project.yaml: app %s includes mixed mount.api_version %s and %s", appName, appVersion, version)
				}
				seen, ok := seenAudience[include]
				if !ok || len(seen) == 0 {
					continue
				}
				if !seen[app.Audience] {
					c.rulef("http-audience", "project.yaml: app %s (audience %s) includes %s but no operation targets that audience", appName, app.Audience, include)
				}
			}
		}
		for id := range declaredHTTP {
			if !includedHTTP[id] {
				c.rulef("http-app-included", "contracts/project.yaml: %s is not included by any api app", id)
			}
		}
		for service, ops := range serviceOps {
			if len(ops) > 1 {
				sort.Strings(ops)
				c.rulef("http-consumer", "service %s is bound by %d operations (%s); split the service or add explicit operation-level consumers", service, len(ops), strings.Join(ops, ", "))
			}
		}
		for service := range exposedServices {
			binding := c.accessBindings[service]
			if len(binding.permissions) == 0 && !binding.audit && !binding.rateLimited {
				c.rulef("access-coverage", "service %s is exposed by http but has no platform.security.access binding", service)
			}
		}
		for service, pages := range pagesByService {
			if len(pages) > 0 && !exposedServices[service] {
				c.rulef("http-consumer", "service %s is referenced by pages but has no http operation", service)
			}
		}
	}
}

// applyHTTPRequiredDefaults 把省略的 required 按 true 补进内存文档。
// HTTP 语义默认必填，源契约不必重复写 required: true；模板仍把该字段声明为必填。
func applyHTTPRequiredDefaults(doc map[string]any) {
	paths, _ := asMap(doc["paths"])
	for _, rawPath := range paths {
		pathItem, _ := asMap(rawPath)
		for method, rawOp := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			op, _ := asMap(rawOp)
			request, _ := asMap(op["request"])
			applyRequiredDefaults(request["headers"])
			applyRequiredDefaults(request["query"])
			if body, ok := asMap(request["body"]); ok {
				setRequiredDefault(body)
				applyRequiredDefaults(body["fields"])
			}
			responses, _ := asMap(op["response"])
			for _, rawResp := range responses {
				resp, _ := asMap(rawResp)
				applyRequiredDefaults(resp["headers"])
			}
		}
	}
}

func applyRequiredDefaults(raw any) {
	for _, item := range asList(raw) {
		if value, ok := asMap(item); ok {
			setRequiredDefault(value)
		}
	}
}

func setRequiredDefault(value map[string]any) {
	if _, ok := value["required"]; !ok {
		value["required"] = true
	}
}

// checkHTTPBodyTypes 校验模板里的 body.kind 枚举与平台的 body_types 映射不漂移。
func (c *Checker) checkHTTPBodyTypes(schema *Schema, bodyTypes map[string]string) {
	request, response := httpBodyKindEnums(schema)
	platform := make([]string, 0, len(bodyTypes))
	for kind := range bodyTypes {
		platform = append(platform, kind)
	}
	sort.Strings(platform)
	if !sameStringSet(request, platform) {
		sort.Strings(request)
		c.rulef("http-body-types", "request.body.kind enum %v must equal platform.http_conventions.body_types keys %v", request, platform)
	}
	for _, kind := range response {
		if _, ok := bodyTypes[kind]; !ok {
			c.rulef("http-body-types", "response.body.kind %s is not declared in platform.http_conventions.body_types", kind)
		}
	}
	if len(response) == 0 {
		c.rulef("http-body-types", "response.body.kind enum must not be empty")
	}
}

// httpBodyKindEnums 读取 _meta 中 request/response body.kind 的 enum。
func httpBodyKindEnums(schema *Schema) (request, response []string) {
	paths, ok := contractspec.FindField(schema.Fields, "paths")
	if !ok || len(paths.ItemFields) == 0 {
		return nil, nil
	}
	op := paths.ItemFields[0].ItemFields
	kindEnum := func(body Field) []string {
		kind, ok := contractspec.FindField(body.Fields, "kind")
		if !ok {
			return nil
		}
		out := make([]string, 0, len(kind.Enum))
		for _, value := range kind.Enum {
			if s, ok := value.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	if requestField, ok := contractspec.FindField(op, "request"); ok {
		if body, ok := contractspec.FindField(requestField.Fields, "body"); ok {
			request = kindEnum(body)
		}
	}
	if responseField, ok := contractspec.FindField(op, "response"); ok {
		if body, ok := contractspec.FindField(responseField.ItemFields, "body"); ok {
			response = kindEnum(body)
		}
	}
	return request, response
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]bool{}
	for _, value := range a {
		seen[value] = true
	}
	for _, value := range b {
		if !seen[value] {
			return false
		}
	}
	return true
}

func (c *Checker) checkHTTPStatusCodes(rel, method, path string, responses map[string]any, policy httpPolicy) {
	if len(policy.SuccessCodes) == 0 && len(policy.ClientErrorCodes) == 0 && len(policy.ServerErrorCodes) == 0 {
		return
	}
	for status := range responses {
		code, err := strconv.Atoi(status)
		if err != nil {
			continue
		}
		var allowed []int
		switch {
		case code >= 200 && code < 300:
			allowed = policy.SuccessCodes
		case code >= 400 && code < 500:
			allowed = policy.ClientErrorCodes
		case code >= 500 && code < 600:
			allowed = policy.ServerErrorCodes
		}
		if allowed == nil || !containsInt(allowed, code) {
			c.rulef("http-status", "%s %s %s: status %d is not declared in platform.http_conventions.status_codes", rel, method, path, code)
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

// singleMediaSchema 返回唯一的媒体类型及其 schema；body 只允许一个媒体类型。
func singleMediaSchema(content map[string]any) (string, map[string]any) {
	if len(content) != 1 {
		return "", nil
	}
	for mediaType, raw := range content {
		media, _ := asMap(raw)
		schema, _ := asMap(media["schema"])
		return mediaType, schema
	}
	return "", nil
}

// bodyKindByMediaType 按 platform.http_conventions.body_types 反查 body.kind。
func bodyKindByMediaType(bodyTypes map[string]string, mediaType string) string {
	for kind, value := range bodyTypes {
		if value == mediaType {
			return kind
		}
	}
	return ""
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

// checkHTTPOperationSecurity 校验 operation 的 security 声明。
func (c *Checker) checkHTTPOperationSecurity(rel, method, path string, op map[string]any, schemes map[string]bool, serviceRef string) {
	securityDeclared := false
	if raw, ok := op["security"]; ok {
		for _, item := range asList(raw) {
			requirement, _ := asMap(item)
			if len(requirement) > 0 {
				securityDeclared = true
			}
			for name, scopes := range requirement {
				if !schemes[name] {
					c.rulef("http-security", "%s %s %s: unknown security scheme %s", rel, method, path, name)
				}
				if len(asList(scopes)) > 0 {
					c.rulef("http-security", "%s %s %s: security scheme %s scopes must be empty", rel, method, path, name)
				}
			}
		}
	}
	if len(c.accessBindings[serviceRef].permissions) > 0 && !securityDeclared {
		c.rulef("http-security", "%s %s %s: service %s has permissions and requires security", rel, method, path, serviceRef)
	}
}

// checkHTTPResponse 校验 2xx 响应体与 service 业务返回值的一一对应。
func (c *Checker) checkHTTPResponse(rel, method, path, status string, resp map[string]any, op map[string]any, schemas map[string]any, serviceMeta map[string]serviceMeta, bodyTypes map[string]string) {
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
	mediaType, schema := singleMediaSchema(content)
	if code == 204 {
		if len(target.returns) != 0 {
			c.rulef("http-service-return", "%s %s %s: 204 requires service %s without business returns", rel, method, path, serviceRef)
		}
		if mediaType != "" || schema != nil {
			c.rulef("http-service-return", "%s %s %s: 204 must not declare a response body", rel, method, path)
		}
		return
	}
	if schema == nil {
		c.rulef("http-service-return", "%s %s %s: %s requires a response body", rel, method, path, status)
		return
	}
	kind := bodyKindByMediaType(bodyTypes, mediaType)
	switch kind {
	case "json":
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
		sourceKind, typeID := httpComponentSource(comp)
		if sourceKind == "" || sourceKind == "platform" {
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
	case "text", "binary":
		family := c.httpSchemaFamily(schema, schemas)
		if family != "string" {
			c.rulef("http-service-return", "%s %s %s: %s %s response must be a string, got %s", rel, method, path, status, kind, family)
			return
		}
		found := false
		for _, rt := range target.returns {
			if familiesCompatible(family, c.httpServiceFamily(rt.typ)) {
				found = true
				break
			}
		}
		if !found {
			c.rulef("http-service-return", "%s %s %s: %s %s response is not a return of %s", rel, method, path, status, kind, serviceRef)
		}
	default:
		if mediaType == "" {
			c.rulef("http-service-return", "%s %s %s: %s response content type is missing", rel, method, path, status)
		} else {
			c.rulef("http-service-return", "%s %s %s: %s response media type %s is not declared in platform.body_types", rel, method, path, status, mediaType)
		}
	}
}

// checkHTTPErrors 校验 response.<status>.errors 与 service 错误集精确一致。
func (c *Checker) checkHTTPErrors(rel, method, path string, op map[string]any, target serviceMeta, responses map[string]any, policy httpPolicy, deprecated bool) {
	got := map[string]bool{}
	for status, rawResp := range responses {
		resp, _ := asMap(rawResp)
		code, err := strconv.Atoi(status)
		if err != nil {
			continue
		}
		codes, has := resp["x-error-codes"]
		if code >= 200 && code < 300 {
			if has {
				c.rulef("http-errors", "%s %s %s: 2xx response %s must not declare errors", rel, method, path, status)
			}
			continue
		}
		if !has {
			continue
		}
		for _, item := range asList(codes) {
			codeID, _ := item.(string)
			if codeID == "" {
				c.rulef("http-errors", "%s %s %s: response %s errors must be non-empty platform.errors.* IDs", rel, method, path, status)
				continue
			}
			short := strings.TrimPrefix(codeID, "platform.errors.")
			if !c.errorCodes()[short] {
				c.rulef("http-errors", "%s %s %s: unknown error %s", rel, method, path, codeID)
				continue
			}
			if strings.HasPrefix(short, "COMMON_") {
				c.rulef("http-errors", "%s %s %s: %s is a platform error; do not list COMMON_*", rel, method, path, codeID)
				continue
			}
			if got[codeID] {
				c.rulef("http-errors", "%s %s %s: duplicate error %s", rel, method, path, codeID)
				continue
			}
			got[codeID] = true
			if !target.errors[codeID] {
				c.rulef("http-errors", "%s %s %s: %s is not declared by service %s", rel, method, path, codeID, op["x-service"])
			}
			if want := c.errorStatus[short]; want != 0 && want != code {
				c.rulef("http-errors", "%s %s %s: %s has status %d but is listed under response %s", rel, method, path, codeID, want, status)
			}
		}
	}
	secured := len(asList(op["security"])) > 0
	for code := range target.errors {
		short := strings.TrimPrefix(code, "platform.errors.")
		status := c.errorStatus[short]
		if status == 0 {
			continue
		}
		hasBody := op["requestBody"] != nil
		covered := containsInt(policy.Always, status) ||
			(secured && containsInt(policy.Secured, status)) ||
			(hasBody && containsInt(policy.Body, status)) ||
			(deprecated && containsInt(policy.Deprecated, status))
		if !covered {
			if _, ok := responses[fmt.Sprint(status)]; !ok {
				c.rulef("http-errors", "%s %s %s: service error %s status %d has no response (baseline or errors)", rel, method, path, code, status)
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
			c.rulef("http-errors", "%s %s %s: missing error %s from responses.<status>.errors", rel, method, path, code)
		}
	}
	for code := range got {
		if !want[code] {
			c.rulef("http-errors", "%s %s %s: unexpected error %s in responses", rel, method, path, code)
		}
	}
}

// checkHTTPAudience 校验 operation 的有效 audience 非空且来自平台注册表。
func (c *Checker) checkHTTPAudience(rel, method, path, contractID string, audience []string, valid map[string]bool) {
	if len(audience) == 0 {
		c.rulef("http-audience", "%s %s %s: operation %s requires audience (file or operation)", rel, method, path, contractID)
		return
	}
	seen := map[string]bool{}
	for _, value := range audience {
		if seen[value] {
			c.rulef("http-audience", "%s %s %s: duplicate audience %s", rel, method, path, value)
			continue
		}
		seen[value] = true
		if !valid[value] {
			c.rulef("http-audience", "%s %s %s: audience %s is not declared in platform.http_conventions.audiences", rel, method, path, value)
		}
	}
}

// checkHTTPConsumer 校验 operation 的页面消费者与 service 的双向引用。
func (c *Checker) checkHTTPConsumer(rel, method, path, contractID, service string, usedBy []string, pagesByService map[string]map[string]bool) {
	expected := pagesByService[service]
	declared := map[string]bool{}
	for _, page := range usedBy {
		if declared[page] {
			c.rulef("http-consumer", "%s %s %s: duplicate used_by %s", rel, method, path, page)
			continue
		}
		declared[page] = true
		if !expected[page] {
			c.rulef("http-consumer", "%s %s %s: used_by %s does not reference service %s", rel, method, path, page, service)
		}
	}
	if len(usedBy) == 0 && len(expected) == 0 {
		return
	}
	for page := range expected {
		if !declared[page] {
			c.rulef("http-consumer", "%s %s %s: missing used_by %s (page references %s)", rel, method, path, page, service)
		}
	}
}

// checkHTTPMount 校验文件级挂载的语义：版本前缀、资源前缀与调用端。
func (c *Checker) checkHTTPMount(rel string, mount openapispec.HTTPMount, audiences map[string]bool) {
	if match(`^/v[0-9]+(/|$)`, mount.Path) {
		c.rulef("http-version", "%s: mount.path %s must not carry a version prefix; use mount.api_version", rel, mount.Path)
	}
	for _, segment := range strings.Split(strings.TrimPrefix(mount.Path, "/"), "/") {
		if audiences[segment] {
			c.rulef("http-mount", "%s: mount.path %s must be the common resource prefix; put audience segment %s in paths, not mount", rel, mount.Path, segment)
			break
		}
	}
	for _, audience := range mount.Audience {
		if !audiences[audience] {
			c.rulef("http-audience", "%s: mount.audience %s is not declared in platform.http_conventions.audiences", rel, audience)
		}
	}
}

// checkHTTPPathParams 校验路径占位符与 request.path 声明一一对应。
func (c *Checker) checkHTTPPathParams(rel, method, path string, op map[string]any) {
	placeholders := map[string]bool{}
	for i := 0; i < len(path); i++ {
		if path[i] != '{' {
			continue
		}
		end := strings.IndexByte(path[i:], '}')
		if end < 0 {
			c.rulef("http-path-params", "%s %s %s: unclosed path placeholder", rel, method, path)
			return
		}
		name := path[i+1 : i+end]
		if !match(`^[a-z][a-z0-9_]*$`, name) {
			c.rulef("http-path-params", "%s %s %s: path placeholder {%s} must match [a-z][a-z0-9_]*", rel, method, path, name)
		}
		placeholders[name] = true
		i += end
	}
	declared := map[string]bool{}
	for _, raw := range asList(op["parameters"]) {
		param, _ := asMap(raw)
		if in, _ := param["in"].(string); in != "path" {
			continue
		}
		name, _ := param["name"].(string)
		declared[name] = true
		if required, ok := param["required"].(bool); !ok || !required {
			c.rulef("http-path-params", "%s %s %s: path parameter %s must be required", rel, method, path, name)
		}
	}
	for name := range declared {
		if !placeholders[name] {
			c.rulef("http-path-params", "%s %s %s: path parameter %s is not present in the path template", rel, method, path, name)
		}
	}
	for name := range placeholders {
		if !declared[name] {
			c.rulef("http-path-params", "%s %s %s: path placeholder {%s} has no request.path declaration", rel, method, path, name)
		}
	}
}

// checkHTTPHeaderKinds 校验源契约 header 恰好声明 auth/value/type 之一。
// 归一化后的形式无法再区分三者来源，因此必须在契约层拦截。
func (c *Checker) checkHTTPHeaderKinds(rel string, doc map[string]any) {
	paths, _ := asMap(doc["paths"])
	for path, rawPath := range paths {
		pathItem, _ := asMap(rawPath)
		for method, rawOp := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			op, _ := asMap(rawOp)
			where := fmt.Sprintf("%s %s %s", rel, strings.ToUpper(method), path)
			request, _ := asMap(op["request"])
			for i, raw := range asList(request["headers"]) {
				c.checkHTTPHeaderKind(where+" request.headers", i, raw, true)
			}
			responses, _ := asMap(op["response"])
			for status, rawResp := range responses {
				resp, _ := asMap(rawResp)
				for i, raw := range asList(resp["headers"]) {
					c.checkHTTPHeaderKind(fmt.Sprintf("%s response.%s.headers", where, status), i, raw, false)
				}
			}
		}
	}
}

func (c *Checker) checkHTTPHeaderKind(where string, index int, raw any, request bool) {
	header, _ := asMap(raw)
	if header == nil {
		return
	}
	name, _ := header["name"].(string)
	kinds := 0
	if request {
		if _, ok := header["auth"]; ok {
			kinds++
		}
	}
	if value, ok := header["value"]; ok {
		kinds++
		if s, _ := value.(string); strings.TrimSpace(s) == "" {
			c.rulef("http-headers", "%s[%d] (%s): value must not be empty", where, index, name)
		}
	}
	if _, ok := header["type"]; ok {
		kinds++
	}
	if kinds != 1 {
		c.rulef("http-headers", "%s[%d] (%s): header must declare exactly one of auth/value/type", where, index, name)
	}
}

// checkHTTPHeaders 校验归一化后的 header 参数与响应头没有重复或误写。
func (c *Checker) checkHTTPHeaders(rel, method, path string, op map[string]any) {
	seen := map[string]bool{}
	for _, raw := range asList(op["parameters"]) {
		param, _ := asMap(raw)
		if param == nil {
			continue
		}
		if in, _ := param["in"].(string); in != "header" {
			continue
		}
		name, _ := param["name"].(string)
		if name == "" {
			c.rulef("http-headers", "%s %s %s: header parameter name required", rel, method, path)
			continue
		}
		if seen[strings.ToLower(name)] {
			c.rulef("http-headers", "%s %s %s: duplicate header %s", rel, method, path, name)
		}
		seen[strings.ToLower(name)] = true
		if strings.EqualFold(name, "Content-Type") || strings.EqualFold(name, "Authorization") {
			c.rulef("http-headers", "%s %s %s: %s must be represented as body media type or security, not a header parameter", rel, method, path, name)
		}
	}
	responses, _ := asMap(op["responses"])
	for status, raw := range responses {
		resp, _ := asMap(raw)
		if resp == nil {
			continue
		}
		for name := range asMapValue(resp["headers"]) {
			if strings.EqualFold(name, "Content-Type") {
				c.rulef("http-headers", "%s %s %s: response %s Content-Type must be represented as body media type", rel, method, path, status)
			}
		}
	}
}

// checkHTTPRequest 校验请求体与 HTTP 参数都来自 x-service 的 params，且覆盖全部必填参数。
func (c *Checker) checkHTTPRequest(rel, method, path string, op map[string]any, target serviceMeta, schemas map[string]any, httpParameters map[string]bool, httpParameterDefs map[string]map[string]any, bodyTypes map[string]string) {
	params, ok := asMap(op["x-service-params"])
	if !ok {
		c.rulef("http-service-params", "%s %s %s: x-service-params required", rel, method, path)
		return
	}
	for key := range params {
		switch key {
		case "body", "form", "path", "query":
		default:
			c.rulef("http-service-params", "%s %s %s: unknown x-service-params key %s", rel, method, path, key)
		}
	}
	bearerAuth := securityDeclares(op, "Bearer")
	rb, hasRB := asMap(op["requestBody"])
	if hasRB && (method == "get" || method == "head") {
		c.rulef("http-shape", "%s %s %s: GET/HEAD must not declare a request body", rel, method, path)
	}
	var bodySchema map[string]any
	bodyKind := ""
	if hasRB {
		content, _ := asMap(rb["content"])
		mediaType, schema := singleMediaSchema(content)
		bodySchema = schema
		bodyKind = bodyKindByMediaType(bodyTypes, mediaType)
		c.checkClosedObjects(bodySchema, rel, method+" "+path+" requestBody")
		family := c.httpSchemaFamily(bodySchema, schemas)
		switch bodyKind {
		case "json", "form", "multipart":
			if family != "object" && !strings.HasPrefix(family, "schema:") && !strings.HasPrefix(family, "entity:") {
				c.rulef("http-service-type", "%s %s %s: %s requestBody must be an object schema, got %s", rel, method, path, bodyKind, family)
			}
		case "text", "binary":
			if family != "string" {
				c.rulef("http-service-type", "%s %s %s: %s requestBody must be a string, got %s", rel, method, path, bodyKind, family)
			}
		default:
			if mediaType == "" {
				c.rulef("http-service-type", "%s %s %s: requestBody content type is missing", rel, method, path)
			} else {
				c.rulef("http-service-type", "%s %s %s: requestBody media type %s is not declared in platform.body_types", rel, method, path, mediaType)
			}
		}
	}
	body, hasBody := params["body"]
	form, hasForm := params["form"]
	if hasBody && hasForm {
		c.rulef("http-service-params", "%s %s %s: x-service-params.body and form are mutually exclusive", rel, method, path)
	}
	if hasBody && !hasRB {
		c.rulef("http-service-type", "%s %s %s: x-service-params.body declared but requestBody is missing", rel, method, path)
	}
	if hasForm && !hasRB {
		c.rulef("http-service-type", "%s %s %s: x-service-params.form declared but requestBody is missing", rel, method, path)
	}
	if !hasBody && !hasForm && hasRB {
		c.rulef("http-service-type", "%s %s %s: requestBody declared but x-service-params.body/form is missing", rel, method, path)
	}
	covered := map[string]bool{}
	if hasBody {
		paramPath, ok := serviceParamPath(body)
		if !ok {
			c.rulef("http-service-type", "%s %s %s: x-service-params.body must be params.<service parameter path>, got %v", rel, method, path, body)
		} else if hasRB {
			decl, exists := target.paramDecls[paramPath]
			if !exists || strings.Contains(paramPath, ".") {
				c.rulef("http-service-type", "%s %s %s: body maps to unknown service parameter %s", rel, method, path, paramPath)
			} else {
				if isAuthInjectedParam(paramPath) {
					c.rulef("http-service-params", "%s %s %s: body must not map to auth-injected service parameter %s", rel, method, path, paramPath)
				}
				covered[paramPath] = true
				if target.requiredParams[paramPath] {
					if required, _ := rb["required"].(bool); !required {
						c.rulef("http-service-params", "%s %s %s: requestBody must be required because service parameter %s is required", rel, method, path, paramPath)
					}
				}
				if bodyKind == "text" || bodyKind == "binary" {
					if !familiesCompatible(c.httpSchemaFamily(bodySchema, schemas), c.httpServiceFamily(decl)) {
						c.rulef("http-service-type", "%s %s %s: %s body type differs from service parameter %s (%s)", rel, method, path, bodyKind, paramPath, decl)
					}
				} else if bodyKind != "" {
					ref, _ := bodySchema["$ref"].(string)
					if !strings.HasPrefix(ref, "#/components/schemas/") {
						c.rulef("http-service-type", "%s %s %s: body for %s must be a $ref to its service schema", rel, method, path, paramPath)
					} else {
						name := strings.TrimPrefix(ref, "#/components/schemas/")
						comp, ok := asMap(schemas[name])
						if !ok {
							c.rulef("http-components", "%s %s %s: requestBody $ref %s points to a missing component", rel, method, path, name)
						} else if kind, typeID := httpComponentSource(comp); kind == "" || kind == "platform" {
							c.rulef("http-service-type", "%s %s %s: requestBody component %s must be source.service_type/source.enum/source.entity", rel, method, path, name)
						} else if typeID != decl {
							c.rulef("http-service-type", "%s %s %s: requestBody type %s is not the type of %s (%s)", rel, method, path, typeID, paramPath, decl)
						}
					}
				}
			}
		}
	}

	if hasForm {
		declared := map[string]map[string]bool{}
		for _, raw := range asList(form) {
			entry, _ := asMap(raw)
			from, _ := entry["from"].(string)
			to, _ := entry["to"].(string)
			if from == "" || to == "" {
				c.rulef("http-service-params", "%s %s %s: form field requires from/to", rel, method, path)
				continue
			}
			servicePath, ok := serviceParamPath(to)
			if !ok {
				c.rulef("http-service-params", "%s %s %s: form field %s must map to params.<service parameter path>, got %v", rel, method, path, from, to)
				continue
			}
			if isAuthInjectedParam(strings.Split(servicePath, ".")[0]) {
				c.rulef("http-service-params", "%s %s %s: form field %s must not map to auth-injected service parameter %s", rel, method, path, from, servicePath)
				continue
			}
			top := strings.Split(servicePath, ".")[0]
			field := strings.Split(strings.TrimPrefix(servicePath, top+"."), ".")[0]
			if field == "" {
				c.rulef("http-service-params", "%s %s %s: form field %s must map to params.%s.<field>", rel, method, path, from, top)
				continue
			}
			if declared[top] == nil {
				declared[top] = map[string]bool{}
			}
			declared[top][field] = true
			covered[top] = true
			decl, ok := c.serviceParamType(target, servicePath)
			if !ok {
				c.rulef("http-service-params", "%s %s %s: form field %s maps to unknown service field params.%s", rel, method, path, from, servicePath)
				continue
			}
			props := schemaPropertyMap(bodySchema["properties"])
			if prop, _ := asMap(props[from]); prop != nil {
				httpFamily := c.httpSchemaFamily(prop, schemas)
				if !familiesCompatible(httpFamily, c.httpServiceFamily(decl)) {
					c.rulef("http-service-params", "%s %s %s: form field %s type differs from service field params.%s (%s)", rel, method, path, from, servicePath, decl)
				} else if httpType, _ := entry["type"].(string); httpType != "" {
					if hb, sb, bad := c.httpIntegerBits(httpType, decl); bad {
						c.rulef("http-service-params", "%s %s %s: form field %s integer bits %d do not match service field params.%s (%s, bits %d)", rel, method, path, from, hb, servicePath, decl, sb)
					}
				}
			}
		}
		for top, fields := range declared {
			decl := target.paramDecls[top]
			def := target.typeSchemas[strings.TrimPrefix(decl, "*")]
			for _, rawProp := range asList(def["properties"]) {
				prop, _ := asMap(rawProp)
				name, _ := prop["name"].(string)
				required, _ := prop["required"].(bool)
				if required && !fields[name] {
					c.rulef("http-service-params", "%s %s %s: form field %s is required by service parameter params.%s", rel, method, path, name, top)
				}
			}
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
		if in == "path" || in == "query" {
			httpParams[in+"\x00"+name] = param
		}
	}
	mapped := map[string]bool{}
	for _, location := range []string{"path", "query"} {
		for _, raw := range asList(params[location]) {
			entry, _ := asMap(raw)
			from, _ := entry["from"].(string)
			to, _ := entry["to"].(string)
			if from == "" {
				c.rulef("http-service-params", "%s %s %s: %s to declaration required", rel, method, path, location)
				continue
			}
			param, ok := httpParams[location+"\x00"+from]
			if !ok {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s is not declared in request.%s", rel, method, path, location, from, location)
				continue
			}
			mapped[location+"\x00"+from] = true
			servicePath, ok := serviceParamPath(to)
			if !ok {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s to must map to params.<service parameter path>, got %v", rel, method, path, location, from, to)
				continue
			}
			if isAuthInjectedParam(strings.Split(servicePath, ".")[0]) {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s must not map to auth-injected service parameter %s", rel, method, path, location, from, servicePath)
				continue
			}
			covered[strings.Split(servicePath, ".")[0]] = true
			decl, ok := c.serviceParamType(target, servicePath)
			if !ok {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s maps to unknown service field params.%s", rel, method, path, location, from, servicePath)
				continue
			}
			schema, _ := asMap(param["schema"])
			if schema == nil {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s requires a schema", rel, method, path, location, from)
				continue
			}
			httpFamily := c.httpSchemaFamily(schema, schemas)
			if strings.HasPrefix(httpFamily, "object") || strings.HasPrefix(httpFamily, "schema:") || strings.HasPrefix(httpFamily, "entity:") {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s must be scalar or enum, got %s", rel, method, path, location, from, httpFamily)
				continue
			}
			if location == "path" && httpFamily == "array" {
				c.rulef("http-service-params", "%s %s %s: path parameter %s must be scalar or enum, got array", rel, method, path, from)
				continue
			}
			if !familiesCompatible(httpFamily, c.httpServiceFamily(decl)) {
				c.rulef("http-service-params", "%s %s %s: %s parameter %s type differs from service field params.%s (%s)", rel, method, path, location, from, servicePath, decl)
			} else if httpType, _ := entry["type"].(string); httpType != "" {
				if hb, sb, bad := c.httpIntegerBits(httpType, decl); bad {
					c.rulef("http-service-params", "%s %s %s: %s parameter %s integer bits %d do not match service field params.%s (%s, bits %d)", rel, method, path, location, from, hb, servicePath, decl, sb)
				}
			}
		}
	}
	for key := range httpParams {
		if !mapped[key] {
			c.rulef("http-service-params", "%s %s %s: HTTP parameter %s has no to declaration", rel, method, path, strings.ReplaceAll(key, "\x00", " "))
		}
	}
	for name := range target.requiredParams {
		if isAuthInjectedParam(name) {
			if !bearerAuth {
				c.rulef("http-service-params", "%s %s %s: auth-injected service parameter %s requires security.Bearer=true", rel, method, path, name)
			}
			continue
		}
		if !covered[name] {
			c.rulef("http-service-params", "%s %s %s: required service parameter %s is not covered", rel, method, path, name)
		}
	}
}

// isAuthInjectedParam 判断 service 参数是否由 Bearer 中间件注入，而不是来自 HTTP 参数。
func isAuthInjectedParam(name string) bool {
	return name == "account_id" || name == "access_token"
}

// securityDeclares 判断 operation.security 是否声明了某个 scheme。
func securityDeclares(op map[string]any, scheme string) bool {
	for _, item := range asList(op["security"]) {
		requirement, _ := asMap(item)
		if _, ok := requirement[scheme]; ok {
			return true
		}
	}
	return false
}

// serviceParamPath 解析 request 参数 to 的 params.<name>[.<field>] 表达式。
func serviceParamPath(raw any) (string, bool) {
	value, _ := raw.(string)
	path := strings.TrimPrefix(value, "params.")
	if path == value || path == "" {
		return "", false
	}
	if !match(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)*$`, path) {
		return "", false
	}
	return path, true
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
		if t, _ := node["type"].(string); t == "_types.schemas.object" {
			props = schemaPropertyMap(node["properties"])
			continue
		}
		return "", false
	}
	return "", false
}

// httpIntegerBits 比较 HTTP 参数类型与 service 声明类型的 integer 位宽；非 integer 或无法解析时不返回 mismatch。
func (c *Checker) httpIntegerBits(httpType, serviceType string) (httpBits, serviceBits int, mismatch bool) {
	if c.httpServiceFamily(httpType) != "integer" || c.httpServiceFamily(serviceType) != "integer" {
		return 0, 0, false
	}
	httpBits, httpOK := c.typeIntegerBits(httpType)
	serviceBits, serviceOK := c.typeIntegerBits(serviceType)
	if !httpOK || !serviceOK {
		return 0, 0, false
	}
	return httpBits, serviceBits, httpBits != serviceBits
}

// httpServiceFamily 把契约类型归一为可比较的类型族。
func (c *Checker) httpServiceFamily(t string) string {
	t = strings.TrimPrefix(strings.TrimSpace(t), "*")
	if strings.HasPrefix(t, "_types.structures.list<") {
		return "array"
	}
	if family, variant, ok := contractspec.SplitBasicsRef(t); ok {
		if family == "timestamp" && variant == "long" {
			return "integer"
		}
		t = family
	}
	if custom, ok := c.valueTypes.CustomTypes[t]; ok {
		family, variant, _ := custom.Basics()
		if family == "timestamp" && variant == "long" {
			return "integer"
		}
		return c.httpServiceFamily(family)
	}
	switch t {
	case "string", "timestamp", "datetime", "bytes", "bit", "xml":
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

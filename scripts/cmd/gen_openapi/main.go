// gen_openapi 合并 contracts/project.yaml 声明的 HTTP 契约为 OpenAPI 3.1 spec。
package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
	"gopkg.in/yaml.v3"
)

type project struct {
	Project struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"project"`
	Apps map[string]struct {
		Kind      string   `yaml:"kind"`
		Audience  string   `yaml:"audience"`
		TimeoutMS int      `yaml:"timeout_ms"`
		Includes  []string `yaml:"includes"`
		Generated struct {
			Spec   string `yaml:"spec"`
			Server string `yaml:"server"`
		} `yaml:"generated"`
	} `yaml:"apps"`
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify generated spec is up to date")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	specs, err := build(abs)
	if err != nil {
		fail(err)
	}
	for _, path := range sortedBytesKeys(specs) {
		out := specs[path]
		target := filepath.Join(abs, "contracts", path)
		if *check {
			got, err := os.ReadFile(target)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "gen-openapi: %s missing; run make gen-openapi\n", target)
				} else {
					fmt.Fprintf(os.Stderr, "gen-openapi: %s: %v\n", target, err)
				}
				os.Exit(1)
			}
			if !bytes.Equal(got, out) {
				fmt.Fprintf(os.Stderr, "gen-openapi: %s is not up to date; run make gen-openapi\n", target)
				os.Exit(1)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(target, out, 0o644); err != nil {
			fail(err)
		}
		fmt.Println("generated", target)
	}
	if *check {
		fmt.Println("openapi: OK")
	}
}

func build(root string) (map[string][]byte, error) {
	pj, err := loadProject(filepath.Join(root, "contracts/project.yaml"))
	if err != nil {
		return nil, err
	}
	httpFiles, err := globYAML(filepath.Join(root, "contracts/capabilities/protocols/http"))
	if err != nil {
		return nil, err
	}
	customTypeIdx, err := loadCustomTypes(root)
	if err != nil {
		return nil, err
	}
	policy, err := loadHTTPPolicy(root)
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	fileAudienceByID := map[string][]string{}
	fileVersionByID := map[string]string{}
	fileStabilityByID := map[string]string{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id != "" {
			mount := openapispec.CollectHTTPMount(doc)
			fileAudienceByID[id] = mount.Audience
			fileVersionByID[id] = mount.APIVersion
			fileStabilityByID[id], _ = doc["stability"].(string)
			if err := openapispec.NormalizeHTTPDocument(doc, httpTypeResolver(customTypeIdx), false, policy.TransportHeaders); err != nil {
				return nil, fmt.Errorf("%s: %w", file, err)
			}
			byID[id] = doc
		}
	}
	bizCodes, err := loadErrorBizCodes(root)
	if err != nil {
		return nil, err
	}
	commonByStatus, err := loadCommonErrors(root)
	if err != nil {
		return nil, err
	}
	enumIdx, err := loadEnums(root)
	if err != nil {
		return nil, err
	}
	typeIdx, err := loadServiceTypes(root, customTypeIdx, enumIdx)
	if err != nil {
		return nil, err
	}
	entityIdx, err := loadEntities(root, enumIdx, customTypeIdx)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	knownTypes := map[string]bool{}
	for name := range typeIdx {
		knownTypes[name] = true
	}
	eventIdx, err := loadEventPayloads(root, customTypeIdx, enumIdx, knownTypes)
	if err != nil {
		return nil, err
	}
	for appName, app := range pj.Apps {
		if app.Kind != "api" || app.Generated.Spec == "" {
			continue
		}
		spec := map[string]any{
			"openapi": "3.1.0",
			"info": map[string]any{
				"title":   pj.Project.Name + " " + appName + " API",
				"version": pj.Project.Version,
			},
			"paths":      map[string]any{},
			"components": map[string]any{},
		}
		paths := spec["paths"].(map[string]any)
		components := spec["components"].(map[string]any)
		if app.Audience == "" {
			return nil, fmt.Errorf("app %s: audience required for api apps", appName)
		}
		if !containsString(policy.Audiences, app.Audience) {
			return nil, fmt.Errorf("app %s: audience %q is not declared in platform.http_conventions.audiences", appName, app.Audience)
		}
		if app.TimeoutMS <= 0 {
			return nil, fmt.Errorf("app %s: timeout_ms must be a positive integer", appName)
		}
		appVersion := ""
		for _, include := range app.Includes {
			version := fileVersionByID[include]
			if version == "" {
				return nil, fmt.Errorf("app %s: include %s must declare mount.api_version", appName, include)
			}
			if appVersion == "" {
				appVersion = version
				continue
			}
			if appVersion != version {
				return nil, fmt.Errorf("app %s: includes mix mount.api_version %s and %s", appName, appVersion, version)
			}
		}
		if appVersion == "" && len(app.Includes) > 0 {
			return nil, fmt.Errorf("app %s: includes no HTTP file with mount.api_version", appName)
		}
		if appVersion != "" {
			spec["servers"] = []any{map[string]any{"url": "/" + appVersion}}
		}
		mergeMap(components, copyDoc(policy.Components))
		deprecatedOps := map[string]bool{}
		for _, include := range app.Includes {
			doc := copyDoc(byID[include])
			if doc == nil {
				return nil, fmt.Errorf("app %s: include %s not found", appName, include)
			}
			filtered := map[string]any{}
			for path, rawItem := range asMap(doc["paths"]) {
				item := asMap(rawItem)
				outItem := map[string]any{}
				for method, rawOp := range item {
					op := asMap(rawOp)
					if op == nil {
						outItem[method] = rawOp
						continue
					}
					if !containsString(fileAudienceByID[include], app.Audience) {
						continue
					}
					op["x-timeout-ms"] = app.TimeoutMS
					delete(op, "x-service-params")
					if fileStabilityByID[include] == "deprecated" {
						deprecatedOps[path+"\x00"+method] = true
					}
					outItem[method] = op
				}
				if len(outItem) > 0 {
					filtered[path] = outItem
				}
			}
			mergeMap(paths, filtered)
			mergeMap(components, asMap(doc["components"]))
		}
		if err := ensureHTTPRefs(components, paths, typeIdx, entityIdx, enumIdx); err != nil {
			return nil, fmt.Errorf("app %s: %w", appName, err)
		}
		expandSchemas(components, typeIdx, enumIdx, entityIdx)
		schemas := asMap(components["schemas"])
		if schemas == nil {
			schemas = map[string]any{}
			components["schemas"] = schemas
		}
		for name, schema := range eventIdx {
			if _, exists := schemas[name]; exists {
				return nil, fmt.Errorf("app %s: event component %s collides with an existing schema", appName, name)
			}
			schemas[name] = schema
		}
		eventRefs := map[string]bool{}
		for _, schema := range eventIdx {
			collectSchemaRefs(schema, eventRefs)
		}
		for ref := range eventRefs {
			if _, exists := schemas[ref]; exists {
				continue
			}
			if def := typeIdx[ref]; def != nil {
				schemas[ref] = def
				continue
			}
			found := false
			for name, def := range entityIdx {
				if pascal(name) == ref {
					schemas[ref] = def
					found = true
					break
				}
			}
			if !found {
				return nil, fmt.Errorf("app %s: event schema references unknown component %s", appName, ref)
			}
		}
		responses := asMap(asMap(components["responses"]))
		for _, path := range sortedKeys(paths) {
			item := asMap(paths[path])
			for method, rawOp := range item {
				op := asMap(rawOp)
				if op == nil {
					continue
				}
				resp := asMap(op["responses"])
				if resp == nil {
					resp = map[string]any{}
					op["responses"] = resp
				}
				effective := append([]int{}, policy.Always...)
				if len(asList(op["security"])) > 0 {
					effective = append(effective, policy.Secured...)
				}
				if op["requestBody"] != nil {
					effective = append(effective, policy.Body...)
				}
				if deprecatedOps[path+"\x00"+method] {
					effective = append(effective, policy.Deprecated...)
				}
				for _, status := range effective {
					key := fmt.Sprint(status)
					if existing, exists := resp[key]; exists {
						mergeBaselineCodes(existing, commonByStatus[status])
						continue
					}
					name, ok := openapispec.DefaultResponseName(status)
					if !ok {
						return nil, fmt.Errorf("app %s: baseline status %d has no response mapping", appName, status)
					}
					if _, ok := responses[name]; !ok {
						return nil, fmt.Errorf("app %s: baseline error %d needs components.responses.%s", appName, status, name)
					}
					injected := map[string]any{"$ref": "#/components/responses/" + name}
					if codes := commonByStatus[status]; len(codes) > 0 {
						injected["x-error-codes"] = codes
					}
					resp[key] = injected
				}
				for status, rawResp := range resp {
					response := asMap(rawResp)
					rawCodes, ok := response["x-error-codes"]
					if !ok {
						continue
					}
					if err := expandErrorCodes(response, rawCodes, bizCodes); err != nil {
						return nil, fmt.Errorf("app %s: %s %s %s: %w", appName, method, path, status, err)
					}
				}
				item[method] = op
			}
			paths[path] = item
		}
		if err := wrapResponseEnvelopes(components, paths); err != nil {
			return nil, fmt.Errorf("app %s: %w", appName, err)
		}
		stripGeneratedExtensions(spec)
		if err := openapispec.Validate(spec); err != nil {
			return nil, fmt.Errorf("app %s: %w", appName, err)
		}
		b, err := marshal(spec)
		if err != nil {
			return nil, err
		}
		if bytes.Contains(b, []byte("_types.")) {
			return nil, fmt.Errorf("%s: generated OpenAPI must not contain source-only _types references", app.Generated.Spec)
		}
		if err := validateOpenAPI(b); err != nil {
			return nil, fmt.Errorf("%s: %w", app.Generated.Spec, err)
		}
		out[app.Generated.Spec] = b
	}
	return out, nil
}

// stripGeneratedExtensions 删除契约内部元数据；生成物只输出标准 OpenAPI 与错误码清单。
func stripGeneratedExtensions(spec map[string]any) {
	var walk func(any)
	walk = func(v any) {
		switch value := v.(type) {
		case map[string]any:
			for key := range value {
				if strings.HasPrefix(key, "x-") && key != "x-error-codes" {
					delete(value, key)
				}
			}
			for _, child := range value {
				walk(child)
			}
		case []any:
			for _, child := range value {
				walk(child)
			}
		}
	}
	walk(spec)
}

func copyValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for key, child := range value {
			out[key] = copyValue(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i, child := range value {
			out[i] = copyValue(child)
		}
		return out
	}
	return v
}

func copyDoc(doc map[string]any) map[string]any {
	if doc == nil {
		return nil
	}
	return copyValue(doc).(map[string]any)
}

type httpPolicy = openapispec.HTTPPolicy

// loadHTTPPolicy 读取平台基线错误状态；解析只在 internal/spec 一处。
func loadHTTPPolicy(root string) (httpPolicy, error) {
	return openapispec.LoadHTTPPolicy(root)
}

// loadCommonErrors 按状态聚合 COMMON_* 错误，供基线响应注入。
func loadCommonErrors(root string) (map[int][]any, error) {
	out := map[int][]any{}
	files, err := globYAML(filepath.Join(root, "contracts/platform/errors"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["errors"]) {
			item := asMap(raw)
			code, _ := item["code"].(string)
			if !strings.HasPrefix(code, "COMMON_") {
				continue
			}
			status, ok := toInt(item["status"])
			biz, okBiz := toInt(item["biz_code"])
			if !ok || !okBiz {
				continue
			}
			out[status] = append(out[status], map[string]any{
				"code":     code,
				"biz_code": biz,
			})
		}
	}
	return out, nil
}

// mergeBaselineCodes 把基线 COMMON 错误并入已有 response 的 x-error-codes。
func mergeBaselineCodes(raw any, codes []any) {
	if len(codes) == 0 {
		return
	}
	response, ok := raw.(map[string]any)
	if !ok {
		return
	}
	existing, ok := response["x-error-codes"].([]any)
	if !ok {
		response["x-error-codes"] = codes
		return
	}
	response["x-error-codes"] = append(append([]any{}, codes...), existing...)
}

// loadErrorBizCodes 建立 platform.errors.* → 整数业务码索引。
func loadErrorBizCodes(root string) (map[string]int, error) {
	out := map[string]int{}
	files, err := globYAML(filepath.Join(root, "contracts/platform/errors"))
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["errors"]) {
			item := asMap(raw)
			code, _ := item["code"].(string)
			biz, ok := toInt(item["biz_code"])
			if code == "" || !ok {
				continue
			}
			out["platform.errors."+code] = biz
		}
	}
	return out, nil
}

// expandErrorCodes 把源契约的错误 ID 列表转成 {code, biz_code} 清单。
func expandErrorCodes(resp map[string]any, raw any, bizCodes map[string]int) error {
	out := []any{}
	for _, item := range asList(raw) {
		if expanded, ok := item.(map[string]any); ok {
			out = append(out, expanded)
			continue
		}
		id, _ := item.(string)
		biz, ok := bizCodes[id]
		if !ok {
			return fmt.Errorf("x-error-codes references unknown %s", id)
		}
		out = append(out, map[string]any{
			"code":     strings.TrimPrefix(id, "platform.errors."),
			"biz_code": biz,
		})
	}
	resp["x-error-codes"] = out
	return nil
}

func validateOpenAPI(b []byte) error {
	doc, err := openapi3.NewLoader().LoadFromData(b)
	if err != nil {
		return err
	}
	return doc.Validate(context.Background())
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}

func loadProject(path string) (*project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p project
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	m, ok := normalizeYAMLKeys(raw).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: top-level map required", path)
	}
	return m, nil
}

func normalizeYAMLKeys(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = normalizeYAMLKeys(child)
		}
		return value
	case map[any]any:
		out := map[string]any{}
		for key, child := range value {
			out[fmt.Sprint(key)] = normalizeYAMLKeys(child)
		}
		return out
	case []any:
		for i, child := range value {
			value[i] = normalizeYAMLKeys(child)
		}
		return value
	}
	return v
}

func globYAML(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if sm := asMap(v); sm != nil {
			if dm := asMap(dst[k]); dm != nil {
				mergeMap(dm, sm)
				dst[k] = dm
				continue
			}
		}
		dst[k] = v
	}
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

func stringList(v any) []string {
	var out []string
	for _, item := range asList(v) {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
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

func sortedBytesKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return unquoteStatusKeys(buf.Bytes()), nil
}

// unquoteStatusKeys 只去掉响应状态码键的引号："200": → 200:；
// 描述文本等字符串内容不受影响。
func unquoteStatusKeys(b []byte) []byte {
	lines := strings.Split(string(b), "\n")
	for i, line := range lines {
		if match := quotedStatusLine.FindStringSubmatch(line); match != nil {
			lines[i] = match[1] + match[2] + ":"
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

var quotedStatusLine = regexp.MustCompile(`^(\s*)"(\d{3})":$`)

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-openapi:", err)
	os.Exit(1)
}

func loadCustomTypes(root string) (map[string]contractspec.CustomValueType, error) {
	t, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts/_meta"))
	if err != nil {
		return nil, err
	}
	doc, err := contractspec.LoadCustomTypes(root, t.TypeFamilies)
	if err != nil {
		return nil, err
	}
	return doc.CustomTypes, nil
}

func loadServiceTypes(root string, customTypes map[string]contractspec.CustomValueType, enums map[string][]any) (map[string]map[string]any, error) {
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts/_meta"))
	if err != nil {
		return nil, err
	}
	kinds := shared.Schemas
	raw := map[string]map[string]any{}
	files, err := globYAML(filepath.Join(root, "contracts/capabilities/services"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for name, r := range asMap(doc["schemas"]) {
			if schema := asMap(r); schema != nil {
				if _, exists := raw[name]; exists {
					return nil, fmt.Errorf("duplicate schema name %s across service files; OpenAPI component names must be unique", name)
				}
				raw[name] = schema
			}
		}
	}
	known := map[string]bool{}
	for name := range raw {
		known[name] = true
	}
	out := map[string]map[string]any{}
	for name, node := range raw {
		schema, err := renderSchemaNode(node, customTypes, enums, known, kinds)
		if err != nil {
			return nil, fmt.Errorf("schema %s: %w", name, err)
		}
		out[name] = schema
	}
	return out, nil
}

// loadEventPayloads 把 cap.events topics 的 payload 投影成 OpenAPI components。
// 组件名：Event<File><TopicName>。
func loadEventPayloads(root string, customTypes map[string]contractspec.CustomValueType, enums map[string][]any, known map[string]bool) (map[string]map[string]any, error) {
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts/_meta"))
	if err != nil {
		return nil, err
	}
	kinds := shared.Schemas
	dir := filepath.Join(root, "contracts/capabilities/events")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return map[string]map[string]any{}, nil
	}
	files, err := globYAML(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		for _, raw := range asList(doc["topics"]) {
			topic := asMap(raw)
			if topic == nil {
				continue
			}
			name, _ := topic["name"].(string)
			publish := asMap(topic["publish"])
			payload := asMap(publish["payload"])
			if name == "" || payload == nil {
				continue
			}
			component := "Event" + pascal(stem) + pascal(lastSegment(name))
			if _, exists := out[component]; exists {
				return nil, fmt.Errorf("duplicate event component %s", component)
			}
			schema, err := renderSchemaNode(payload, customTypes, enums, known, kinds)
			if err != nil {
				return nil, fmt.Errorf("event %s: %w", name, err)
			}
			if summary, _ := topic["summary"].(string); summary != "" {
				schema["description"] = summary
			}
			out[component] = schema
		}
	}
	return out, nil
}

func renderSchemaNode(node map[string]any, customTypes map[string]contractspec.CustomValueType, enums map[string][]any, known map[string]bool, kinds map[string]contractspec.SchemaKind) (map[string]any, error) {
	kind, ok := contractspec.SchemaNodeKind(node, kinds)
	if !ok {
		return nil, fmt.Errorf("schema node must have exactly one of ref or type")
	}
	switch kind {
	case "object":
		propsOut := map[string]any{}
		var required []any
		for _, raw := range asList(node["properties"]) {
			prop := asMap(raw)
			if prop == nil {
				return nil, fmt.Errorf("properties: property must be an object")
			}
			name, _ := prop["name"].(string)
			if name == "" {
				return nil, fmt.Errorf("properties: property name required")
			}
			if _, dup := propsOut[name]; dup {
				return nil, fmt.Errorf("properties.%s: duplicate property", name)
			}
			child, err := renderSchemaNode(asMap(prop["schema"]), customTypes, enums, known, kinds)
			if err != nil {
				return nil, fmt.Errorf("properties.%s: %w", name, err)
			}
			applyPropertyAnnotations(prop, child)
			propsOut[name] = child
			if v, _ := prop["required"].(bool); v {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "additionalProperties": false, "properties": propsOut}
		if len(required) > 0 {
			out["required"] = required
		}
		return out, nil
	case "array":
		item, err := renderSchemaNode(asMap(node["items"]), customTypes, enums, known, kinds)
		if err != nil {
			return nil, fmt.Errorf("items: %w", err)
		}
		out := map[string]any{"type": "array", "items": item}
		if v, ok := toInt(node["min_items"]); ok {
			out["minItems"] = v
		}
		if v, ok := toInt(node["max_items"]); ok {
			out["maxItems"] = v
		}
		return out, nil
	case "map":
		values, err := renderSchemaNode(asMap(node["values"]), customTypes, enums, known, kinds)
		if err != nil {
			return nil, fmt.Errorf("values: %w", err)
		}
		out := map[string]any{"type": "object", "additionalProperties": values}
		if v, ok := toInt(node["min_items"]); ok {
			out["minProperties"] = v
		}
		if v, ok := toInt(node["max_items"]); ok {
			out["maxProperties"] = v
		}
		if patterns := stringList(node["key_patterns"]); len(patterns) == 1 {
			out["propertyNames"] = map[string]any{"pattern": patterns[0]}
		} else if len(patterns) > 1 {
			all := make([]any, 0, len(patterns))
			for _, pattern := range patterns {
				all = append(all, map[string]any{"pattern": pattern})
			}
			out["propertyNames"] = map[string]any{"allOf": all}
		}
		return out, nil
	case "scalar":
		typ, _ := node["type"].(string)
		return scalarSchema(typ, customTypes), nil
	case "ref":
		ref, _ := node["ref"].(string)
		switch {
		case strings.HasPrefix(ref, "data.enums."):
			return map[string]any{"type": "string", "enum": enums[lastSegment(ref)]}, nil
		case strings.HasPrefix(ref, "data.entities."):
			return map[string]any{"$ref": "#/components/schemas/" + pascal(lastSegment(ref))}, nil
		default:
			name := ref
			if strings.HasPrefix(ref, "cap.services.") && strings.Contains(ref, ".schemas.") {
				name = lastSegment(ref)
			}
			if !known[name] {
				return nil, fmt.Errorf("unknown schema ref %s", ref)
			}
			return map[string]any{"$ref": "#/components/schemas/" + name}, nil
		}
	}
	return nil, fmt.Errorf("unknown schema kind")
}

// applyPropertyAnnotations 把槽位注解映射到 OpenAPI property schema。
// 注解只存在于 property 条目，schema 节点没有方向/可空/默认值语义。
func applyPropertyAnnotations(prop, out map[string]any) {
	if v, _ := prop["description"].(string); strings.TrimSpace(v) != "" {
		out["description"] = v
	}
	if v, _ := prop["nullable"].(bool); v {
		out["nullable"] = true
	}
	if v, _ := prop["read_only"].(bool); v {
		out["readOnly"] = true
	}
	if v, _ := prop["write_only"].(bool); v {
		out["writeOnly"] = true
	}
	if v, ok := prop["default"]; ok {
		out["default"] = v
	}
}

// httpTypeResolver 把 _types.<name> 解析成 OpenAPI 标量 schema。
func httpTypeResolver(customTypes map[string]contractspec.CustomValueType) openapispec.TypeResolver {
	return func(name string) (map[string]any, error) {
		typ := strings.TrimPrefix(name, "_types.")
		if typ == "any" {
			return map[string]any{}, nil
		}
		if _, ok := customTypes[typ]; !ok {
			switch typ {
			case "string", "boolean", "integer", "float", "decimal", "datetime", "timestamp", "json", "bytes":
			default:
				return nil, fmt.Errorf("unknown type %s", name)
			}
		}
		return scalarSchema(name, customTypes), nil
	}
}

func scalarSchema(typ string, customTypes map[string]contractspec.CustomValueType) map[string]any {
	typ = strings.TrimPrefix(typ, "_types.")
	family := typ
	var params map[string]any
	if nt, ok := customTypes[typ]; ok {
		family = nt.Family
		params = nt.Params
	}
	out := map[string]any{}
	switch family {
	case "string":
		out["type"] = "string"
	case "integer":
		out["type"] = "integer"
	case "boolean":
		out["type"] = "boolean"
	case "timestamp", "datetime":
		out["type"] = "string"
		out["format"] = "date-time"
	case "float", "decimal":
		out["type"] = "number"
	case "json":
		out["type"] = "object"
	case "bytes":
		out["type"] = "string"
		out["format"] = "byte"
	default:
		out["type"] = "object"
	}
	applyScalarParams(out, params)
	return out
}

func applyScalarParams(out map[string]any, params map[string]any) {
	for k, v := range params {
		switch k {
		case "min_len":
			out["minLength"] = v
		case "max_len":
			out["maxLength"] = v
		case "pattern":
			out["pattern"] = v
		case "min":
			out["minimum"] = v
		case "max":
			out["maximum"] = v
		case "default":
			out["default"] = v
		}
	}
}

func loadEnums(root string) (map[string][]any, error) {
	files, _ := globYAML(filepath.Join(root, "contracts/data/enums"))
	out := map[string][]any{}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["enums"]) {
			item := asMap(raw)
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			var values []any
			for _, rawValue := range asList(item["values"]) {
				vm := asMap(rawValue)
				if v, _ := vm["value"].(string); v != "" {
					values = append(values, v)
				}
			}
			out[name] = values
		}
	}
	return out, nil
}

func loadEntities(root string, enums map[string][]any, customTypes map[string]contractspec.CustomValueType) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	baseFields, err := loadEntityBaseFields(root)
	if err != nil {
		return nil, err
	}
	columns, err := loadTableColumns(root)
	if err != nil {
		return nil, err
	}
	files, err := globYAML(filepath.Join(root, "contracts/data/entities"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" || id == "data.entities.base" {
			continue
		}
		fields := mergeEntityFields(baseFields, asList(doc["fields"]))
		out[lastSegment(id)] = entitySchema(fields, enums, columns, customTypes)
	}
	return out, nil
}

type columnMeta struct {
	typ      string
	nullable bool
}

func loadEntityBaseFields(root string) ([]any, error) {
	doc, err := loadYAML(filepath.Join(root, "contracts/data/entities/_base.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return asList(doc["fields"]), nil
}

func loadTableColumns(root string) (map[string]columnMeta, error) {
	out := map[string]columnMeta{}
	files, err := globYAML(filepath.Join(root, "contracts/data/tables"))
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		for _, raw := range asList(doc["columns"]) {
			column := asMap(raw)
			name, _ := column["name"].(string)
			typ, _ := column["type"].(string)
			nullable, _ := column["nullable"].(bool)
			if name != "" && typ != "" {
				out[id+".col."+name] = columnMeta{typ: typ, nullable: nullable}
			}
		}
	}
	return out, nil
}

func mergeEntityFields(base, own []any) []any {
	ownNames := map[string]bool{}
	for _, raw := range own {
		field := asMap(raw)
		if name, _ := field["name"].(string); name != "" {
			ownNames[name] = true
		}
	}
	out := make([]any, 0, len(base)+len(own))
	for _, raw := range base {
		field := asMap(raw)
		if name, _ := field["name"].(string); name != "" && !ownNames[name] {
			out = append(out, raw)
		}
	}
	return append(out, own...)
}

// collectSchemaRefs 收集 event schema 内的 component 引用，补齐事件依赖。
func collectSchemaRefs(schema map[string]any, refs map[string]bool) {
	if ref, _ := schema["$ref"].(string); ref != "" {
		refs[strings.TrimPrefix(ref, "#/components/schemas/")] = true
	}
	if props := asMap(schema["properties"]); props != nil {
		for _, raw := range props {
			if child := asMap(raw); child != nil {
				collectSchemaRefs(child, refs)
			}
		}
	}
	if items := asMap(schema["items"]); items != nil {
		collectSchemaRefs(items, refs)
	}
	if ap := asMap(schema["additionalProperties"]); ap != nil {
		collectSchemaRefs(ap, refs)
	}
	for _, key := range []string{"allOf", "oneOf", "anyOf"} {
		for _, raw := range asList(schema[key]) {
			if child := asMap(raw); child != nil {
				collectSchemaRefs(child, refs)
			}
		}
	}
}

func wrapResponseEnvelopes(components, paths map[string]any) error {
	schemas := asMap(components["schemas"])
	if schemas == nil {
		schemas = map[string]any{}
		components["schemas"] = schemas
	}
	for _, path := range sortedKeys(paths) {
		item := asMap(paths[path])
		for _, rawOp := range item {
			op := asMap(rawOp)
			if op == nil {
				continue
			}
			responses := asMap(op["responses"])
			if responses == nil {
				continue
			}
			for status, rawResp := range responses {
				resp := asMap(rawResp)
				if resp == nil {
					continue
				}
				code, ok := toInt(status)
				if !ok || code < 200 || code >= 300 || code == 204 {
					continue
				}
				content := asMap(resp["content"])
				if content == nil {
					continue
				}
				media := asMap(content["application/json"])
				if media == nil {
					continue
				}
				schema := asMap(media["schema"])
				if schema == nil {
					continue
				}
				ref, _ := schema["$ref"].(string)
				if !strings.HasPrefix(ref, "#/components/schemas/") {
					continue
				}
				dataName := strings.TrimPrefix(ref, "#/components/schemas/")
				envelopeName := "Api" + dataName + "Response"
				if _, exists := schemas[envelopeName]; !exists {
					schemas[envelopeName] = map[string]any{
						"type":                 "object",
						"additionalProperties": false,
						"required":             []any{"code", "msg", "data"},
						"properties": map[string]any{
							"code": map[string]any{"type": "integer", "const": 0},
							"msg":  map[string]any{"type": "string"},
							"data": map[string]any{"$ref": "#/components/schemas/" + dataName},
						},
					}
				}
				media["schema"] = map[string]any{"$ref": "#/components/schemas/" + envelopeName}
			}
		}
	}
	return nil
}

// ensureHTTPRefs 递归补齐 operation/schema 引用到的 service schema、entity、enum 组件。
func ensureHTTPRefs(components, paths map[string]any, typeIdx, entityIdx map[string]map[string]any, enumIdx map[string][]any) error {
	schemas := asMap(components["schemas"])
	if schemas == nil {
		schemas = map[string]any{}
		components["schemas"] = schemas
	}
	for {
		refs := map[string]bool{}
		collectDocSchemaRefs(paths, refs)
		for _, raw := range schemas {
			if schema := asMap(raw); schema != nil {
				collectSchemaRefs(schema, refs)
				if xt, _ := schema["x-service-type"].(string); xt != "" {
					if def := typeIdx[lastSegment(xt)]; def != nil {
						collectSchemaRefs(def, refs)
					}
				}
			}
		}
		added := false
		for ref := range refs {
			if schemas[ref] != nil {
				continue
			}
			if def := typeIdx[ref]; def != nil {
				schemas[ref] = def
				added = true
				continue
			}
			found := false
			for name, def := range entityIdx {
				if pascal(name) == ref {
					schemas[ref] = def
					added = true
					found = true
					break
				}
			}
			if found {
				continue
			}
			for name, values := range enumIdx {
				if pascal(name) == ref {
					schemas[ref] = map[string]any{"type": "string", "enum": values}
					added = true
					break
				}
			}
		}
		if !added {
			return nil
		}
	}
}

func collectDocSchemaRefs(v any, refs map[string]bool) {
	switch value := v.(type) {
	case map[string]any:
		if ref, _ := value["$ref"].(string); strings.HasPrefix(ref, "#/components/schemas/") {
			refs[strings.TrimPrefix(ref, "#/components/schemas/")] = true
		}
		for _, child := range value {
			collectDocSchemaRefs(child, refs)
		}
	case []any:
		for _, child := range value {
			collectDocSchemaRefs(child, refs)
		}
	}
}

func expandSchemas(components map[string]any, types map[string]map[string]any, enums map[string][]any, entities map[string]map[string]any) {
	schemas := asMap(components["schemas"])
	known := map[string]bool{}
	for name := range schemas {
		known[name] = true
	}
	for _, schema := range schemas {
		if s := asMap(schema); s != nil {
			expandSchema(s, types, enums, entities, known, map[string]bool{})
		}
	}
}

func expandSchema(schema map[string]any, types map[string]map[string]any, enums map[string][]any, entities map[string]map[string]any, known map[string]bool, seen map[string]bool) {
	if ref, _ := schema["x-service-type"].(string); ref != "" {
		name := lastSegment(ref)
		if def := types[name]; def != nil && !seen[name] {
			seen[name] = true
			for k, v := range def {
				schema[k] = v
			}
			expandSchema(schema, types, enums, entities, known, seen)
		}
	}
	if ref, _ := schema["x-enum"].(string); ref != "" {
		schema["type"] = "string"
		schema["enum"] = enums[lastSegment(ref)]
	}
	if ref, _ := schema["x-entity"].(string); ref != "" {
		if def := entities[lastSegment(ref)]; def != nil {
			for k, v := range def {
				schema[k] = v
			}
		}
	}
	if typ, _ := schema["type"].(string); strings.HasPrefix(typ, "data.enums.") {
		schema["type"] = "string"
		schema["enum"] = enums[lastSegment(typ)]
	}
	if typ, _ := schema["type"].(string); strings.HasPrefix(typ, "data.entities.") {
		delete(schema, "type")
		schema["$ref"] = "#/components/schemas/" + pascal(lastSegment(typ))
	}
	if typ, _ := schema["type"].(string); known[typ] {
		delete(schema, "type")
		schema["$ref"] = "#/components/schemas/" + typ
	}
	if items := asMap(schema["items"]); items != nil {
		expandSchema(items, types, enums, entities, known, seen)
	}
	if props := asMap(schema["properties"]); props != nil {
		for _, raw := range props {
			if child := asMap(raw); child != nil {
				expandSchema(child, types, enums, entities, known, seen)
			}
		}
	}
}

func entitySchema(fields []any, enums map[string][]any, columns map[string]columnMeta, customTypes map[string]contractspec.CustomValueType) map[string]any {
	props := map[string]any{}
	var required []any
	for _, raw := range fields {
		f := asMap(raw)
		if f == nil {
			continue
		}
		if sensitivity, _ := f["sensitivity"].(string); sensitivity == "secret" {
			continue
		}
		source := asMap(f["source"])
		if asMap(source["join"]) != nil {
			continue
		}
		name, _ := f["name"].(string)
		jsonName, _ := f["json_name"].(string)
		if jsonName == "" {
			jsonName = contractspec.SnakeCase(name)
		}
		if jsonName == "" {
			continue
		}
		typ, _ := f["type"].(string)
		nullable, hasNullable := f["nullable"].(bool)
		if typ == "" {
			if ref, _ := source["column"].(string); ref != "" {
				if column, ok := columns[ref]; ok {
					typ = column.typ
					if !hasNullable {
						nullable = column.nullable
					}
				}
			}
		}
		if typ == "" {
			continue
		}
		sch := typeSchema(typ, enums, customTypes)
		if nullable {
			sch["nullable"] = true
		} else {
			required = append(required, jsonName)
		}
		if desc, _ := f["description"].(string); desc != "" {
			sch["description"] = desc
		}
		props[jsonName] = sch
	}
	out := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func typeSchema(typ string, enums map[string][]any, customTypes map[string]contractspec.CustomValueType) map[string]any {
	typ = strings.TrimPrefix(strings.TrimSpace(typ), "*")
	if strings.HasPrefix(typ, "_types.list<") && strings.HasSuffix(typ, ">") {
		inner := strings.TrimSuffix(strings.TrimPrefix(typ, "_types.list<"), ">")
		return map[string]any{"type": "array", "items": typeSchema(inner, enums, customTypes)}
	}
	if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
		return map[string]any{"type": "array", "items": typeSchema(strings.TrimSuffix(strings.TrimPrefix(typ, "List<"), ">"), enums, customTypes)}
	}
	typ = strings.TrimPrefix(typ, "_types.")
	if _, ok := customTypes[typ]; ok {
		return scalarSchema(typ, customTypes)
	}
	switch typ {
	case "string":
		return map[string]any{"type": "string"}
	case "integer":
		return map[string]any{"type": "integer"}
	case "boolean":
		return map[string]any{"type": "boolean"}
	case "timestamp", "datetime":
		return map[string]any{"type": "string", "format": "date-time"}
	case "number", "float", "decimal":
		return map[string]any{"type": "number"}
	case "json":
		return map[string]any{"type": "object"}
	}
	if strings.HasPrefix(typ, "data.enums.") {
		return map[string]any{"type": "string", "enum": enums[lastSegment(typ)]}
	}
	if strings.HasPrefix(typ, "data.entities.") {
		return map[string]any{"$ref": "#/components/schemas/" + pascal(lastSegment(typ))}
	}
	return map[string]any{"type": "object"}
}

func lastSegment(ref string) string {
	if i := strings.LastIndex(ref, "."); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func pascal(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		if r == '_' || r == '-' || r == '.' {
			up = true
			continue
		}
		if up {
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

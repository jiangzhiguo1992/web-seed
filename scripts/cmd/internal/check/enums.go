// enums.go 校验 data.enums 集合结构、名称/类型名唯一性、code 唯一性、used_by 与全仓枚举引用。
package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
)

func (c *Checker) checkEnums(schema *Schema) map[string]map[string]bool {
	c.checkEnumSchema(schema)
	c.enumCodes = map[string]map[int]bool{}
	files := c.enumFiles(schema)
	if len(files) == 0 {
		c.rulef("schema.targets", "%s: no contract files match %s/%s", schema.ID, schema.Targets.Dir, schema.Targets.Glob)
		return map[string]map[string]bool{}
	}
	usages := c.enumUsages()
	enums := map[string]map[string]bool{}
	names := map[string]string{}
	goTypes := map[string]string{}
	tsTypes := map[string]string{}
	for _, path := range files {
		rel := relPath(c.root, path)
		doc, err := loadYAML(path)
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{top: "enum-key-order", nested: "enum-key-order"})
		c.checkTargetFileName(schema, filepath.Base(path), rel)
		c.checkStability(doc, rel)
		stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		owner, _ := doc["owner_module"].(string)
		if owner != "" && owner != stem {
			c.rulef("enum-owner-module", "%s: owner_module %s must equal file stem %s", rel, owner, stem)
		}
		fileID := schema.Targets.FileID(filepath.Base(path))
		if id, ok := doc["id"].(string); ok && id != fileID {
			c.rulef("enum-id", "%s: id must be %s", rel, fileID)
		}
		rawEnums := asList(doc["enums"])
		if _, present := doc["enums"]; present && len(rawEnums) == 0 {
			c.rulef("schema.non-empty", "%s: enums must not be empty", rel)
		}
		fileNames := map[string]bool{}
		fileGoTypes := map[string]string{}
		fileTSTypes := map[string]string{}
		for i, raw := range rawEnums {
			m, ok := asMap(raw)
			if !ok {
				c.rulef("schema.type", "%s: enums[%d] must be an object", rel, i)
				continue
			}
			name, _ := m["name"].(string)
			id := enumIDPrefix + name
			if prev, ok := names[name]; ok {
				c.rulef("schema.unique-names", "%s: enums[%d]: duplicate name %s (also %s)", rel, i, name, prev)
			}
			names[name] = rel
			fileNames[name] = true
			if typ, _ := m["go_type"].(string); typ != "" {
				fileGoTypes[typ] = name
			}
			if typ, _ := m["ts_type"].(string); typ != "" {
				fileTSTypes[typ] = name
			}
			c.checkEnumTypeName(goTypes, "go_type", i, name, m, rel)
			c.checkEnumTypeName(tsTypes, "ts_type", i, name, m, rel)
			values := map[string]bool{}
			codes := map[int]bool{}
			rawValues := asList(m["values"])
			if _, present := m["values"]; present && len(rawValues) == 0 {
				c.rulef("schema.non-empty", "%s: enums[%d] (%s): values must not be empty", rel, i, name)
			}
			for j, rv := range rawValues {
				vm, ok := asMap(rv)
				if !ok {
					c.rulef("schema.type", "%s: enums[%d].values[%d] must be an object", rel, i, j)
					continue
				}
				v, _ := vm["value"].(string)
				if values[v] {
					c.rulef("enum-values-unique", "%s: enums[%d] (%s): duplicate value %s", rel, i, name, v)
				}
				values[v] = true
				if n, ok := toInt(vm["code"]); ok {
					if codes[n] {
						c.rulef("enum-code-unique", "%s: enums[%d] (%s): duplicate code %d", rel, i, name, n)
					}
					codes[n] = true
				}
			}
			if _, exists := enums[id]; exists {
				c.rulef("schema.unique-names", "%s: enums[%d]: duplicate enum id %s", rel, i, id)
			}
			enums[id] = values
			c.enumCodes[id] = codes
			c.checkEnumUsedBy(rel, i, id, m, usages[id])
		}
		c.checkEnumDoNot(doc, rel, fileID, fileNames, fileGoTypes, fileTSTypes)
	}
	return enums
}

// enumIDPrefix 是枚举项 ID 的固定前缀；文件 ID 是 data.enums.<file>，与枚举项 ID 不同。
const enumIDPrefix = "data.enums."

// enumFiles 返回 targets.dir/glob 下的枚举文件。
func (c *Checker) enumFiles(schema *Schema) []string {
	if schema.Targets.Dir == "" || schema.Targets.Glob == "" {
		return nil
	}
	return glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
}

// checkEnumDoNot 要求 do_not 命中集合 ID、枚举 name 或 go_type/ts_type，避免通用规则堆在实例。
func (c *Checker) checkEnumDoNot(doc map[string]any, rel, setID string, enumNames map[string]bool, goTypes, tsTypes map[string]string) {
	tokens := map[string]bool{setID: true}
	for name := range enumNames {
		if name != "" {
			tokens[name] = true
		}
	}
	for name := range goTypes {
		tokens[name] = true
	}
	for name := range tsTypes {
		tokens[name] = true
	}
	for _, v := range doNotViolations(asList(doc["do_not"]), tokens) {
		if v.Empty {
			c.rulef("enum-do-not-specific", "%s: do_not[%d] must not be empty", rel, v.Index)
			continue
		}
		c.rulef("enum-do-not-specific", "%s: do_not[%d] must mention %s, an enum name, or go_type/ts_type: %s", rel, v.Index, setID, v.Text)
	}
}

// checkEnumSchema 防止 meta 字段树被误删；硬校验依赖这些 required 字段。
func (c *Checker) checkEnumSchema(schema *Schema) {
	c.checkTargets(schema)
	required := []string{"schema_version", "id", "summary", "stability"}
	if schema.Targets.Dir != "" {
		required = append(required, "owner_module")
	}
	for _, name := range required {
		f, ok := contractspec.FindField(schema.Fields, name)
		if !ok || !f.Required {
			c.rulef("schema.meta", "%s: %s must be required", schema.ID, name)
		}
	}
	enums, ok := contractspec.FindField(schema.Fields, "enums")
	if !ok {
		c.rulef("schema.meta", "%s: fields.enums is missing", schema.ID)
		return
	}
	if !enums.Required {
		c.rulef("schema.meta", "%s: enums must be required", schema.ID)
	}
	for _, name := range []string{"name", "go_type", "ts_type", "used_by", "values"} {
		f, ok := contractspec.FindField(enums.ItemFields, name)
		if !ok || !f.Required {
			c.rulef("schema.meta", "%s: enums.%s must be required", schema.ID, name)
		}
	}
	if usedBy, ok := contractspec.FindField(enums.ItemFields, "used_by"); ok {
		if normalizeTypeExpr(usedBy.Type) != "set<string>" || (usedBy.MinLen == nil && usedBy.MinItems == nil) {
			c.rulef("schema.meta", "%s: enums.used_by must be set<string> with min_items", schema.ID)
		}
	}
	values, ok := contractspec.FindField(enums.ItemFields, "values")
	if !ok {
		return
	}
	for _, name := range []string{"value", "code"} {
		f, ok := contractspec.FindField(values.ItemFields, name)
		if !ok || !f.Required {
			c.rulef("schema.meta", "%s: values.%s must be required", schema.ID, name)
		}
	}
}

// checkEnumTypeName 保证生成类型名在集合内唯一；重名会让 Go/TS 产物互相覆盖。
func (c *Checker) checkEnumTypeName(seen map[string]string, field string, i int, name string, m map[string]any, rel string) {
	typeName, _ := m[field].(string)
	if typeName == "" {
		return
	}
	if prev, ok := seen[typeName]; ok {
		c.rulef("enum-type-unique", "%s: enums[%d] (%s): duplicate %s %s (also %s)", rel, i, name, field, typeName, prev)
		return
	}
	seen[typeName] = name
}

// enumUsage 记录一个枚举被哪些 entity / capability 使用。
type enumUsage struct {
	entities     map[string]bool
	capabilities map[string]bool
}

// enumUsages 扫描已解析的 entity 字段和 capability 中的 data.enums.* 引用。
// capability 侧记录具体使用点：service 项 / event topic 或 job / 其他契约文件。
func (c *Checker) enumUsages() map[string]*enumUsage {
	usages := map[string]*enumUsage{}
	for _, e := range c.entities {
		for _, f := range e.Fields {
			if enumID, ok := enumTypeRef(f.Type); ok {
				enumUsageFor(usages, enumID).entities[e.ID] = true
			}
		}
	}
	type capDoc struct {
		id  string
		doc map[string]any
	}
	var docs []capDoc
	serviceSchemas := map[string]map[string]any{}
	for _, f := range walkYAML(c.root, "contracts/capabilities") {
		doc, err := loadYAML(f)
		if err != nil {
			continue
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		docs = append(docs, capDoc{id: id, doc: doc})
		if strings.HasPrefix(id, "cap.services.") {
			schemas, _ := asMap(doc["schemas"])
			for name, raw := range schemas {
				if def, ok := asMap(raw); ok {
					serviceSchemas[id+".schemas."+name] = def
				}
			}
		}
	}
	add := func(enumID, owner string) {
		if owner != "" {
			enumUsageFor(usages, enumID).capabilities[owner] = true
		}
	}
	for _, d := range docs {
		switch {
		case strings.HasPrefix(d.id, "cap.services."):
			for _, rawService := range asList(d.doc["services"]) {
				service, _ := asMap(rawService)
				name, _ := service["name"].(string)
				if name == "" {
					continue
				}
				owner := d.id + "." + name
				c.collectServiceEnumRefs(service, serviceSchemas, func(enumID string) {
					add(enumID, owner)
				})
			}
		case strings.HasPrefix(d.id, "cap.events."):
			for _, key := range []string{"topics", "crons", "dispatches"} {
				for _, rawItem := range asList(d.doc[key]) {
					item, _ := asMap(rawItem)
					owner, _ := item["id"].(string)
					if owner == "" {
						continue
					}
					c.collectServiceEnumRefs(item, serviceSchemas, func(enumID string) {
						add(enumID, owner)
					})
				}
			}
		case strings.HasPrefix(d.id, "cap.protocols.http."):
			c.collectHTTPEnumRefs(d.doc, d.id, serviceSchemas, add)
		default:
			c.collectEnumRefs(d.doc, func(enumID string) {
				add(enumID, d.id)
			})
		}
	}
	return usages
}

// collectServiceEnumRefs 收集一个 service 项的直接枚举引用，以及它引用的本地 schema 中的枚举引用。
func (c *Checker) collectServiceEnumRefs(v any, schemas map[string]map[string]any, fn func(string)) {
	c.collectEnumRefs(v, fn)
	seen := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		if m, ok := asMap(node); ok {
			for k, child := range m {
				if k == "type" || k == "ref" {
					if s, ok := child.(string); ok {
						if id, ok := serviceSchemaID(s); ok && !seen[id] {
							seen[id] = true
							if def := schemas[id]; def != nil {
								c.collectEnumRefs(def, fn)
								walk(def)
							}
						}
					}
				}
				walk(child)
			}
			return
		}
		for _, item := range asList(node) {
			walk(item)
		}
	}
	walk(v)
}

// collectHTTPEnumRefs 只把被 operation 引用的 x-enum component 计入使用；
// 未被任何 operation 引用的 component 不算使用。
func (c *Checker) collectHTTPEnumRefs(doc map[string]any, fileID string, serviceSchemas map[string]map[string]any, add func(string, string)) {
	policy, err := c.loadHTTPPolicy()
	if err != nil {
		return
	}
	rawEnums := map[string][]string{}
	rawPaths, _ := asMap(doc["paths"])
	for _, rawPath := range rawPaths {
		pathItem, _ := asMap(rawPath)
		for method, rawOp := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete", "head", "options":
			default:
				continue
			}
			op, _ := asMap(rawOp)
			opID, _ := op["id"].(string)
			if opID == "" {
				if contract, ok := asMap(op["contract"]); ok {
					opID, _ = contract["id"].(string)
				}
			}
			if opID == "" {
				continue
			}
			c.collectServiceEnumRefs(op, serviceSchemas, func(enumID string) {
				rawEnums[opID] = append(rawEnums[opID], enumID)
			})
		}
	}
	if err := openapispec.NormalizeHTTPDocument(doc, c.httpTypeResolver, c.strictHTTP, openapispec.HTTPNormalizeOptions{TransportHeaders: policy.TransportHeaders, BodyTypes: policy.BodyTypes}); err != nil {
		// 归一化会就地改写文档；失败时用归一化前收集的原始 operation 枚举引用，
		// 避免 enum.used_by 因文件级格式错误丢掉整组使用点。
		for opID, enumIDs := range rawEnums {
			for _, enumID := range enumIDs {
				add(enumID, opID)
			}
		}
		return
	}
	components, _ := asMap(doc["components"])
	schemas, _ := asMap(components["schemas"])
	responses, _ := asMap(components["responses"])
	headers, _ := asMap(components["headers"])
	parameters, _ := asMap(components["parameters"])
	enumByComponent := map[string]string{}
	for name, raw := range schemas {
		component, _ := asMap(raw)
		enumID, _ := component["x-enum"].(string)
		if enumID == "" {
			if source, ok := asMap(component["source"]); ok {
				enumID, _ = source["enum"].(string)
			}
		}
		if strings.HasPrefix(enumID, "data.enums.") {
			enumByComponent[name] = enumID
		}
	}
	componentNode := func(kind, name string) map[string]any {
		switch kind {
		case "schemas":
			node, _ := asMap(schemas[name])
			return node
		case "responses":
			node, _ := asMap(responses[name])
			return node
		case "headers":
			node, _ := asMap(headers[name])
			return node
		case "parameters":
			node, _ := asMap(parameters[name])
			return node
		}
		return nil
	}
	var walkNode func(node map[string]any, owner string, seen map[string]bool)
	var walkRef func(ref, owner string, seen map[string]bool)
	walkRef = func(ref, owner string, seen map[string]bool) {
		prefix, name := "", ""
		for _, candidate := range []string{"schemas", "responses", "headers", "parameters"} {
			marker := "#/components/" + candidate + "/"
			if strings.HasPrefix(ref, marker) {
				prefix = candidate
				name = strings.TrimPrefix(ref, marker)
				break
			}
		}
		if prefix == "" || name == "" {
			return
		}
		key := prefix + ":" + name
		if seen[key] {
			return
		}
		seen[key] = true
		if prefix == "schemas" {
			if enumID := enumByComponent[name]; enumID != "" {
				add(enumID, owner)
			}
			component, _ := asMap(schemas[name])
			serviceRef, _ := component["x-service-type"].(string)
			if serviceRef == "" {
				if source, ok := asMap(component["source"]); ok {
					serviceRef, _ = source["service_type"].(string)
				}
			}
			if serviceRef != "" {
				if id, ok := serviceSchemaID(serviceRef); ok {
					if def := serviceSchemas[id]; def != nil {
						c.collectServiceEnumRefs(def, serviceSchemas, func(enumID string) {
							add(enumID, owner)
						})
					}
				}
			}
		}
		node := componentNode(prefix, name)
		if node != nil {
			collectRefs(node, func(child string) {
				walkRef(child, owner, seen)
			})
		}
	}
	walkNode = func(node map[string]any, owner string, seen map[string]bool) {
		collectRefs(node, func(ref string) {
			walkRef(ref, owner, seen)
		})
	}
	paths, _ := asMap(doc["paths"])
	for _, rawPath := range paths {
		path, _ := asMap(rawPath)
		for method, rawOp := range path {
			op, ok := asMap(rawOp)
			if !ok {
				continue
			}
			owner, _ := op["x-contract-id"].(string)
			if owner == "" {
				if contract, ok := asMap(op["contract"]); ok {
					owner, _ = contract["id"].(string)
				}
			}
			if owner == "" {
				owner = fileID + "." + method
			}
			walkNode(op, owner, map[string]bool{})
		}
	}
}

func serviceSchemaID(v string) (string, bool) {
	base, ok := parseServiceType(v)
	if !ok {
		return "", false
	}
	if strings.HasPrefix(base, "cap.services.") && strings.Contains(base, ".schemas.") {
		return base, true
	}
	return "", false
}

func enumUsageFor(usages map[string]*enumUsage, id string) *enumUsage {
	u := usages[id]
	if u == nil {
		u = &enumUsage{entities: map[string]bool{}, capabilities: map[string]bool{}}
		usages[id] = u
	}
	return u
}

// collectEnumRefs 收集任意层级 type: data.enums.* 引用；只收集，不报错。
func (c *Checker) collectEnumRefs(v any, fn func(string)) {
	if m, ok := asMap(v); ok {
		for k, child := range m {
			if k == "type" {
				if t, ok := child.(string); ok {
					if enumID, ok := enumTypeRef(t); ok {
						fn(enumID)
					}
				}
			}
			if k == "ref" || k == "x-enum" {
				if t, ok := child.(string); ok && strings.HasPrefix(t, "data.enums.") {
					fn(t)
				}
			}
			c.collectEnumRefs(child, fn)
		}
		return
	}
	if list, ok := asList2(v); ok {
		for _, item := range list {
			c.collectEnumRefs(item, fn)
		}
	}
}

// enumTypeRef 提取 type 中的枚举 ID，兼容 *T 与 List<T>。
func enumTypeRef(typ string) (string, bool) {
	typ = normalizeContractType(typ)
	typ = strings.TrimPrefix(typ, "*")
	if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
		typ = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(typ, "List<"), ">"))
		typ = strings.TrimPrefix(typ, "*")
	}
	if strings.HasPrefix(typ, "data.enums.") {
		return typ, true
	}
	return "", false
}

// EnumUsedBy 计算每个枚举 ID 的期望 used_by 集合，供 gen-graph 写回。
// 过滤 enum-used-by 自身，避免生成前必须先手工对齐；其他契约错误仍然阻止生成。
func EnumUsedBy(root string) (map[string][]string, error) {
	c := &Checker{root: root, strictHTTP: true}
	c.run()
	var others []string
	for _, e := range c.errs {
		if isGeneratedDriftError(e) {
			continue
		}
		others = append(others, e)
	}
	if len(others) > 0 {
		return nil, fmt.Errorf("contracts invalid: %s", strings.Join(others, "; "))
	}
	out := map[string][]string{}
	for id, usage := range c.enumUsages() {
		set := map[string]bool{}
		for entityID := range usage.entities {
			set[entityID] = true
		}
		for capID := range usage.capabilities {
			set[capID] = true
		}
		out[id] = sortedKeys(set)
	}
	return out, nil
}

// isGeneratedDriftError 判断错误是否只是生成字段尚未写回；gen-graph 需要先写回再校验。
func isGeneratedDriftError(e string) bool {
	if strings.Contains(e, "does not list the query in used_by") || strings.Contains(e, "lists the query but index_ref does not") {
		return true
	}
	for _, rule := range []string{
		"[enum-used-by]",
		"[entity-included-by]",
		"[entity-used-by]",
		"[entity-validation-used-by]",
		"[constraint-used-by]",
		"[table-entities]",
		"[table-queries-generated]",
		"[table-referenced-by]",
		"[query-used-by]",
		"[index-used-by-ref]",
		"[query-index-used-by]",
		"[schema.generated-range]",
	} {
		if strings.Contains(e, rule) {
			return true
		}
	}
	return false
}

// enumDeclared 报告 data.enums.* 是否已登记；注册表未加载时返回 true，避免部分测试误报。
func (c *Checker) enumDeclared(id string) bool {
	if c.enumIDs == nil {
		return true
	}
	return c.enumIDs[id]
}

// checkEnumUsedBy 要求 used_by 与 entity 字段和 capability 引用的并集完全一致。
func (c *Checker) checkEnumUsedBy(rel string, i int, id string, m map[string]any, usage *enumUsage) {
	raw, ok := m["used_by"]
	if !ok {
		return // schema.required 报缺失
	}
	expected := map[string]bool{}
	if usage != nil {
		for entityID := range usage.entities {
			expected[entityID] = true
		}
		for capabilityID := range usage.capabilities {
			expected[capabilityID] = true
		}
	}
	declared := stringSet(asList(raw))
	if len(declared) == 0 {
		return // 空值由 schema.generated-range 报出
	}
	if !sameSet(declared, expected) {
		c.rulef("enum-used-by", "%s: enums[%d] (%s): used_by must be %v, got %v", rel, i, id, sortedKeys(expected), sortedKeys(declared))
	}
}

// checkEnumRefs 校验表列引用与所有契约中的 data.enums.* 类型引用。
// 表列不引用枚举；枚举引用可出现在 entity / capability / feature 等任意契约。
func (c *Checker) checkEnumRefs(tableIDs map[string]string, cols map[string]map[string]bool, enums map[string]map[string]bool) {
	for _, rel := range tableIDs {
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			continue
		}
		for i, raw := range asList(doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			if t, _ := m["type"].(string); isTableColumnType(t) {
				checkColumnRef(c, t, tableIDs, cols, fmt.Sprintf("%s: columns[%d] (%s)", rel, i, m["name"]))
			}
		}
	}
	for _, f := range c.contractFiles() {
		rel := relPath(c.root, f)
		if strings.HasPrefix(rel, "contracts/_meta/") ||
			strings.HasPrefix(rel, c.tableDir()+"/") ||
			strings.HasPrefix(rel, filepath.Dir(baseContractPath)+"/") ||
			strings.HasPrefix(rel, "contracts/generated/") {
			continue
		}
		doc, err := loadYAML(f)
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		c.scanEnumTypeRefs(doc, rel, "", enums)
	}
}

// scanEnumTypeRefs 递归查找任意层级的 type: data.enums.* 引用。
func (c *Checker) scanEnumTypeRefs(v any, rel, path string, enums map[string]map[string]bool) {
	if m, ok := asMap(v); ok {
		for k, child := range m {
			childPath := k
			if path != "" {
				childPath = path + "." + k
			}
			if k == "type" {
				if t, ok := child.(string); ok {
					if enumID, ok := enumTypeRef(t); ok {
						if _, found := enums[enumID]; !found {
							c.rulef("schema.ref", "%s: %s: unknown enum %s", rel, childPath, enumID)
						}
					}
				}
			}
			if k == "ref" {
				if t, ok := child.(string); ok && strings.HasPrefix(t, "data.enums.") {
					if _, found := enums[t]; !found {
						c.rulef("schema.ref", "%s: %s: unknown enum %s", rel, childPath, t)
					}
				}
			}
			c.scanEnumTypeRefs(child, rel, childPath, enums)
		}
		return
	}
	if list, ok := asList2(v); ok {
		for i, child := range list {
			c.scanEnumTypeRefs(child, rel, fmt.Sprintf("%s[%d]", path, i), enums)
		}
	}
}

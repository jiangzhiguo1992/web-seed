// enums.go 校验 data.enums 集合结构、ID/名称/类型名唯一性、code 唯一性与全仓枚举引用。
package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func (c *Checker) checkEnums(schema *Schema) map[string]map[string]bool {
	c.checkEnumSchema(schema)
	if schema.Targets.File == "" || schema.Targets.ID == "" {
		c.rulef("schema.targets", "%s: targets.file and targets.id must not be empty", schema.ID)
		return map[string]map[string]bool{}
	}
	path := filepath.Join(c.root, schema.Targets.File)
	rel := relPath(c.root, path)
	doc, err := loadYAML(path)
	if err != nil {
		c.rulef("schema.load", "%s: %v", rel, err)
		return map[string]map[string]bool{}
	}
	c.validateDoc(doc, schema.Fields, rel)
	c.checkStability(doc, rel)
	if s, ok := doc["summary"].(string); ok && strings.TrimSpace(s) == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	if doc["id"] != schema.Targets.ID {
		c.rulef("enum-id", "%s: id must be %s", rel, schema.Targets.ID)
	}
	prefix := schema.Targets.ID + "."
	usages := c.enumUsages()
	enums := map[string]map[string]bool{}
	names := map[string]bool{}
	goTypes := map[string]string{}
	tsTypes := map[string]string{}
	rawEnums := asList(doc["enums"])
	if len(rawEnums) == 0 {
		c.rulef("schema.non-empty", "%s: enums must not be empty", rel)
	}
	for i, raw := range rawEnums {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: enums[%d] must be an object", rel, i)
			continue
		}
		id, _ := m["id"].(string)
		name, _ := m["name"].(string)
		if id != prefix+name {
			c.rulef("enum-id-matches-name", "%s: enums[%d]: id must be %s%s, got %s", rel, i, prefix, name, id)
		}
		if _, ok := enums[id]; ok {
			c.rulef("schema.contract-id-unique", "%s: enums[%d]: duplicate id %s", rel, i, id)
		}
		if names[name] {
			c.rulef("schema.unique-names", "%s: enums[%d]: duplicate name %s", rel, i, name)
		}
		names[name] = true
		c.checkEnumTypeName(goTypes, "go_type", i, name, m, rel)
		c.checkEnumTypeName(tsTypes, "ts_type", i, name, m, rel)
		values := map[string]bool{}
		codes := map[int]bool{}
		rawValues := asList(m["values"])
		if len(rawValues) == 0 {
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
		enums[id] = values
		c.checkEnumUsedBy(rel, i, id, m, usages[id])
	}
	return enums
}

// checkEnumSchema 防止 meta 字段树被误删；硬校验依赖这些 required 字段。
func (c *Checker) checkEnumSchema(schema *Schema) {
	for _, name := range []string{"schema_version", "id", "summary", "stability"} {
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
	for _, name := range []string{"id", "name", "go_type", "ts_type", "used_by", "values"} {
		f, ok := contractspec.FindField(enums.ItemFields, name)
		if !ok || !f.Required {
			c.rulef("schema.meta", "%s: enums.%s must be required", schema.ID, name)
		}
	}
	usedBy, ok := contractspec.FindField(enums.ItemFields, "used_by")
	if ok {
		for _, name := range []string{"entities", "tables"} {
			f, found := contractspec.FindField(usedBy.Fields, name)
			if !found || !f.Required {
				c.rulef("schema.meta", "%s: enums.used_by.%s must be required", schema.ID, name)
			}
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

// enumUsage 记录一个枚举被哪些 entity 使用，以及这些 entity 对应的物理表。
type enumUsage struct {
	entities map[string]bool
	tables   map[string]bool
}

// enumUsages 扫描 entity 字段中的 data.enums.* 引用，忽略 capability/feature。
func (c *Checker) enumUsages() map[string]*enumUsage {
	usages := map[string]*enumUsage{}
	for _, f := range glob(c.root, "contracts/data/entities/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			continue
		}
		entityID, _ := doc["id"].(string)
		tables := entityTableRefs(doc)
		// 只统计字段上的枚举引用；来源表按 column 或单表实体确定。
		for _, raw := range asList(doc["fields"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			typ, _ := m["type"].(string)
			if !strings.HasPrefix(typ, "data.enums.") {
				continue
			}
			u := usages[typ]
			if u == nil {
				u = &enumUsage{entities: map[string]bool{}, tables: map[string]bool{}}
				usages[typ] = u
			}
			if entityID != "" {
				u.entities[entityID] = true
			}
			src := ""
			if ref, _ := m["column"].(string); ref != "" {
				if t, _, ok := splitColumnRef(ref); ok {
					src = t
				}
			} else if len(tables) == 1 {
				src = tables[0]
			}
			if src != "" {
				u.tables[src] = true
			}
		}
	}
	return usages
}

// checkEnumUsedBy 要求 used_by.entities/tables 与 entity 实际引用完全一致。
func (c *Checker) checkEnumUsedBy(rel string, i int, id string, m map[string]any, usage *enumUsage) {
	ub, ok := asMap(m["used_by"])
	if !ok {
		return // schema.required 报缺失
	}
	declaredEntities := stringSet(asList(ub["entities"]))
	declaredTables := stringSet(asList(ub["tables"]))
	expectedEntities := map[string]bool{}
	expectedTables := map[string]bool{}
	if usage != nil {
		expectedEntities = usage.entities
		expectedTables = usage.tables
	}
	if !sameSet(declaredEntities, expectedEntities) {
		c.rulef("enum-used-by", "%s: enums[%d] (%s): used_by.entities must be %v, got %v", rel, i, id, sortedKeys(expectedEntities), sortedKeys(declaredEntities))
	}
	if !sameSet(declaredTables, expectedTables) {
		c.rulef("enum-used-by", "%s: enums[%d] (%s): used_by.tables must be %v, got %v", rel, i, id, sortedKeys(expectedTables), sortedKeys(declaredTables))
	}
}

// checkEnumRefs 校验表列引用与所有契约中的 data.enums.* 类型引用。
// 表列不引用枚举；枚举引用可出现在 entity / capability / feature 等任意契约。
func (c *Checker) checkEnumRefs(tableIDs map[string]string, cols map[string]map[string]bool, enums map[string]map[string]bool, refs ColumnRefs) {
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
			if t, _ := m["type"].(string); isColumnRefType(t, refs) {
				checkColumnRef(c, t, tableIDs, cols, fmt.Sprintf("%s: columns[%d] (%s)", rel, i, m["name"]))
			}
		}
	}
	for _, f := range walkYAML(c.root, "contracts") {
		rel := relPath(c.root, f)
		if strings.HasPrefix(rel, "contracts/_meta/") ||
			strings.HasPrefix(rel, "contracts/data/tables/") ||
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

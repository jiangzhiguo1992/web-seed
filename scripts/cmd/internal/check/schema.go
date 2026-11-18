// schema.go 校验受限 schema DSL：两层语法。
//
// schema 节点（object / array / scalar / ref）只描述值域；
// property 条目（name + schema + 槽位注解）只允许出现在 object.properties[]。
// 槽位键出现在节点位置直接报 schema.slot-position，避免 read_only 等被误读为类型属性。
// 标量约束只在 _meta/_types.yaml，这里不重复定义 minLength/minimum 等线格式键。
package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// validateSchemaNode 校验一个值域节点。
func (c *Checker) validateSchemaNode(v any, path string) {
	c.validateSchemaNodeWith(v, path, nil)
}

// validateSchemaNodeWith 校验值域节点；allowed 非空时限制节点种类。
func (c *Checker) validateSchemaNodeWith(v any, path string, allowed []string) {
	m, ok := asMap(v)
	if !ok {
		c.rulef("schema.type", "%s: must be a schema object", path)
		return
	}
	kind, ok := contractspec.SchemaNodeKind(m, c.nodeKinds)
	if !ok {
		c.rulef("schema.type", "%s: schema node must have exactly one of ref or type", path)
		return
	}
	if !allowsSchemaKind(allowed, kind) {
		c.rulef("schema.kind", "%s: schema kind %s is not allowed here (allowed: %v)", path, kind, allowed)
		return
	}
	kindSpec, ok := c.nodeKinds[kind]
	if !ok {
		c.rulef("schema.type", "%s: schema kind %s is not defined", path, kind)
		return
	}
	allowedKeys := map[string]bool{}
	for _, key := range kindSpec.Keys {
		allowedKeys[key.Name] = true
	}
	slots := c.propertyKeySet()
	for key := range m {
		if allowedKeys[key] {
			continue
		}
		if slots[key] {
			c.rulef("schema.slot-position", "%s: %q is a property key and must not appear on a schema node", path, key)
			continue
		}
		c.rulef("schema.unknown-field", "%s: unknown schema key %q", path, key)
	}
	switch kind {
	case "object":
		c.validateObjectNode(m, path)
	case "array":
		c.validateArrayNode(m, path)
	case "scalar":
		c.validateScalarNode(m, path)
	case "ref":
		c.validateRefNode(m, path)
	default:
		c.rulef("schema.type", "%s: unsupported schema kind %s", path, kind)
	}
	c.validateSchemaKeyValues(m, path, kindSpec.Keys)
}

// validateObjectNode 校验对象节点的额外不变量：属性名唯一、方向互斥、槽位默认值。
// 字段是否齐全、类型是否正确由 properties.item_fields 的通用校验负责。
func (c *Checker) validateObjectNode(m map[string]any, path string) {
	raw, present := m["properties"]
	if !present {
		c.rulef("schema.required", "%s: object node requires properties", path)
		return
	}
	props, ok := asList2(raw)
	if !ok {
		return // validateValue 的 list<object> 会报 must be list
	}
	names := map[string]bool{}
	for i, item := range props {
		pm, ok := asMap(item)
		if !ok {
			continue // validateValue 的 list<object> 会报 must be object
		}
		if name, _ := pm["name"].(string); name != "" {
			if names[name] {
				c.rulef("schema.unique", "%s: duplicate property %q", path, name)
			}
			names[name] = true
		}
		c.validatePropertyExtras(pm, fmt.Sprintf("%s.properties[%d]", path, i))
	}
}

// validatePropertyExtras 校验 properties 条目的额外不变量；通用字段规则由
// properties.item_fields 负责，这里不重复。
func (c *Checker) validatePropertyExtras(m map[string]any, path string) {
	if raw, ok := m["description"]; ok {
		if s, _ := raw.(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s.description: must not be empty", path)
		}
	}
	if ro, _ := m["read_only"].(bool); ro {
		if wo, _ := m["write_only"].(bool); wo {
			c.rulef("schema.conflict", "%s: read_only and write_only cannot both be true", path)
		}
	}
	if def, ok := m["default"]; ok {
		if sm, ok := asMap(m["schema"]); ok {
			if ref, _ := sm["ref"].(string); strings.HasPrefix(ref, "data.enums.") {
				if values := c.enumValues[ref]; values != nil {
					if s, ok := def.(string); !ok || !values[s] {
						c.rulef("schema.default", "%s.default: %v is not a value of %s", path, def, ref)
					}
				}
			} else if typ, _ := sm["type"].(string); typ != "" && typ != "object" && typ != "array" {
				if family := c.valueType(typ); family != "" {
					if ok, want := defaultMatchesType(def, family); !ok {
						c.rulef("schema.default", "%s.default: must be %s for type %s", path, want, family)
					}
				}
			}
		}
	}
}

// propertyKeySet 返回槽位键集合，用于区分「节点上放错位置的槽位键」与真正未知的键。
func (c *Checker) propertyKeySet() map[string]bool {
	set := make(map[string]bool, len(c.propertyKeys))
	for _, key := range c.propertyKeys {
		set[key.Name] = true
	}
	return set
}

func (c *Checker) validateArrayNode(m map[string]any, path string) {
	if _, present := m["items"]; !present {
		c.rulef("schema.required", "%s: array node requires items", path)
	}
	min, hasMin := toInt(m["min_items"])
	max, hasMax := toInt(m["max_items"])
	if hasMin && min < 0 {
		c.rulef("schema.range", "%s.min_items: must be >= 0", path)
	}
	if hasMax && max < 0 {
		c.rulef("schema.range", "%s.max_items: must be >= 0", path)
	}
	if hasMin && hasMax && min > max {
		c.rulef("schema.range", "%s: min_items must be <= max_items", path)
	}
}

func (c *Checker) validateScalarNode(m map[string]any, path string) {
	typ, _ := m["type"].(string)
	if typ == "" {
		c.rulef("schema.non-empty", "%s.type: must not be empty", path)
		return
	}
	if typ == "object" || typ == "array" {
		c.rulef("schema.type", "%s.type: %s must use its kind structure", path, typ)
		return
	}
	if c.valueType(typ) == "" {
		c.rulef("schema.type", "%s.type: unknown value type %q", path, typ)
	}
}

func (c *Checker) validateRefNode(m map[string]any, path string) {
	ref, _ := m["ref"].(string)
	if ref == "" {
		c.rulef("schema.non-empty", "%s.ref: must not be empty", path)
		return
	}
	switch {
	case strings.HasPrefix(ref, "data.enums."):
		if !c.enumIDs[ref] {
			c.rulef("service-ref", "%s.ref: unknown enum %s", path, ref)
		}
	case strings.HasPrefix(ref, "data.entities."):
		if c.entities[ref] == nil {
			c.rulef("service-ref", "%s.ref: unknown entity %s", path, ref)
		}
	case strings.HasPrefix(ref, "data.tables."):
		c.rulef("service-ref", "%s.ref: %s is not a schema type; use data.entities.*", path, ref)
	default:
		if c.methodTypeDefs[ref] == nil {
			c.rulef("service-ref", "%s.ref: unknown schema %s", path, ref)
		}
	}
}

// validateSchemaKeyValues 按 Field 规范校验键值类型；types 与 type: schema
// 都由 validateValue 统一递归，结构分支不再重复。
func (c *Checker) validateSchemaKeyValues(m map[string]any, path string, keys []contractspec.Field) {
	for _, key := range keys {
		if key.Type == "" && len(key.Types) == 0 {
			continue
		}
		v, present := m[key.Name]
		if !present {
			continue
		}
		c.validateValue(v, key, path+"."+key.Name)
		if key.Name == "description" {
			if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
				c.rulef("schema.non-empty", "%s.description: must not be empty", path)
			}
		}
	}
}

// allowsSchemaKind 报告 kind 是否在允许列表内；空列表表示不限。
func allowsSchemaKind(allowed []string, kind string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, k := range allowed {
		if k == kind {
			return true
		}
	}
	return false
}

// schemaKindAllowed 报告名称是否是 SchemaNodeKind 支持的节点种类。
func schemaKindAllowed(kind string) bool {
	switch kind {
	case "object", "array", "scalar", "ref":
		return true
	}
	return false
}

// checkSchemaCycles 检测本地 schema 的循环引用；跨文件共享 methodTypeDefs。
func (c *Checker) checkSchemaCycles() {
	edges := map[string][]string{}
	for name, node := range c.methodTypeDefs {
		edges[name] = schemaLocalRefs(node)
	}
	const (
		unvisited = iota
		visiting
		visited
	)
	state := map[string]int{}
	var stack []string
	var visit func(string)
	visit = func(name string) {
		switch state[name] {
		case visiting:
			c.rulef("schema.cycle", "schemas: cycle %s -> %s", strings.Join(stack, " -> "), name)
			return
		case visited:
			return
		}
		state[name] = visiting
		stack = append(stack, name)
		for _, ref := range edges[name] {
			if _, ok := c.methodTypeDefs[ref]; ok {
				visit(ref)
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = visited
	}
	names := make([]string, 0, len(c.methodTypeDefs))
	for name := range c.methodTypeDefs {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		visit(name)
	}
}

// schemaLocalRefs 递归收集 node 内指向本地 schema 的 ref。
func schemaLocalRefs(v any) []string {
	var out []string
	switch x := v.(type) {
	case map[string]any:
		if ref, _ := x["ref"].(string); ref != "" && !strings.HasPrefix(ref, "data.") {
			out = append(out, ref)
		}
		for k, child := range x {
			if k == "ref" {
				continue
			}
			out = append(out, schemaLocalRefs(child)...)
		}
	case []any:
		for _, child := range x {
			out = append(out, schemaLocalRefs(child)...)
		}
	}
	return out
}

// valueType 返回标量类型对应的类型族；自定义类型解析到 family，
// family 不存在时返回空，避免 schema 层把断链的 custom type 当成有效类型。
func (c *Checker) valueType(typ string) string {
	if _, ok := c.valueTypes.TypeFamilies[typ]; ok {
		return typ
	}
	if customType, ok := c.valueTypes.CustomTypes[typ]; ok {
		if _, ok := c.valueTypes.TypeFamilies[customType.Family]; ok {
			return customType.Family
		}
	}
	return ""
}

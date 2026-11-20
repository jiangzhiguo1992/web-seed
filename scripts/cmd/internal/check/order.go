// order.go 按 _meta 模板的字段顺序校验契约实例的 YAML 键顺序。
// 模板是唯一顺序源：新增字段只改 _meta/<type>.yaml，不在 checker 里维护第二份顺序表。
package check

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// orderRules 指定不同层级的错误规则 ID；model 为空时沿用 nested。
type orderRules struct {
	top    string
	nested string
	model  string
}

// checkFieldOrder 从 <root>/<rel> 读取 YAML，并按 fields 的顺序校验。
func (c *Checker) checkFieldOrder(rel string, fields []Field, rules orderRules) {
	root, err := loadYAMLNode(filepath.Join(c.root, rel))
	if err != nil {
		c.rulef("schema.load", "%s: %v", rel, err)
		return
	}
	c.checkNodeOrder(root, fields, rel, "", rules)
}

// checkNodeOrder 递归校验 mapping 的键顺序。
// 只校验模板中声明的键；未知键留给 validateDoc 报 schema.unknown-field。
// Unordered 字段（运行时注入的类型族参数）不参与顺序约束。
func (c *Checker) checkNodeOrder(n *yaml.Node, fields []Field, rel, path string, rules orderRules) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	pos := make(map[string]int, len(fields))
	byName := make(map[string]Field, len(fields))
	for i, f := range fields {
		if f.Unordered {
			continue
		}
		pos[f.Name] = i
		byName[f.Name] = f
	}
	last, lastKey := -1, ""
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		if idx, ok := pos[key]; ok {
			if idx < last {
				rule := rules.nested
				if path == "" {
					rule = rules.top
				}
				if path == "model" && rules.model != "" {
					rule = rules.model
				}
				c.reportOrder(rule, rel, path, key, lastKey)
			}
			last, lastKey = idx, key
		}
		f, ok := byName[key]
		if !ok {
			continue
		}
		value := n.Content[i+1]
		childPath := joinPath(path, key)
		typ := normalizeTypeExpr(f.Type)
		switch {
		case value.Kind == yaml.MappingNode && len(f.Types) > 0 && typ != "map":
			c.checkSchemaNodeOrder(value, rel, childPath, rules)
		case value.Kind == yaml.MappingNode && typ == "map" && len(f.Types) > 0:
			for j := 0; j+1 < len(value.Content); j += 2 {
				c.checkSchemaNodeOrder(value.Content[j+1], rel, childPath+"."+value.Content[j].Value, rules)
			}
		case value.Kind == yaml.MappingNode && typ == "map<object>":
			for j := 0; j+1 < len(value.Content); j += 2 {
				if item := value.Content[j+1]; item.Kind == yaml.MappingNode {
					c.checkNodeOrder(item, f.ItemFields, rel, childPath+"."+value.Content[j].Value, rules)
				}
			}
		case value.Kind == yaml.MappingNode && typ == "map<list<object>>":
			for j := 0; j+1 < len(value.Content); j += 2 {
				list := value.Content[j+1]
				if list.Kind != yaml.SequenceNode {
					continue
				}
				for _, item := range list.Content {
					if item.Kind == yaml.MappingNode {
						c.checkNodeOrder(item, f.ItemFields, rel, childPath+"."+value.Content[j].Value+"[]", rules)
					}
				}
			}
		case value.Kind == yaml.MappingNode && len(f.Fields) > 0:
			c.checkNodeOrder(value, f.Fields, rel, childPath, rules)
		case value.Kind == yaml.SequenceNode && typ == "list<map<object>>":
			for _, item := range value.Content {
				if item.Kind != yaml.MappingNode {
					continue
				}
				for j := 0; j+1 < len(item.Content); j += 2 {
					if v := item.Content[j+1]; v.Kind == yaml.MappingNode {
						c.checkNodeOrder(v, f.ItemFields, rel, childPath+"[]."+item.Content[j].Value, rules)
					}
				}
			}
		case value.Kind == yaml.SequenceNode && len(f.ItemFields) > 0:
			for _, item := range value.Content {
				if item.Kind == yaml.MappingNode {
					c.checkNodeOrder(item, f.ItemFields, rel, childPath+"[]", rules)
				}
			}
		}
	}
}

// checkSchemaNodeOrder 按 _types.yaml schemas.<kind>.keys 的顺序校验 schema 节点自身的键顺序；
// object.properties 是 property 条目列表，条目顺序按 properties 的 item_fields 校验。
func (c *Checker) checkSchemaNodeOrder(n *yaml.Node, rel, path string, rules orderRules) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	nodeKind := schemaNodeKindFromYAML(n, c.nodeKinds)
	if nodeKind == "" {
		return
	}
	spec, ok := c.nodeKinds[nodeKind]
	if !ok {
		return
	}
	pos := map[string]int{}
	for i, key := range spec.Keys {
		pos[key.Name] = i
	}
	last, lastKey := -1, ""
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i].Value
		idx, ok := pos[key]
		if !ok {
			continue
		}
		if idx < last {
			c.reportOrder(rules.nested, rel, path, key, lastKey)
		}
		last, lastKey = idx, key
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i].Value, n.Content[i+1]
		switch {
		case key == "items" && value.Kind == yaml.MappingNode:
			c.checkSchemaNodeOrder(value, rel, path+".items", rules)
		case key == "properties" && value.Kind == yaml.SequenceNode:
			for j, item := range value.Content {
				if item.Kind == yaml.MappingNode {
					c.checkPropertyOrder(item, rel, fmt.Sprintf("%s.properties[%d]", path, j), rules)
				}
			}
		}
	}
}

// checkPropertyOrder 按 properties.item_fields 的顺序校验属性条目，并递归其 schema 子树。
func (c *Checker) checkPropertyOrder(n *yaml.Node, rel, path string, rules orderRules) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	pos := map[string]int{}
	for i, key := range c.propertyKeys {
		pos[key.Name] = i
	}
	last, lastKey := -1, ""
	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i].Value, n.Content[i+1]
		if idx, ok := pos[key]; ok {
			if idx < last {
				c.reportOrder(rules.nested, rel, path, key, lastKey)
			}
			last, lastKey = idx, key
		}
		if key == "schema" && value.Kind == yaml.MappingNode {
			c.checkSchemaNodeOrder(value, rel, path+".schema", rules)
		}
	}
}

// schemaNodeKindFromYAML 从映射节点读取 ref/type，按标准 schema 语义判定节点种类。
func schemaNodeKindFromYAML(n *yaml.Node, kinds map[string]SchemaKind) string {
	if n == nil || n.Kind != yaml.MappingNode {
		return ""
	}
	hasRef := false
	typ := ""
	for i := 0; i+1 < len(n.Content); i += 2 {
		switch n.Content[i].Value {
		case "ref":
			hasRef = true
		case "type":
			typ = n.Content[i+1].Value
		}
	}
	if hasRef && typ != "" {
		return ""
	}
	kind := ""
	switch normalizeTypeExpr(typ) {
	case "":
		if hasRef {
			kind = "ref"
		}
	case "object":
		kind = "object"
	case "array":
		kind = "array"
	case "map":
		kind = "map"
	default:
		kind = "scalar"
	}
	if kind == "" {
		return ""
	}
	if _, ok := kinds[kind]; !ok {
		return ""
	}
	return kind
}

// reportOrder 保留字面量 rulef 调用，供 gen_rule_ids 的 AST 扫描收集规则 ID。
func (c *Checker) reportOrder(rule, rel, path, key, before string) {
	where := rel + pathLabel(path)
	switch rule {
	case "table-key-order":
		c.rulef("table-key-order", "%s: field %s must come before %s (template order)", where, key, before)
	case "entity-key-order":
		c.rulef("entity-key-order", "%s: field %s must come before %s (template order)", where, key, before)
	case "entity-model-order":
		c.rulef("entity-model-order", "%s: field %s must come before %s (template order)", where, key, before)
	case "query-key-order":
		c.rulef("query-key-order", "%s: field %s must come before %s (template order)", where, key, before)
	case "enum-key-order":
		c.rulef("enum-key-order", "%s: field %s must come before %s (template order)", where, key, before)
	default:
		c.rulef("schema.field-order", "%s: field %s must come before %s (template order)", where, key, before)
	}
}

func pathLabel(path string) string {
	if path == "" {
		return ""
	}
	return ": " + path
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func loadYAMLNode(path string) (*yaml.Node, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return &doc, nil
		}
		return doc.Content[0], nil
	}
	return &doc, nil
}

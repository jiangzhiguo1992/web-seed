// order.go 按 _meta 模板的字段顺序校验契约实例的 YAML 键顺序。
// 模板是唯一顺序源：新增字段只改 _meta/<type>.yaml，不在 checker 里维护第二份顺序表。
package check

import (
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
		switch {
		case value.Kind == yaml.MappingNode && len(f.Fields) > 0:
			c.checkNodeOrder(value, f.Fields, rel, childPath, rules)
		case value.Kind == yaml.SequenceNode && len(f.ItemFields) > 0:
			for _, item := range value.Content {
				if item.Kind == yaml.MappingNode {
					c.checkNodeOrder(item, f.ItemFields, rel, childPath+"[]", rules)
				}
			}
		}
	}
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

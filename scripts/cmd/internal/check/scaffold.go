// scaffold.go 校验 contract-new 生成的契约骨架。
//
// 骨架只保证结构与字段顺序；generated 字段在 gen-graph 之前允许为空，
// 跨契约引用由 make contract-check 在用户补齐后校验。
package check

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ValidateScaffold 校验骨架的结构、字段顺序、文件名与文本字段的双引号。
// root 是仓库根；typ 是契约类型；doc 是 contract-new 的输出。
func ValidateScaffold(root, typ string, doc []byte, fileName string) []string {
	c := &Checker{root: root}
	schema, err := c.loadSchemaByType(typ)
	if err != nil {
		return []string{err.Error()}
	}
	var m map[string]any
	if err := yaml.Unmarshal(doc, &m); err != nil {
		return []string{"scaffold: " + err.Error()}
	}
	relaxed := *schema
	relaxed.Fields = relaxGeneratedFields(schema.Fields)
	c.nodeKinds = schema.NodeKinds
	c.propertyKeys = schema.PropertyKeys
	c.valueTypes = schema.ValueTypes

	c.validateDoc(m, relaxed.Fields, fileName)

	dir, err := os.MkdirTemp(filepath.Join(root, "contracts"), ".scaffold-")
	if err != nil {
		return append(c.errs, "scaffold: "+err.Error())
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, fileName)
	if err := os.WriteFile(path, doc, 0o644); err != nil {
		return append(c.errs, "scaffold: "+err.Error())
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return append(c.errs, "scaffold: "+err.Error())
	}
	c.checkFieldOrder(rel, relaxed.Fields, orderRules{})
	c.checkTargetFileName(&relaxed, fileName, rel)
	var node yaml.Node
	if err := yaml.Unmarshal(doc, &node); err != nil {
		return append(c.errs, "scaffold: "+err.Error())
	}
	c.checkProseQuotes(&node, rel)
	return c.errs
}

// relaxGeneratedFields 放宽 generated 字段的 required/min_items：
// gen-graph 会在引用建立后填充这些字段。
func relaxGeneratedFields(fields []Field) []Field {
	out := make([]Field, len(fields))
	for i, f := range fields {
		out[i] = f
		if f.Generated {
			out[i].Required = false
			out[i].MinItems = nil
		}
		out[i].Fields = relaxGeneratedFields(f.Fields)
		out[i].ItemFields = relaxGeneratedFields(f.ItemFields)
	}
	return out
}

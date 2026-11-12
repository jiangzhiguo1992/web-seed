// Package check 实现契约校验：结构、ID、基类、列/约束/索引、引用、used_by、index 与 stability。
//
// 当前覆盖：
//   - _meta 的 description/generates/caution 双引号强制
//   - data.tables/*.yaml 的结构、ID/文件、基类继承、列/约束/索引、used_by、do_not 具体性
//   - data/enums.yaml 的结构、item ID、值唯一性
//   - 表列引用的 data.enums.* / data.tables.*.col.* 是否存在
//   - 枚举默认值、enum 约束与列类型一致
//   - 禁止 foreign_key；unique 约束与唯一索引的软删语义区分
//   - owner_module 写归属（capabilities.persistence）
//   - entity/query/capability 的表引用是否存在
//
// 不覆盖（后续扩展）：
//   - 全类型 schema
//   - graph.json / 越层引用
//   - generated diff
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

type Schema = contractspec.Schema
type Targets = contractspec.Targets
type ColumnTypes = contractspec.ColumnTypes
type ColumnRefs = contractspec.ColumnRefs
type ValueTypeFamily = contractspec.ValueTypeFamily
type Field = contractspec.Field

var baseColumns = []string{"id", "status", "created_at", "updated_at", "deleted_at"}

type Checker struct {
	root        string
	errs        []string
	tableOwners map[string]string
}

// Run 执行全部契约校验并返回错误列表。
func Run(root string) []string {
	c := &Checker{root: root}
	c.run()
	return c.errs
}

// rulef 追加带稳定规则 ID 的错误，AI/CI 可直接按 [rule-id] 检索。
func (c *Checker) rulef(rule, format string, args ...any) {
	c.errs = append(c.errs, fmt.Sprintf("[%s] %s", rule, fmt.Sprintf(format, args...)))
}

func (c *Checker) run() {
	c.checkMetaQuotes()
	c.checkTypeMeta()
	tableSchema, err := loadSchema(filepath.Join(c.root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		c.rulef("schema.load", "load tables schema: %v", err)
		return
	}
	enumSchema, err := loadSchema(filepath.Join(c.root, "contracts/_meta/data.enums.yaml"))
	if err != nil {
		c.rulef("schema.load", "load enums schema: %v", err)
		return
	}

	tableIDs, cols := c.checkTables(tableSchema)
	enumValues := c.checkEnums(enumSchema)
	c.checkEnumRefs(tableIDs, cols, enumValues)
	c.checkCrossFileTableRefs(tableIDs, cols)
	c.checkUsedBy(tableIDs)
	c.checkIndex(tableIDs)
}

// checkMetaQuotes 强制 _meta 的 description/generates/caution 使用双引号，
// 避免 AI/正则按未加引号的字符串解析时出现歧义。
func (c *Checker) checkMetaQuotes() {
	files, _ := filepath.Glob(filepath.Join(c.root, "contracts/_meta/*.yaml"))
	sort.Strings(files)
	for _, file := range files {
		rel := relPath(c.root, file)
		b, err := os.ReadFile(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(b, &doc); err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		c.checkProseQuotes(&doc, rel)
	}
}

func (c *Checker) checkProseQuotes(n *yaml.Node, rel string) {
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			c.checkProseQuotes(child, rel)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			switch k.Value {
			case "description", "caution":
				if v.Kind != yaml.ScalarNode {
					c.rulef("schema.quote", "%s: %s must be a double-quoted string", rel, k.Value)
				} else if v.Style != yaml.DoubleQuotedStyle {
					c.rulef("schema.quote", "%s: %s must be double-quoted", rel, k.Value)
				}
			case "generates":
				// _types.yaml 的 generates 是生成物列表；标量时必须双引号。
				if v.Kind == yaml.ScalarNode && v.Style != yaml.DoubleQuotedStyle {
					c.rulef("schema.quote", "%s: %s must be double-quoted", rel, k.Value)
				}
			}
			c.checkProseQuotes(v, rel)
		}
	}
}

type typeMeta struct {
	SchemaSemantics struct {
		HardFields []string `yaml:"hard_fields"`
		SoftFields []string `yaml:"soft_fields"`
	} `yaml:"schema_semantics"`
	Types map[string]struct {
		Generates []string `yaml:"generates"`
		DoNot     []string `yaml:"do_not"`
	} `yaml:"types"`
}

// checkTypeMeta 校验 _types.yaml 能按预期形状解析：
// do_not 必须是字符串列表，generates 必须是字符串列表。
// 这类文件不被 schema loader 读取，但 AI 会直接读；形状错误必须显式报出。
func (c *Checker) checkTypeMeta() {
	path := filepath.Join(c.root, "contracts/_meta/_types.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		c.rulef("schema.load", "contracts/_meta/_types.yaml: %v", err)
		return
	}
	var meta typeMeta
	if err := yaml.Unmarshal(b, &meta); err != nil {
		c.rulef("schema.load", "contracts/_meta/_types.yaml: %v", err)
		return
	}
	if len(meta.SchemaSemantics.HardFields) == 0 || len(meta.SchemaSemantics.SoftFields) == 0 {
		c.rulef("schema.meta", "contracts/_meta/_types.yaml: schema_semantics.hard_fields/soft_fields must not be empty")
	}
}

func loadSchema(path string) (*Schema, error) {
	s, err := contractspec.LoadSchema(path)
	if err != nil {
		return nil, err
	}
	if err := validatePatterns(s.Fields); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateFieldTypes(s.Fields); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func filterFields(fields []Field, names map[string]bool) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if names[f.Name] {
			out = append(out, f)
		}
	}
	return out
}

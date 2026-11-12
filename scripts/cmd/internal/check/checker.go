// Package check 实现契约校验：结构、ID、基类、列/约束/索引、引用、used_by、index 与 stability。
//
// 当前覆盖：
//   - contracts 下文本字段（description/comment/summary/do_not 等）双引号强制
//   - data.tables/*.yaml 的结构、ID/文件、基类继承、列/约束/索引、used_by、do_not 具体性
//   - data/enums.yaml 的结构、item ID、值唯一性、code 唯一性
//   - 表列引用的 column_refs 前缀是否存在；表列不引用 data.enums.*
//   - status 必须是 integer；枚举只在 entity
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

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

type Schema = contractspec.Schema
type Targets = contractspec.Targets
type TargetException = contractspec.TargetException
type ColumnTypes = contractspec.ColumnTypes
type Rule = contractspec.Rule
type ColumnRefs = contractspec.ColumnRefs
type ValueTypeFamily = contractspec.ValueTypeFamily
type Field = contractspec.Field

var baseColumns = []string{"id", "status", "created_at", "updated_at", "deleted_at"}

type Checker struct {
	root            string
	errs            []string
	tableOwners     map[string]string
	baseID          string
	declaredRuleIDs map[string]bool
	queryIDs        map[string]string
	queryTables     map[string]map[string]bool
	querySQL        map[string]string
	queryIndexCols  map[string][][]string
	tableIndexes    map[string][]tableIndex
	tablePrimaryKey map[string][]string
}

// Run 执行全部契约校验并返回错误列表。
func Run(root string) []string {
	c := &Checker{root: root}
	c.run()
	return c.errs
}

// rulef 追加带稳定规则 ID 的错误，AI/CI 可直接按 [rule-id] 检索。
// 未登记的 rule ID 先报 schema.rules，避免动态 rule 绕过生成映射。
func (c *Checker) rulef(rule, format string, args ...any) {
	if !knownRuleIDs[rule] {
		c.errs = append(c.errs, fmt.Sprintf("[schema.rules] unregistered rule id %q", rule))
	}
	c.errs = append(c.errs, fmt.Sprintf("[%s] %s", rule, fmt.Sprintf(format, args...)))
}

func (c *Checker) run() {
	c.checkContractProseQuotes()
	c.checkTypeMeta()
	tableSchema, err := c.loadSchemaByType("data.tables")
	if err != nil {
		c.rulef("schema.load", "load tables schema: %v", err)
		return
	}
	enumSchema, err := c.loadSchemaByType("data.enums")
	if err != nil {
		c.rulef("schema.load", "load enums schema: %v", err)
		return
	}

	c.loadQueryIndex()
	tableIDs, cols := c.checkTables(tableSchema)
	c.checkRedundancy(tableIDs, cols)
	c.loadTableIndexes(tableIDs)
	c.checkQueryIndexColumns(tableIDs, cols)
	enumValues := c.checkEnums(enumSchema)
	c.checkRules(tableSchema)
	c.checkRules(enumSchema)
	c.checkUndeclaredRuleIDs()
	c.checkEnumRefs(tableIDs, cols, enumValues, tableSchema.ColumnRefs)
	c.checkCrossFileTableRefs(tableIDs, cols)
	c.checkUsedBy(tableIDs)
	c.checkIndex(tableIDs)
}

// checkContractProseQuotes 强制 contracts 下文本字段使用双引号，
// 避免正则/外部工具按未加引号的字符串解析时出现歧义。
func (c *Checker) checkContractProseQuotes() {
	for _, file := range walkYAML(c.root, "contracts") {
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

var proseScalarFields = map[string]bool{
	"action": true, "body": true, "caution": true, "comment": true,
	"consistency": true, "description": true, "display_name": true,
	"error": true, "error_copy_uniform": true, "explanation": true,
	"generates": true, "handling": true, "id_policy": true, "instead": true,
	"label": true, "message": true, "naming": true, "note": true,
	"on_failure": true, "on_success": true, "ordering": true, "rationale": true,
	"rate_limit_hint": true, "reason": true, "route_label": true, "rule": true,
	"semantics": true, "scope": true, "storage": true, "success": true,
	"summary": true, "text": true, "title": true, "ui": true,
	"when": true, "why": true,
}

var proseListFields = map[string]bool{
	"allowed_cases": true, "client_requirements": true, "common_mistakes": true,
	"definition": true, "do_not": true, "forbidden": true,
	"forbidden_usage": true, "global_notes": true, "non_breaking": true,
	"process": true, "rules": true, "scope": true,
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
			if proseScalarFields[k.Value] {
				switch {
				case v.Kind == yaml.ScalarNode:
					if v.Style != yaml.DoubleQuotedStyle {
						c.rulef("schema.quote", "%s: %s must be double-quoted", rel, k.Value)
					}
				case k.Value == "description" || k.Value == "caution":
					c.rulef("schema.quote", "%s: %s must be a double-quoted string", rel, k.Value)
				}
			}
			if proseListFields[k.Value] && v.Kind == yaml.SequenceNode {
				for _, item := range v.Content {
					if item.Kind == yaml.ScalarNode && item.Style != yaml.DoubleQuotedStyle {
						c.rulef("schema.quote", "%s: %s list items must be double-quoted", rel, k.Value)
					}
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
	SchemaRules []struct {
		ID            string   `yaml:"id"`
		Text          string   `yaml:"text"`
		ImplementedBy []string `yaml:"implemented_by"`
	} `yaml:"schema_rules"`
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
	if len(meta.SchemaRules) == 0 {
		c.rulef("schema.meta", "contracts/_meta/_types.yaml: schema_rules must not be empty")
	}
	seenRule := map[string]bool{}
	for i, r := range meta.SchemaRules {
		if r.ID == "" || r.Text == "" {
			c.rulef("schema.meta", "contracts/_meta/_types.yaml: schema_rules[%d] must have id and text", i)
		}
		if seenRule[r.ID] {
			c.rulef("schema.meta", "contracts/_meta/_types.yaml: duplicate schema rule %q", r.ID)
		}
		seenRule[r.ID] = true
		c.checkImplementedBy(r.ID, "contracts/_meta/_types.yaml: schema_rules["+fmt.Sprint(i)+"]", r.ImplementedBy)
	}
}

// checkRules 校验类型级 rules：id/text 非空、id 唯一、implemented_by 有实际输出。
func (c *Checker) checkRules(schema *Schema) {
	seen := map[string]bool{}
	for i, r := range schema.Rules {
		if r.ID == "" || r.Text == "" {
			c.rulef("schema.rules", "%s: rules[%d] must have id and text", schema.ID, i)
		}
		if seen[r.ID] {
			c.rulef("schema.rules", "%s: duplicate rule id %q", schema.ID, r.ID)
		}
		seen[r.ID] = true
		c.checkImplementedBy(r.ID, fmt.Sprintf("%s: rules[%d]", schema.ID, i), r.ImplementedBy)
	}
}

// loadSchemaByType 按 type 从 contracts/_meta registry 加载并做工具侧自检。
func (c *Checker) loadSchemaByType(typ string) (*Schema, error) {
	s, path, err := contractspec.LoadSchemaByType(c.root, typ)
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

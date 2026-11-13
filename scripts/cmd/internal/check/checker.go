// Package check 实现契约校验：结构、ID、基类、列/约束/索引、引用、used_by、index 与 stability。
//
// 当前覆盖：
//   - contracts 下文本字段（description/comment/summary/do_not 等）双引号强制
//   - data.tables / data.tables.base 的结构、ID/文件、基类 5 列、列/约束/索引、used_by、do_not 具体性
//   - data/enums.yaml 的结构、item ID、值/code/类型名唯一性、code int32 范围、stability 与 summary
//   - data.entities 的字段来源（column/computed）、指针/可空、字段校验、关系字段、used_by
//   - data.queries 的 ID/名称、tables/joins、params、returns、伪 SQL 子句、index_ref、写归属与敏感列
//   - data.storage 的表级物理事实（engine/size_class/retention/partition）
//   - 表列引用的 data.tables.<t>.col.<c> 是否存在；表列不引用 data.enums.*
//   - 任意契约中的 data.enums.* 类型引用必须存在
//   - status 由基类提供且为 integer；物理表不得重声明；枚举只在 entity
//   - 禁止 foreign_key；unique 约束与唯一索引的软删语义区分
//   - owner_module 写归属（capabilities.persistence / data.queries 写查询）
//   - entity/query/capability 的表引用是否存在；每张物理表至少一个 entity
//
// 不覆盖（后续扩展）：
//   - graph.json / 越层引用
//   - generated diff
//   - SQL 语义（交给 sqlc / EXPLAIN）
package check

import (
	"fmt"
	"os"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

type Schema = contractspec.Schema
type Targets = contractspec.Targets
type ColumnTypes = contractspec.ColumnTypes
type Rule = contractspec.Rule
type ValueTypeFamily = contractspec.ValueTypeFamily
type Field = contractspec.Field

var baseColumns = []string{"id", "status", "created_at", "updated_at", "deleted_at"}

type Checker struct {
	root            string
	errs            []string
	tableOwners     map[string]string
	baseID          string
	inheritedCols   map[string]map[string]bool
	declaredRuleIDs map[string]bool
	queryIDs        map[string]string
	queryTables     map[string]map[string]bool
	tableIndexes    map[string][]tableIndex
	tablePrimaryKey map[string][]string
	tableObjects    map[string]map[string]bool
	objectNames     map[string]string

	// 实体与查询解析索引。
	tableColumns      map[string]map[string]columnMeta
	tableDocs         map[string]map[string]any
	tableTypeFamilies map[string]bool
	redundancyGroups  map[string]*redundancyGroup
	entityDocs        []entityDoc
	entityBaseDoc     map[string]any
	entityBaseFields  []entityFieldInfo
	entities          map[string]*entityInfo
	entityOrder       []string
	queries           map[string]*queryInfo
	declaredWrites    map[string]bool
	queryDocs         []queryDoc
	queryDupIDs       map[string]bool
	queryOrder        []string
	errCodeSet        map[string]bool
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
	c.checkFieldPlacement()
	c.checkDoNotDuplicates()
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
	entitySchema, err := c.loadSchemaByType("data.entities")
	if err != nil {
		c.rulef("schema.load", "load entities schema: %v", err)
		return
	}
	querySchema, err := c.loadSchemaByType("data.queries")
	if err != nil {
		c.rulef("schema.load", "load queries schema: %v", err)
		return
	}

	c.checkSharedTypes(tableSchema)
	c.loadQueryIndex(querySchema)
	c.tableTypeFamilies = map[string]bool{}
	for name := range tableSchema.ColumnTypes.ValueTypeFamilies {
		c.tableTypeFamilies[name] = true
	}
	tableIDs, cols := c.checkTables(tableSchema)
	c.loadTableColumns(tableIDs)
	c.checkIndexLiveOnly(tableIDs)
	c.checkStorage(tableIDs)
	c.loadEntities(entitySchema)
	c.checkRedundancy(tableIDs)
	c.loadTableIndexes(tableIDs)
	c.checkQueryIndexRefs()
	enumValues := c.checkEnums(enumSchema)
	c.checkRules(tableSchema)
	c.checkRules(enumSchema)
	c.checkRules(entitySchema)
	c.checkRules(querySchema)
	c.checkUndeclaredRuleIDs()
	c.checkEnumRefs(tableIDs, cols, enumValues)
	c.checkCrossFileTableRefs(tableIDs, cols)
	c.checkEntities(entitySchema)
	c.checkQueries(querySchema)
	c.checkEntityUsedBy()
	c.checkUsedBy(tableIDs)
	c.checkIndex(tableIDs, enumSchema, entitySchema, querySchema)
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

// checkRules 校验类型级 rules：id/text 非空、id 唯一、implemented_by 有实际输出。
func (c *Checker) checkRules(schema *Schema) {
	c.checkRuleList(schema.Rules, schema.ID)
}

// checkRuleList 是 meta rules 的共用校验。
func (c *Checker) checkRuleList(rules []Rule, where string) {
	seen := map[string]bool{}
	for i, r := range rules {
		if r.ID == "" || r.Text == "" {
			c.rulef("schema.rules", "%s: rules[%d] must have id and text", where, i)
		}
		if seen[r.ID] {
			c.rulef("schema.rules", "%s: duplicate rule id %q", where, r.ID)
		}
		seen[r.ID] = true
		c.checkImplementedBy(r.ID, fmt.Sprintf("%s: rules[%d]", where, i), r.ImplementedBy)
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

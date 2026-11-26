// Package check 实现契约校验：结构、ID、引用、写归属与生成字段一致性。
//
// 覆盖 data.*、capabilities.*、features.* 的字段规范、索引登记与交叉引用；
// 不覆盖 OpenAPI 结构、SQL 语义与生成物 diff（分别由 gen_openapi、sqlc、make gen-check 负责）。
package check

import (
	"fmt"
	"os"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

type Schema = contractspec.Schema
type Targets = contractspec.Targets
type ValueTypes = contractspec.ValueTypes
type SchemaKind = contractspec.SchemaKind
type CustomValueType = contractspec.CustomValueType
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
	cacheEntries    map[string]storageCacheEntry
	archiveEntries  map[string]storageArchiveEntry
	tableObjects    map[string]map[string]bool
	objectNames     map[string]string
	tableTargets    contractspec.Targets
	entityTargets   contractspec.Targets
	queryTargets    contractspec.Targets

	// 实体与查询解析索引。
	tableColumns             map[string]map[string]columnMeta
	tableDocs                map[string]map[string]any
	tableTypeFamilies        map[string]bool
	entityDocs               []entityDoc
	entityBaseDoc            map[string]any
	entityBaseFields         []entityFieldInfo
	entities                 map[string]*entityInfo
	entityOrder              []string
	queries                  map[string]*queryInfo
	declaredWrites           map[string]bool
	queryDocs                []queryDoc
	queryDupIDs              map[string]bool
	queryOrder               []string
	enumValues               map[string]map[string]bool
	enumCodes                map[string]map[int]bool
	enumIDs                  map[string]bool
	errCodeSet               map[string]bool
	errorStatus              map[string]int
	errorBizCode             map[string]int
	dataAccessErrSet         map[string]bool
	permissionSet            map[string]bool
	methodReturns            map[string]map[string]returnType
	methodTypeDefs           map[string]map[string]any
	serviceQueries           map[string]map[string]bool
	serviceStepIDs           map[string]bool
	stepService              map[string]string
	validationRefs           map[string]map[string]bool
	constraintRefs           map[string]map[string]bool
	constraintIDs            map[string]bool
	serviceQueryClosure      map[string]map[string]bool
	serviceErrorClosure      map[string]map[string]bool
	pageParamTypes           map[string]map[string]string
	pageTypeFamilies         map[string]string
	serviceWrites            map[string]bool
	httpOps                  map[string]httpOpInfo
	ruleIDSet                map[string]bool
	ruleErrors               map[string][]string
	lifecycleStates          map[string]map[string]bool
	lifecycleActionStates    map[string]map[string]map[string]bool
	lifecycleSubjectTables   map[string]map[string]bool
	lifecycleSubjectEntities map[string]map[string]bool
	queryReturnTypes         map[string]map[string]returnType
	eventPayloads            map[string]map[string]eventField
	accessBindings           map[string]accessPolicy
	inlineSeq                int
	nodeKinds                map[string]contractspec.SchemaKind
	propertyKeys             []contractspec.Field
	valueTypes               contractspec.ValueTypes
	strictHTTP               bool
}

// Run 执行全部契约校验并返回错误列表。
func Run(root string) []string {
	c := &Checker{root: root, strictHTTP: true}
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
	c.checkModules()
	c.checkErrorFiles()
	c.checkProjectApps()
	c.checkPlatformContracts()
	c.checkRoles()
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
	storageSchema, err := c.loadSchemaByType("data.storage")
	if err != nil {
		c.rulef("schema.load", "load storage schema: %v", err)
		return
	}

	c.valueTypes = tableSchema.ValueTypes
	c.checkSharedTypes(tableSchema)
	c.tableTargets = tableSchema.Targets
	c.entityTargets = entitySchema.Targets
	c.queryTargets = querySchema.Targets
	c.loadQueryIndex(querySchema)
	c.tableTypeFamilies = map[string]bool{}
	for name := range tableSchema.ValueTypes.TypeFamilies {
		c.tableTypeFamilies[name] = true
	}
	for name := range tableSchema.ValueTypes.CustomTypes {
		c.tableTypeFamilies[name] = true
	}
	tableIDs, cols := c.checkTables(tableSchema)
	c.loadTableColumns(tableIDs)
	c.checkIndexLiveOnly(tableIDs)
	c.checkStorage(tableIDs, storageSchema)
	c.loadEntities(entitySchema)
	c.checkRedundancy(tableIDs)
	c.loadTableIndexes(tableIDs)
	c.checkQueryIndexRefs()
	enumValues := c.checkEnums(enumSchema)
	c.enumValues = enumValues
	c.enumIDs = map[string]bool{}
	for id := range enumValues {
		c.enumIDs[id] = true
	}
	c.checkEnumCodeLiterals()
	c.checkRules(tableSchema)
	c.checkRules(enumSchema)
	c.checkRules(entitySchema)
	c.checkRules(querySchema)
	c.checkRules(storageSchema)
	c.checkCapabilities()
	c.checkFeatureRoutes()
	c.checkQuality()
	c.checkAnalytics()
	c.checkVersioning()
	c.checkLogFields()
	c.checkMetrics()
	c.checkArchitecture()
	c.checkAuthPolicy()
	c.checkConfigEnv()
	c.checkWebDeps()
	c.checkFeaturePages()
	c.checkPageRouteBindings()
	c.checkScenarioContracts()
	c.checkUndeclaredRuleIDs()
	c.checkEnumRefs(tableIDs, cols, enumValues)
	c.checkCapabilityWrites(tableIDs)
	c.checkEntities(entitySchema)
	c.checkValidationRefs()
	c.checkConstraintRefs()
	c.checkQueries(querySchema)
	c.checkQueryStorageRefs()
	c.checkEntityUsedBy()
	c.checkUsedBy(tableIDs)
	c.checkQueryUsedBy()
	c.checkIndex(tableIDs, enumSchema, entitySchema, querySchema)
	c.checkReferences(tableIDs)
	c.checkTypeVocabulary()
}

// checkContractProseQuotes 强制 contracts 下文本字段使用双引号，
// 并顺带拒绝同一 mapping 的重复键，避免正则/外部工具按未加引号的字符串解析时出现歧义。
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
		c.checkDuplicateKeys(&doc, rel, "")
		// 生成物不遵循手写契约的双引号风格；重复键仍需拒绝。
		if !strings.HasPrefix(rel, "contracts/generated/") {
			c.checkProseQuotes(&doc, rel)
		}
	}
}

// checkDuplicateKeys 拒绝同一 mapping 内的重复键，避免 yaml 静默 last-wins。
func (c *Checker) checkDuplicateKeys(n *yaml.Node, rel, path string) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode:
		for _, child := range n.Content {
			c.checkDuplicateKeys(child, rel, path)
		}
	case yaml.SequenceNode:
		for i, child := range n.Content {
			c.checkDuplicateKeys(child, rel, fmt.Sprintf("%s[%d]", path, i))
		}
	case yaml.MappingNode:
		seen := map[string]bool{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key := n.Content[i]
			childPath := key.Value
			if path != "" {
				childPath = path + "." + key.Value
			}
			if keyID, ok := yamlKeyID(key); ok {
				if seen[keyID] {
					c.rulef("schema.duplicate-key", "%s: duplicate key %q at %s", rel, key.Value, childPath)
				}
				seen[keyID] = true
			}
			c.checkDuplicateKeys(n.Content[i+1], rel, childPath)
		}
	}
}

// yamlKeyID 区分 YAML 标量 tag，避免 200 与 "200" 被误判为同一键。
func yamlKeyID(n *yaml.Node) (string, bool) {
	if n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Tag + "\x00" + n.Value, true
}

var proseScalarFields = map[string]bool{
	"action": true, "caution": true, "comment": true,
	"consistency": true, "description": true, "display_name": true,
	"error": true, "error_copy_uniform": true, "explanation": true,
	"generates": true, "handling": true, "id_policy": true, "instead": true,
	"label": true, "message": true, "naming": true, "note": true,
	"on_failure": true, "on_success": true, "ordering": true, "rationale": true,
	"rate_limit_hint": true, "reason": true, "route_label": true,
	"semantics": true, "scope": true, "storage": true, "success": true,
	"summary": true, "text": true, "title": true, "ui": true,
	"when": true, "why": true,
}

var proseListFields = map[string]bool{
	"allowed_cases": true, "client_requirements": true, "common_mistakes": true,
	"definition": true, "do_not": true, "forbidden": true,
	"forbidden_usage": true, "global_notes": true, "non_breaking": true,
	"process": true, "scope": true,
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
	if err := validateSchemaMeta(s, path); err != nil {
		return nil, err
	}
	return s, nil
}

func loadSchema(path string) (*Schema, error) {
	s, err := contractspec.LoadSchema(path)
	if err != nil {
		return nil, err
	}
	if err := validateSchemaMeta(s, path); err != nil {
		return nil, err
	}
	return s, nil
}

// validateSchemaMeta 校验 _meta 字段规范自身，避免拼写/类型错误把硬约束降级为无效文本。
func validateSchemaMeta(s *Schema, path string) error {
	if err := validatePatterns(s.Fields); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := validateFieldTypes(s.Fields, s.ValueTypes, false); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := validateFieldMarkers(s.Fields); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := validateMetaFields(s.Fields, "fields"); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	m, ok := normalizeYAMLKeys(raw).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: top-level map required", path)
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

// tables.go 校验 data.tables：基类、列、约束、索引与物理表名。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// 基类是固定单例：路径/ID 写死，不通过 targets 配置。
const (
	baseContractID   = "data.tables.base"
	baseContractFile = "_base.yaml"
	baseContractPath = "contracts/data/tables/_base.yaml"
)

// tableDoc 保存物理表文档，供继承解析二次遍历。
type tableDoc struct {
	id  string
	rel string
	doc map[string]any
}

func (c *Checker) checkTables(schema *Schema) (map[string]string, map[string]map[string]bool) {
	c.checkTargets(schema)
	c.baseID = baseContractID
	dir := schema.Targets.Dir
	globPattern := schema.Targets.Glob
	if dir == "" || globPattern == "" {
		return map[string]string{}, map[string]map[string]bool{}
	}

	tableIDs := map[string]string{}
	cols := map[string]map[string]bool{}
	physicalNames := map[string]string{}
	c.tableOwners = map[string]string{}
	c.tableObjects = map[string]map[string]bool{}
	c.tableDocs = map[string]map[string]any{}
	c.tableRel = map[string]string{}
	c.objectNames = map[string]string{}
	c.inheritedCols = map[string]map[string]bool{}
	c.constraintIDs = map[string]bool{}

	// 基类固定单例：路径/ID 写死，不通过 targets 配置。
	if doc, err := loadYAML(filepath.Join(c.root, baseContractPath)); err != nil {
		c.rulef("base-file", "%s: missing base contract: %v", baseContractPath, err)
	} else {
		tableIDs[baseContractID] = baseContractPath
		c.checkBaseTable(doc, schema, baseContractPath)
		cols[baseContractID] = baseFullSet()
	}

	files, _ := filepath.Glob(filepath.Join(c.root, dir, globPattern))
	sort.Strings(files)
	if len(files) == 0 {
		c.rulef("schema.load", "no contract files match %s/%s", dir, globPattern)
	}
	var physical []tableDoc
	for _, file := range files {
		rel := relPath(c.root, file)
		name := filepath.Base(file)
		if name == baseContractFile {
			continue // 基类已按固定路径加载
		}
		c.checkTargetFileName(schema, name, rel)
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		id, _ := doc["id"].(string)
		if id == "" {
			c.rulef("schema.id-from-file", "%s: missing id", rel)
			continue
		}
		if prev, ok := tableIDs[id]; ok {
			c.rulef("schema.contract-id-unique", "%s: duplicate contract id %s (also %s)", rel, id, prev)
		}
		tableIDs[id] = rel
		c.checkFieldOrder(rel, schema.Fields, orderRules{top: "table-key-order", nested: "table-key-order"})
		physical = append(physical, tableDoc{id: id, rel: rel, doc: doc})
	}

	full, ancestors := c.checkInheritance(physical, extendsPattern(schema))
	c.inheritedCols = make(map[string]map[string]bool, len(physical))
	for _, td := range physical {
		c.inheritedCols[td.id] = ancestors[td.id]
		c.tableDocs[td.id] = td.doc
		c.tableRel[td.id] = td.rel
		c.checkPhysicalTable(td.doc, td.rel, schema)
		c.collectTableObjects(td.doc, td.rel, td.id)
		if owner, _ := td.doc["owner_module"].(string); owner != "" {
			c.tableOwners[td.id] = owner
		}
		phys := strings.TrimSuffix(filepath.Base(td.rel), filepath.Ext(td.rel))
		if table, _ := asMap(td.doc["table"]); table != nil {
			if n, ok := table["name"].(string); ok && n != "" {
				phys = n
			}
		}
		if prev, ok := physicalNames[phys]; ok {
			c.rulef("physical-name-unique", "%s: physical table %q already declared by %s", td.rel, phys, prev)
		} else {
			physicalNames[phys] = td.rel
		}
		cols[td.id] = full[td.id]
	}
	return tableIDs, cols
}

// checkInheritance 解析多父 extends，返回每张表的完整列名与祖先列名。
// 继承列按“列名 -> 声明表 ID”合并：同一来源（钻石继承）可重复，不同来源报 extends-conflict。
// 基类 5 列由 baseFullOrigins 统一提供，物理表不得重声明（no-base-columns）。
//
// 分两遍：先单独做父表/环检测（避免把截断结果当完整结果），
// 再用 cycle-safe 的可达集合计算列，保证环外下游表仍能得到完整祖先列。
func (c *Checker) checkInheritance(docs []tableDoc, pattern *regexp.Regexp) (map[string]map[string]bool, map[string]map[string]bool) {
	byID := make(map[string]tableDoc, len(docs))
	for _, td := range docs {
		byID[td.id] = td
	}

	// 第一遍：只校验父表存在性与环，不产出列。
	state := map[string]int{}
	var stack []string
	var walk func(td tableDoc)
	walk = func(td tableDoc) {
		if state[td.id] == 2 {
			return
		}
		if state[td.id] == 1 {
			c.rulef("extends-ref", "%s: extends cycle: %s", td.rel, cyclePath(stack, td.id))
			return
		}
		state[td.id] = 1
		stack = append(stack, td.id)
		for _, parent := range extendsList(td.doc) {
			if parent == baseContractID {
				continue
			}
			if !isTableRef(pattern, parent) {
				continue // 非法 ID 由 schema.pattern 报出
			}
			p, ok := byID[parent]
			if !ok {
				c.rulef("extends-ref", "%s: extends %q is not a data.tables contract", td.rel, parent)
				continue
			}
			walk(p)
		}
		state[td.id] = 2
		stack = stack[:len(stack)-1]
	}
	for _, td := range docs {
		walk(td)
	}

	// 第二遍：cycle-safe 计算完整列（含环内节点）。
	// fullOf 返回某张表除 status 外的完整列来源；exclude 用于剔除调用方自己的声明，
	// 这样环内父表不会把调用方的列当作“祖先列”带回来。
	fullOf := func(id, exclude string) map[string]string {
		out := baseFullOrigins()
		visited := map[string]bool{}
		var collect func(string)
		collect = func(id string) {
			if visited[id] {
				return
			}
			visited[id] = true
			if id == baseContractID {
				return
			}
			td, ok := byID[id]
			if !ok {
				return
			}
			for _, parent := range extendsList(td.doc) {
				if parent == baseContractID || !isTableRef(pattern, parent) {
					continue
				}
				collect(parent)
			}
			if id == exclude {
				return // 只遍历其父链，不收调用方自己的列
			}
			for _, raw := range asList(td.doc["columns"]) {
				m, ok := asMap(raw)
				if !ok {
					continue
				}
				name, _ := m["name"].(string)
				if name == "" {
					continue
				}
				if _, exists := out[name]; !exists {
					out[name] = id
				}
			}
		}
		collect(id)
		return out
	}

	fullNames := map[string]map[string]bool{}
	inheritedNames := map[string]map[string]bool{}
	for _, td := range docs {
		merged := baseFullOrigins()
		for _, parent := range extendsList(td.doc) {
			if parent == baseContractID || !isTableRef(pattern, parent) {
				continue
			}
			if _, ok := byID[parent]; !ok {
				continue
			}
			for name, origin := range fullOf(parent, td.id) {
				if prev, ok := merged[name]; ok {
					if prev != origin {
						c.rulef("extends-conflict", "%s: column %q inherited from both %s and %s", td.rel, name, prev, origin)
					}
					continue
				}
				merged[name] = origin
			}
		}
		inheritedNames[td.id] = namesOf(merged)

		full := copyOrigins(merged)
		for _, raw := range asList(td.doc["columns"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			if _, inherited := merged[name]; !inherited {
				merged[name] = td.id
				full[name] = td.id
			}
		}
		fullNames[td.id] = namesOf(full)
	}
	return fullNames, inheritedNames
}

// cyclePath 返回从首次出现到当前节点的环路径：A -> B -> A。
func cyclePath(stack []string, id string) string {
	start := 0
	for i, name := range stack {
		if name == id {
			start = i
			break
		}
	}
	path := append(append([]string{}, stack[start:]...), id)
	return strings.Join(path, " -> ")
}

// extendsList 提取 table.extends 列表中的非空父表 ID；类型错误由 schema 校验。
func extendsList(doc map[string]any) []string {
	table, _ := asMap(doc["table"])
	var out []string
	for _, raw := range asList(table["extends"]) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// extendsPattern 从字段规范读取 table.extends 的 pattern，避免 checker 与 meta 各写一份正则。
func extendsPattern(schema *Schema) *regexp.Regexp {
	table, ok := contractspec.FindField(schema.Fields, "table")
	if !ok {
		return nil
	}
	for _, f := range table.Fields {
		if f.Name != "extends" || f.Pattern == "" {
			continue
		}
		re, err := regexp.Compile(f.Pattern)
		if err == nil {
			return re
		}
	}
	return nil
}

func isTableRef(pattern *regexp.Regexp, id string) bool {
	return pattern == nil || pattern.MatchString(id)
}

// baseFullOrigins 返回基类 5 列的来源集合。
func baseFullOrigins() map[string]string {
	set := make(map[string]string, len(baseColumns))
	for _, name := range baseColumns {
		set[name] = baseContractID
	}
	return set
}

func namesOf(origins map[string]string) map[string]bool {
	set := make(map[string]bool, len(origins))
	for name := range origins {
		set[name] = true
	}
	return set
}

func copyOrigins(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// baseFullSet 返回基类自身的 5 列。
func baseFullSet() map[string]bool {
	set := make(map[string]bool, len(baseColumns))
	for _, name := range baseColumns {
		set[name] = true
	}
	return set
}

func copySet(src map[string]bool) map[string]bool {
	dst := make(map[string]bool, len(src))
	for k := range src {
		dst[k] = true
	}
	return dst
}

// collectTableObjects 记录表上可被 capabilities 引用的约束/索引名；
// 索引名同时校验全局唯一，check 约束不占用 PG 索引命名空间。
func (c *Checker) collectTableObjects(doc map[string]any, rel, tableID string) {
	objects := map[string]bool{}
	for _, raw := range asList(doc["constraints"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		objects[name] = true
	}
	for _, raw := range asList(doc["indexes"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		objects[name] = true
		c.recordIndexName(name, rel)
	}
	c.tableObjects[tableID] = objects
}

// recordIndexName 校验索引名全局唯一（PG 索引命名空间）。
// 同一文件内的重复索引名已由 unique-names 报出，这里不重复报。
func (c *Checker) recordIndexName(name, rel string) {
	prevRel, seen := c.objectNames[name]
	if !seen {
		c.objectNames[name] = rel
		return
	}
	if prevRel == rel {
		return
	}
	c.rulef("index-name-unique", "%s: index %q already declared in %s", rel, name, prevRel)
}

// checkPhysicalTable 先按 schema 校验结构，再跑表级规则。
func (c *Checker) checkPhysicalTable(doc map[string]any, rel string, schema *Schema) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkTableRules(doc, rel, schema)
}

// baseColumnShape 固定基类 5 列的物理形状；基类漂移会改变所有表的 DDL。
var baseColumnShape = map[string]struct {
	typ      string
	variant  string
	primary  bool
	nullable bool
}{
	"id":         {"integer", "64", true, false},
	"status":     {"integer", "32", false, false},
	"created_at": {"timestamp", "long", false, false},
	"updated_at": {"timestamp", "long", false, false},
	"deleted_at": {"timestamp", "long", false, true},
}

func (c *Checker) checkBaseTable(doc map[string]any, schema *Schema, rel string) {
	c.checkFieldOrder(rel, schema.Fields, orderRules{top: "table-key-order", nested: "table-key-order"})
	if filepath.Base(rel) != baseContractFile {
		c.rulef("base-file", "%s: base contract must live in %s", rel, baseContractFile)
	}
	allowed := map[string]bool{
		"schema_version": true, "id": true, "summary": true,
		"stability": true, "do_not": true, "columns": true,
	}
	fields := filterFields(schema.Fields, allowed)
	c.validateDoc(doc, fields, rel)
	c.checkStability(doc, rel)
	if id, ok := doc["id"].(string); ok && id != baseContractID {
		c.rulef("base-file", "%s: base table id must be %s, got %s", rel, baseContractID, id)
	}
	names := []string{}
	for i, raw := range asList(doc["columns"]) {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: columns must be objects", rel)
			continue
		}
		n, ok := m["name"].(string)
		if !ok {
			continue
		}
		if _, ok := m["redundancy"]; ok {
			c.rulef("redundant-group", "%s: base column %q must not declare redundancy", rel, n)
		}
		names = append(names, n)
		if shape, ok := baseColumnShape[n]; ok {
			typ, _ := m["type"].(string)
			pk, _ := m["primary_key"].(bool)
			nullable, _ := m["nullable"].(bool)
			family, variant, isBasics := contractspec.SplitBasicsRef(typ)
			if !isBasics || family != shape.typ || variant != shape.variant {
				c.rulef("base-columns", "%s: base column %q must be _types.basics.%s.%s, got %q", rel, n, shape.typ, shape.variant, typ)
			}
			if pk != shape.primary {
				c.rulef("base-columns", "%s: base column %q primary_key must be %v", rel, n, shape.primary)
			}
			if nullable != shape.nullable {
				c.rulef("base-columns", "%s: base column %q nullable must be %v", rel, n, shape.nullable)
			}
		}
		if n == "status" {
			if d, ok := m["default"].(int); !ok || d != 0 {
				c.rulef("base-columns", "%s: base status default must be 0", rel)
			}
		}
		c.checkDefaultExclusive(m, rel, i, n)
	}
	want := append([]string(nil), baseColumns...)
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		c.rulef("base-columns", "%s: base columns must be %v, got %v", rel, want, names)
	}
}

func (c *Checker) checkTableRules(doc map[string]any, rel string, schema *Schema) {
	id, _ := doc["id"].(string)
	c.checkStability(doc, rel)
	want := schema.Targets.FileID(filepath.Base(rel))
	if want == "" {
		c.rulef("schema.targets", "%s: cannot derive contract id from targets.id", rel)
	} else if id != want {
		c.rulef("schema.id-from-file", "%s: id must be %s, got %s (file name is the source)", rel, want, id)
	}
	// table.extends 的值域由 fields.table.fields.extends.pattern + unique 保证；
	// 这里只补字段树无法表达的空列表约束，避免同一错误重复报两次。
	table, _ := asMap(doc["table"])
	if table != nil {
		if list, isList := table["extends"].([]any); isList && len(list) == 0 {
			c.rulef("base-only", "%s: table.extends must not be empty", rel)
		}
	}

	columns := asList(doc["columns"])
	if _, present := doc["columns"]; present && len(columns) == 0 {
		c.rulef("schema.non-empty", "%s: columns must not be empty", rel)
	}
	inherited := copySet(c.inheritedCols[id])
	// 基类 4 列始终是继承列；即使 extends 非法，也按基类语义报重声明。
	for name := range baseFullSet() {
		inherited[name] = true
	}
	colNames := map[string]bool{}
	allCols := copySet(inherited)
	for i, raw := range columns {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: columns[%d] must be an object", rel, i)
			continue
		}
		name, _ := m["name"].(string)
		if colNames[name] {
			c.rulef("schema.unique-names", "%s: columns[%d]: duplicate column %q", rel, i, name)
		}
		colNames[name] = true
		allCols[name] = true
		if inherited[name] {
			c.rulef("no-base-columns", "%s: columns[%d]: inherited column %q must not be redeclared", rel, i, name)
		}
		if pk, _ := m["primary_key"].(bool); pk {
			c.rulef("physical-primary-key", "%s: columns[%d] (%s): physical tables must not declare primary_key; base id is the primary key", rel, i, name)
		}
		c.checkDefaultExclusive(m, rel, i, name)
		if t, _ := m["type"].(string); !isColumnType(t, schema.ValueTypes) {
			c.rulef("column-type", "%s: columns[%d] (%s): unsupported type %q (type_families/custom_types %v or data.tables.<t>.col.<c>)", rel, i, name, t, valueTypeNames(schema.ValueTypes))
		} else {
			family, _, isBasics := contractspec.SplitBasicsRef(t)
			if !isBasics {
				family = t
			}
			c.checkIntegerAutoIncrement(m, t, rel, i, name)
			c.checkFamilyParams(m, family, rel, i, name, schema)
		}
	}
	constraintNames := map[string]bool{}
	for i, raw := range asList(doc["constraints"]) {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: constraints[%d] must be an object", rel, i)
			continue
		}
		name, _ := m["name"].(string)
		if !constraintNames[name] {
			constraintNames[name] = true
		} else {
			c.rulef("schema.unique-names", "%s: constraints[%d]: duplicate constraint %q", rel, i, name)
		}
		if code, _ := m["on_violation"].(string); code != "" {
			if errs := c.errorCodes(); len(errs) > 0 && !c.errorCodeDeclared(code) {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): unknown error code %s", rel, i, name, code)
			}
		}
		if c.constraintIDs == nil {
			c.constraintIDs = map[string]bool{}
		}
		c.constraintIDs[id+"."+name] = true
		c.checkConstraintUsedBy(m, id, rel, i, name)
	}

	indexNames := map[string]bool{}
	for i, raw := range asList(doc["indexes"]) {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: indexes[%d] must be an object", rel, i)
			continue
		}
		name, _ := m["name"].(string)
		if !indexNames[name] {
			indexNames[name] = true
		} else {
			c.rulef("schema.unique-names", "%s: indexes[%d]: duplicate index %q", rel, i, name)
		}
		c.checkIndexUsedBy(m, want, rel, i, name)
		where := fmt.Sprintf("%s: indexes[%d] (%s)", rel, i, name)
		for _, x := range asList(m["columns"]) {
			col, ok := x.(string)
			if !ok {
				c.rulef("schema.type", "%s: column name must be string", where)
				continue
			}
			if !allCols[col] {
				c.rulef("index-definition", "%s: unknown column %q", where, col)
			}
		}
		unique, _ := m["unique"].(bool)
		if unique {
			if !strings.HasPrefix(name, "uq_") {
				c.rulef("index-definition", "%s: indexes[%d] (%s): unique name must start with uq_", rel, i, name)
			}
		} else if !strings.HasPrefix(name, "idx_") {
			c.rulef("index-definition", "%s: indexes[%d] (%s): non-unique name must start with idx_", rel, i, name)
		}
	}
	c.checkDoNotSpecificity(doc, rel, strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)), table, allCols, constraintNames, indexNames)
}

// checkConstraintUsedBy 对账 constraints[].used_by：它由 gen-graph 从
// query.constraints[] 反向生成，这里只校验声明值自洽且与生成集合一致。
func (c *Checker) checkConstraintUsedBy(m map[string]any, tableID, rel string, i int, name string) {
	raw, present := m["used_by"]
	declared := stringSet(asList(raw))
	if !present || len(declared) == 0 {
		return // 缺失/空列表由 schema.required 与 schema.generated-range 报出
	}
	bad := false
	for qid := range declared {
		if !strings.HasPrefix(qid, "data.queries.") {
			c.rulef("constraint-used-by", "%s: constraints[%d] (%s): used_by must be data.queries.* item IDs, got %s", rel, i, name, qid)
			bad = true
			continue
		}
		q := c.queries[qid]
		if q == nil {
			c.rulef("constraint-used-by", "%s: constraints[%d] (%s): unknown query %s", rel, i, name, qid)
			bad = true
			continue
		}
		if !containsString(q.Writes, tableID) {
			c.rulef("constraint-used-by", "%s: constraints[%d] (%s): query %s does not write table %s", rel, i, name, qid, tableID)
			bad = true
		}
	}
	if bad {
		return
	}
	want := c.constraintRefs[tableID+"."+name]
	if !sameSet(declared, want) {
		c.rulef("constraint-used-by", "%s: constraints[%d] (%s): used_by must be %v, got %v", rel, i, name, sortedKeys(want), sortedKeys(declared))
	}
}

// checkConstraintRefs 校验 query.constraints[] 指向写表上的已声明 ck_ 约束。
func (c *Checker) checkConstraintRefs() {
	for cid, queries := range c.constraintRefs {
		idx := strings.Index(cid, ".ck_")
		if idx < 0 || !strings.HasPrefix(cid, "data.tables.") {
			c.rulef("constraint-ref", "malformed constraint ID %s", cid)
			continue
		}
		tableID := cid[:idx]
		if !c.constraintIDs[cid] {
			c.rulef("constraint-ref", "%s: unknown constraint", cid)
			continue
		}
		for qid := range queries {
			q := c.queries[qid]
			if q == nil {
				c.rulef("constraint-ref", "%s: unknown query %s", cid, qid)
				continue
			}
			if !containsString(q.Writes, tableID) {
				c.rulef("constraint-ref", "%s: query %s does not write table %s", cid, qid, tableID)
			}
		}
	}
}

// checkIndexUsedBy 校验索引的 used_by：每个查询项存在且引用当前表。
// 非空由 schema.required/min_items 保证；这里只做引用对账。
func (c *Checker) checkIndexUsedBy(m map[string]any, tableID, rel string, i int, name string) {
	raw, present := m["used_by"]
	if !present || raw == nil {
		return // 缺失由 schema.required 报出
	}
	for _, raw := range asList(raw) {
		qid, ok := raw.(string)
		if !ok || qid == "" || !strings.HasPrefix(qid, "data.queries.") {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): used_by must be data.queries.* item IDs, got %v", rel, i, name, raw)
			continue
		}
		if _, ok := c.queryIDs[qid]; !ok {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): unknown query %s", rel, i, name, qid)
			continue
		}
		if !c.queryTables[qid][tableID] {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): query %s does not reference table %s", rel, i, name, qid, tableID)
			continue
		}
	}
}

// checkIndexLiveOnly 在列元数据加载后校验 live_only 只用于唯一索引，且依赖 nullable deleted_at。
func (c *Checker) checkIndexLiveOnly(tableIDs map[string]string) {
	for id, rel := range tableIDs {
		if id == c.baseID {
			continue
		}
		doc := c.tableDocs[id]
		for i, raw := range asList(doc["indexes"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			liveOnly, _ := m["live_only"].(bool)
			if !liveOnly {
				continue
			}
			name, _ := m["name"].(string)
			unique, _ := m["unique"].(bool)
			if !unique {
				c.rulef("index-live-only", "%s: indexes[%d] (%s): live_only is only allowed on unique indexes", rel, i, name)
			}
			meta, ok := c.tableColumns[id]["deleted_at"]
			if !ok || !meta.nullable {
				c.rulef("index-live-only", "%s: indexes[%d] (%s): live_only requires nullable deleted_at", rel, i, name)
			}
		}
	}
}

// checkDoNotSpecificity 落实 contracts/README §4.5.1：
// 每条 do_not 必须含本表的具体标识，通用规则应上移组 README 或 _base。
func (c *Checker) checkDoNotSpecificity(doc map[string]any, rel, stem string, table map[string]any, allCols, constraintNames, indexNames map[string]bool) {
	tokens := map[string]bool{}
	add := func(s string) {
		if utf8.RuneCountInString(s) >= 3 {
			tokens[s] = true
		}
	}
	add(stem)
	add("data.tables." + stem)
	if table != nil {
		if n, ok := table["name"].(string); ok {
			add(n)
		}
	}
	for n := range allCols {
		if !isBaseColumn(n) {
			add(n)
		}
	}
	for n := range constraintNames {
		add(n)
	}
	for n := range indexNames {
		add(n)
	}
	for _, v := range doNotViolations(asList(doc["do_not"]), tokens) {
		if v.Empty {
			c.rulef("do-not-specific", "%s: do_not[%d] must not be empty", rel, v.Index)
			continue
		}
		c.rulef("do-not-specific", "%s: do_not[%d] must mention a specific identifier of table %q (file stem, physical name, column, constraint or index): %s", rel, v.Index, stem, v.Text)
	}
}

// checkDefaultExclusive 校验列的 default 与 default_sql 互斥。
func (c *Checker) checkDefaultExclusive(m map[string]any, rel string, index int, name string) {
	if auto, _ := m["auto_increment"].(bool); auto {
		if _, ok := m["default"]; ok {
			c.rulef("column-type-supported", "%s: columns[%d] (%s): auto_increment must not declare default", rel, index, name)
		}
		if _, ok := m["default_sql"]; ok {
			c.rulef("column-type-supported", "%s: columns[%d] (%s): auto_increment must not declare default_sql", rel, index, name)
		}
		if nullable, _ := m["nullable"].(bool); nullable {
			c.rulef("column-type-supported", "%s: columns[%d] (%s): auto_increment requires nullable:false", rel, index, name)
		}
	}
	if _, ok := m["default"]; !ok {
		return
	}
	if _, ok := m["default_sql"]; ok {
		c.rulef("default-vs-default-sql", "%s: columns[%d] (%s): default and default_sql are mutually exclusive", rel, index, name)
	}
}

func isBaseColumn(name string) bool {
	for _, n := range baseColumns {
		if n == name {
			return true
		}
	}
	return false
}

// commonColumnFields 从 schema 的 columns.item_fields 派生通用列字段。
// 类型族扩展字段由 type_families.params 注入，必须排除，避免把
// “只属于某个族”的字段误认为所有列都可写。
func commonColumnFields(schema *Schema) map[string]bool {
	cols, ok := contractspec.FindField(schema.Fields, "columns")
	if !ok {
		return nil
	}
	familyParams := map[string]bool{}
	for _, fam := range schema.ValueTypes.TypeFamilies {
		for k := range fam.Params {
			familyParams[k] = true
		}
	}
	out := map[string]bool{}
	for _, f := range cols.ItemFields {
		if !familyParams[f.Name] {
			out[f.Name] = true
		}
	}
	return out
}

// checkSharedTypes 校验类型族与项目自定义类型：族在 _meta/_types.yaml，
// 自定义类型在 contracts/data/types.yaml；未使用的族也必须可解析，
// 避免拼错 type 后只在某列使用时才暴露。
func (c *Checker) checkSharedTypes(schema *Schema) {
	if len(schema.ValueTypes.TypeFamilies) == 0 {
		c.rulef("schema.types", "types: type_families must not be empty")
		return
	}
	for _, name := range sortedKeysOfFields(schema.ValueTypes.BasicsParams) {
		c.checkParamSpec("types.commons.basics_params", name, schema.ValueTypes.BasicsParams[name])
	}
	for _, name := range familyNames(schema.ValueTypes) {
		fam := schema.ValueTypes.TypeFamilies[name]
		for _, pname := range sortedKeysOfFields(fam.Params) {
			c.checkParamSpec("types.type_families."+name+".params", pname, fam.Params[pname])
		}
	}
	for _, name := range structureNames(schema.ValueTypes) {
		st := schema.ValueTypes.Structures[name]
		for _, pname := range sortedKeysOfFields(st.Params) {
			c.checkParamSpec("types.structures."+name+".params", pname, st.Params[pname])
		}
	}
	customTypeNames := make([]string, 0, len(schema.ValueTypes.CustomTypes))
	for name := range schema.ValueTypes.CustomTypes {
		customTypeNames = append(customTypeNames, name)
	}
	sort.Strings(customTypeNames)
	for _, name := range customTypeNames {
		customType := schema.ValueTypes.CustomTypes[name]
		if _, ok := schema.ValueTypes.Structures[name]; ok {
			c.rulef("schema.types", "data.types.custom_types.%s: name collides with a structure", name)
		}
		if _, ok := schema.ValueTypes.Schemas[name]; ok {
			c.rulef("schema.types", "data.types.custom_types.%s: name collides with a schema kind", name)
		}
		family, _, ok := customType.Basics()
		if !ok {
			c.rulef("schema.types", "data.types.custom_types.%s: type must be _types.basics.<family>[.<variant>], got %q", name, customType.Type)
			continue
		}
		if _, ok := schema.ValueTypes.TypeFamilies[family]; !ok {
			c.rulef("schema.types", "data.types.custom_types.%s: unknown type family %s", name, family)
			continue
		}
		for pname := range customType.Params {
			if contractspec.TableOnlyBasicsParams[pname] {
				c.rulef("family-params", "data.types.custom_types.%s: param %q is table-only", name, pname)
			}
		}
		c.checkFamilyParams(customType.Params, family, "data.types.custom_types."+name, 0, name, schema)
	}
}

// checkParamSpec 校验一个共用的类型参数声明。
func (c *Checker) checkParamSpec(where, pname string, def Field) {
	if strings.TrimSpace(pname) == "" {
		c.rulef("schema.types", "%s: param name must not be empty", where)
	}
	def.Name = pname
	if err := validateFieldTypes([]Field{def}, c.valueTypes, true); err != nil {
		c.rulef("schema.types", "%s.%s: %v", where, pname, err)
	}
	if strings.TrimSpace(def.Description) == "" {
		c.rulef("schema.types", "%s.%s: description must not be empty", where, pname)
	}
	if def.Pattern != "" {
		if _, err := regexp.Compile(def.Pattern); err != nil {
			c.rulef("schema.types", "%s.%s: invalid pattern: %v", where, pname, err)
		}
	}
	if def.Default != nil {
		c.validateValue(def.Default, def, fmt.Sprintf("%s.%s.default", where, pname))
	}
}

func sortedKeysOfFields(fields map[string]Field) []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func familyNames(ct ValueTypes) []string {
	out := make([]string, 0, len(ct.TypeFamilies))
	for k := range ct.TypeFamilies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func structureNames(ct ValueTypes) []string {
	out := make([]string, 0, len(ct.Structures))
	for k := range ct.Structures {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func valueTypeNames(vt ValueTypes) []string {
	out := append(familyNames(vt), customTypeNames(vt)...)
	sort.Strings(out)
	return out
}

func customTypeNames(vt ValueTypes) []string {
	out := make([]string, 0, len(vt.CustomTypes))
	for k := range vt.CustomTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkColumnRefChains 校验列类型引用链最终落在具体值类型上；
// 断链或成环会让生成器无法确定落型，必须在 contract-check 阶段报出。
func (c *Checker) checkColumnRefChains() {
	tables := make([]string, 0, len(c.tableColumns))
	for table := range c.tableColumns {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		cols := c.tableColumns[table]
		names := make([]string, 0, len(cols))
		for name := range cols {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			meta := cols[name]
			typ := meta.typ
			if _, _, ok := contractspec.SplitColumnRef(typ); !ok {
				continue
			}
			seen := map[string]bool{}
			cycle, missing := false, false
			targetSensitivity := ""
			for {
				refTable, refCol, isRef := contractspec.SplitColumnRef(typ)
				if !isRef {
					break
				}
				if seen[typ] {
					cycle = true
					break
				}
				seen[typ] = true
				target, found := c.tableColumns[refTable][refCol]
				if !found {
					c.rulef("column-type", "%s.col.%s: column type ref target %s not found", table, name, typ)
					missing = true
					break
				}
				if sensitivityRank(target.sensitivity) > sensitivityRank(targetSensitivity) {
					targetSensitivity = target.sensitivity
				}
				typ = target.typ
			}
			if cycle {
				c.rulef("column-type", "%s.col.%s: column type ref %s forms a cycle", table, name, meta.typ)
				continue
			}
			if missing {
				continue
			}
			if family, _, ok := contractspec.SplitBasicsRef(typ); ok {
				typ = family
			}
			if typ == "any" {
				c.rulef("column-type-supported", "%s.col.%s: column type ref %s resolves to _types.basics.any", table, name, meta.typ)
			}
			if sensitivityRank(meta.sensitivity) < sensitivityRank(targetSensitivity) {
				c.rulef("column-type", "%s.col.%s: column type ref sensitivity %s must not be lower than target %s", table, name, normalizeSensitivity(meta.sensitivity), normalizeSensitivity(targetSensitivity))
			}
		}
	}
}

// checkIntegerAutoIncrement 校验 integer.8 没有对应的 serial 落型。
func (c *Checker) checkIntegerAutoIncrement(m map[string]any, t, rel string, i int, name string) {
	if auto, _ := m["auto_increment"].(bool); !auto {
		return
	}
	if _, variant, ok := contractspec.SplitBasicsRef(t); ok && variant == "8" {
		c.rulef("column-type-supported", "%s: columns[%d] (%s): integer.8 has no serial variant", rel, i, name)
	}
}

// checkFamilyParams 校验列上的类型族扩展字段：
//   - 该族不允许的扩展字段直接报错；
//   - required: true 的扩展字段必须出现；
//   - 出现时按族定义校验类型、枚举、范围；
//   - default 的 YAML 类型、长度、范围必须与族定义兼容。
func (c *Checker) checkFamilyParams(m map[string]any, t, rel string, i int, name string, schema *Schema) {
	common := commonColumnFields(schema)
	customType, isCustom := schema.ValueTypes.CustomTypes[t]
	family := t
	if isCustom {
		family, _, _ = customType.Basics()
	}
	switch family {
	case "any":
		c.rulef("column-type-supported", "%s: columns[%d] (%s): _types.basics.any has no deterministic DDL; use a concrete scalar", rel, i, name)
	}
	fam, ok := schema.ValueTypes.TypeFamilies[family]
	if !ok {
		if common != nil {
			for k := range m {
				if !common[k] {
					c.rulef("family-params", "%s: columns[%d] (%s): param %q not allowed for type %s", rel, i, name, k, t)
				}
			}
		}
		return
	}
	allowed := map[string]bool{}
	for k := range common {
		allowed[k] = true
	}
	if !isCustom {
		for k := range fam.Params {
			allowed[k] = true
		}
	}
	if common != nil {
		for k := range m {
			if !allowed[k] {
				c.rulef("family-params", "%s: columns[%d] (%s): param %q not allowed for type %s", rel, i, name, k, t)
			}
		}
	}
	for pk, def := range fam.Params {
		v, present := m[pk]
		if def.Required && !present && !isCustom {
			c.rulef("family-params", "%s: columns[%d] (%s): missing required param %q for type %s", rel, i, name, pk, t)
			continue
		}
		if present {
			c.validateValue(v, def, fmt.Sprintf("%s.columns[%d].%s", rel, i, pk))
			if pk == "pattern" {
				if s, ok := v.(string); ok {
					if _, err := regexp.Compile(s); err != nil {
						c.rulef("family-params", "%s: columns[%d] (%s): invalid pattern %q: %v", rel, i, name, s, err)
					}
				}
			}
		}
	}
	c.checkColumnDefaultType(m, family, rel, i, name)
	switch family {
	case "string", "bytes":
		if a, ok := toInt(m["min_len"]); ok {
			if b, ok2 := toInt(m["max_len"]); ok2 && a > b {
				c.rulef("family-length", "%s: columns[%d] (%s): min_len > max_len", rel, i, name)
			}
		}
		if fixed, ok := m["fixed"].(bool); ok && fixed {
			if _, ok := m["max_len"]; !ok {
				c.rulef("family-length", "%s: columns[%d] (%s): fixed requires max_len", rel, i, name)
			} else if minLen, ok := toInt(m["min_len"]); ok {
				if maxLen, ok2 := toInt(m["max_len"]); ok2 && minLen != maxLen {
					c.rulef("family-length", "%s: columns[%d] (%s): fixed requires min_len == max_len", rel, i, name)
				}
			}
		}
		if s, ok := m["default"].(string); ok {
			n := utf8.RuneCountInString(s)
			if family == "bytes" {
				n = len(s)
			}
			if minLen, ok := toInt(m["min_len"]); ok && n < minLen {
				c.rulef("default-type", "%s: columns[%d] (%s): default length %d < min_len %d", rel, i, name, n, minLen)
			}
			if maxLen, ok := toInt(m["max_len"]); ok && n > maxLen {
				c.rulef("default-type", "%s: columns[%d] (%s): default length %d > max_len %d", rel, i, name, n, maxLen)
			}
		}
	case "integer", "float", "decimal":
		if a, ok := toFloat(m["min"]); ok {
			if b, ok2 := toFloat(m["max"]); ok2 && a > b {
				c.rulef("family-range", "%s: columns[%d] (%s): min > max", rel, i, name)
			}
		}
		if family == "decimal" {
			if p, ok := toInt(m["precision"]); ok {
				if s, ok2 := toInt(m["scale"]); ok2 && s > p {
					c.rulef("family-decimal", "%s: columns[%d] (%s): scale > precision", rel, i, name)
				}
			}
		}
		if family == "integer" {
			if def, ok := toInt(m["default"]); ok {
				if min, ok := toInt(m["min"]); ok && def < min {
					c.rulef("family-range", "%s: columns[%d] (%s): default %d < min %d", rel, i, name, def, min)
				}
				if max, ok := toInt(m["max"]); ok && def > max {
					c.rulef("family-range", "%s: columns[%d] (%s): default %d > max %d", rel, i, name, def, max)
				}
			}
		} else if def, ok := toFloat(m["default"]); ok {
			if min, ok := toFloat(m["min"]); ok && def < min {
				c.rulef("family-range", "%s: columns[%d] (%s): default %v < min %v", rel, i, name, def, min)
			}
			if max, ok := toFloat(m["max"]); ok && def > max {
				c.rulef("family-range", "%s: columns[%d] (%s): default %v > max %v", rel, i, name, def, max)
			}
		}
	}
}

// checkColumnDefaultType 校验字面量 default 的 YAML 类型与列类型族兼容。
// 引用列的 default 在 loadTableColumns 之后由 checkColumnRefDefaultTypes 解析目标族再校验。
// defaultMatchesType 判断 default 是否匹配类型族；不匹配时返回期望的类型描述。
func defaultMatchesType(def any, t string) (bool, string) {
	switch t {
	case "string", "bytes":
		if _, ok := def.(string); !ok {
			return false, "string"
		}
	case "datetime", "timestamp":
		switch def.(type) {
		case string, time.Time:
		default:
			return false, "string/time"
		}
	case "boolean":
		if _, ok := def.(bool); !ok {
			return false, "boolean"
		}
	case "integer":
		if _, ok := toInt(def); !ok {
			return false, "integer"
		}
	case "float", "decimal":
		if _, ok := toFloat(def); !ok {
			return false, "number"
		}
	case "json":
		// JSON 文档允许任意 YAML 结构。
	}
	return true, ""
}

func (c *Checker) checkColumnDefaultType(m map[string]any, t, rel string, i int, name string) {
	def, ok := m["default"]
	if !ok || def == nil {
		return
	}
	if ok, want := defaultMatchesType(def, t); !ok {
		c.rulef("default-type", "%s: columns[%d] (%s): default must be %s for type %s", rel, i, name, want, t)
	}
}

// checkColumnRefDefaultTypes 在列引用可解析后校验引用列的 default 类型。
func (c *Checker) checkColumnRefDefaultTypes() {
	tables := make([]string, 0, len(c.tableDocs))
	for table := range c.tableDocs {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		doc := c.tableDocs[table]
		for i, raw := range asList(doc["columns"]) {
			m, _ := asMap(raw)
			typ, _ := m["type"].(string)
			if _, _, ok := contractspec.SplitColumnRef(typ); !ok {
				continue
			}
			def, ok := m["default"]
			if !ok || def == nil {
				continue
			}
			name, _ := m["name"].(string)
			if ok, want := defaultMatchesType(def, c.resolvedColumnType(typ)); !ok {
				rel := c.tableRel[table]
				if rel == "" {
					rel = table
				}
				c.rulef("default-type", "%s: columns[%d] (%s): default must be %s for type %s", rel, i, name, want, typ)
			}
		}
	}
}

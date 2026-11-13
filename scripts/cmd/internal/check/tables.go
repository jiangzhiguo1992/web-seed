// tables.go 校验 data.tables：基类、列、约束、索引与物理表名。
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

var targetNamingPatterns = map[string]*regexp.Regexp{
	"snake_case": regexp.MustCompile(`^[a-z][a-z0-9_]*$`),
}

// 基类是固定单例：路径/ID 写死，不通过 targets 配置。
const (
	baseContractID   = "data.tables.base"
	baseContractFile = "_base.yaml"
	baseContractPath = "contracts/data/tables/_base.yaml"
)

func (c *Checker) checkTargets(schema *Schema) {
	t := schema.Targets
	where := schema.ID
	if t.Dir == "" || t.Glob == "" {
		c.rulef("schema.targets", "%s: targets.dir/glob must not be empty", where)
	}
	if t.ID == "" || !strings.Contains(t.ID, "<file>") {
		c.rulef("schema.targets", "%s: targets.id must contain <file>", where)
	}
	if _, ok := targetNamingPatterns[t.Naming]; !ok {
		c.rulef("schema.targets", "%s: targets.naming must be snake_case", where)
	}
}

func (c *Checker) checkTargetFileName(schema *Schema, file, rel string) {
	re := targetNamingPatterns[schema.Targets.Naming]
	if re != nil && !re.MatchString(strings.TrimSuffix(file, filepath.Ext(file))) {
		c.rulef("schema.targets", "%s: file name %q does not match targets.naming %s", rel, file, schema.Targets.Naming)
	}
}

func (c *Checker) checkColumnRefs(schema *Schema) {
	for _, p := range schema.ColumnRefs.RequireCol {
		if p == "" || !containsString(schema.ColumnRefs.Prefixes, p) {
			c.rulef("schema.column-refs", "%s: column_refs.require_col %q must also appear in prefixes", schema.ID, p)
		}
	}
	for _, p := range schema.ColumnRefs.Prefixes {
		if p == "" || !strings.HasSuffix(p, ".") {
			c.rulef("schema.column-refs", "%s: column_refs.prefixes %q must end with '.'", schema.ID, p)
		}
	}
}

// tableDoc 保存物理表文档，供继承解析二次遍历。
type tableDoc struct {
	id  string
	rel string
	doc map[string]any
}

// tableKeyOrder 是表契约的固定顶层键顺序。
var tableKeyOrder = []string{
	"id", "summary", "owner_module", "schema_version", "stability",
	"do_not", "table", "columns", "constraints", "indexes", "used_by",
}

// checkTableKeyOrder 校验表契约顶层键顺序；基类文件不参与。
func (c *Checker) checkTableKeyOrder(rel string) {
	keys, err := loadYAMLKeys(filepath.Join(c.root, rel))
	if err != nil {
		return
	}
	pos := map[string]int{}
	for i, k := range tableKeyOrder {
		pos[k] = i
	}
	last, lastKey := -1, ""
	for _, k := range keys {
		i, ok := pos[k]
		if !ok {
			continue
		}
		if i < last {
			c.rulef("table-key-order", "%s: key %s must come before %s", rel, k, lastKey)
		}
		last, lastKey = i, k
	}
}

func (c *Checker) checkTables(schema *Schema) (map[string]string, map[string]map[string]bool) {
	c.checkTargets(schema)
	c.checkColumnRefs(schema)
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
	c.objectNames = map[string]string{}
	c.objectKinds = map[string]string{}
	c.inheritedCols = map[string]map[string]bool{}

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
		c.checkTableKeyOrder(rel)
		physical = append(physical, tableDoc{id: id, rel: rel, doc: doc})
	}

	full, ancestors := c.checkInheritance(physical, extendsPattern(schema))
	c.inheritedCols = make(map[string]map[string]bool, len(physical))
	for _, td := range physical {
		c.inheritedCols[td.id] = ancestors[td.id]
		c.tableDocs[td.id] = td.doc
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
// status 不参与继承，每张物理表必须重声明。
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

// collectTableObjects 记录表上可被 capabilities 引用的约束/索引名，
// 同时校验 PG 索引命名空间内的全局唯一性。
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
		switch typ, _ := m["type"].(string); typ {
		case "unique", "exclude":
			c.recordObjectName(name, rel, "constraint")
		}
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
		c.recordObjectName(name, rel, "index")
	}
	c.tableObjects[tableID] = objects
}

// recordObjectName 校验索引名与 unique/exclude 约束名的全局唯一性。
// 同一文件同一类对象的重复已由 unique-names 报出，这里不重复报。
func (c *Checker) recordObjectName(name, rel, kind string) {
	prevRel, seen := c.objectNames[name]
	if !seen {
		c.objectNames[name] = rel
		c.objectKinds[name] = kind
		return
	}
	if prevRel == rel && c.objectKinds[name] == kind {
		return
	}
	c.rulef("index-name-unique", "%s: %s %q shares the index namespace with %s %q in %s", rel, kind, name, c.objectKinds[name], name, prevRel)
}

// checkPhysicalTable 先按 schema 校验结构，再跑表级规则。
// foreign_key 已禁用：先从结构校验视图中剥离，避免同一错误被拆成
// unknown field / pattern / enum 多条；表级规则只报一条 [no-foreign-key]。
func (c *Checker) checkPhysicalTable(doc map[string]any, rel string, schema *Schema) {
	c.validateDoc(stripForeignKeys(doc), schema.Fields, rel)
	c.checkTableRules(doc, rel, schema)
}

// stripForeignKeys 返回去掉 foreign_key 约束的浅拷贝；没有 FK 时原样返回。
func stripForeignKeys(doc map[string]any) map[string]any {
	raw, ok := doc["constraints"]
	if !ok {
		return doc
	}
	list := asList(raw)
	kept := make([]any, 0, len(list))
	removed := false
	for _, item := range list {
		m, ok := asMap(item)
		if ok && m["type"] == "foreign_key" {
			removed = true
			continue
		}
		kept = append(kept, item)
	}
	if !removed {
		return doc
	}
	cp := make(map[string]any, len(doc))
	for k, v := range doc {
		cp[k] = v
	}
	cp["constraints"] = kept
	return cp
}

// baseColumnShape 固定基类 5 列的物理形状；基类漂移会改变所有表的 DDL。
var baseColumnShape = map[string]struct {
	typ      string
	primary  bool
	nullable bool
}{
	"id":         {"string", true, false},
	"status":     {"integer", false, false},
	"created_at": {"timestamp", false, false},
	"updated_at": {"timestamp", false, false},
	"deleted_at": {"timestamp", false, true},
}

func (c *Checker) checkBaseTable(doc map[string]any, schema *Schema, rel string) {
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
	if s, ok := doc["summary"].(string); ok && s == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
	if doc["id"] != baseContractID {
		c.rulef("base-file", "%s: base table id must be %s, got %v", rel, baseContractID, doc["id"])
	}
	names := []string{}
	for _, raw := range asList(doc["columns"]) {
		m, ok := asMap(raw)
		if !ok {
			c.rulef("schema.type", "%s: columns must be objects", rel)
			continue
		}
		n, ok := m["name"].(string)
		if !ok {
			continue
		}
		names = append(names, n)
		if shape, ok := baseColumnShape[n]; ok {
			typ, _ := m["type"].(string)
			pk, _ := m["primary_key"].(bool)
			nullable, _ := m["nullable"].(bool)
			if typ != shape.typ {
				c.rulef("base-columns", "%s: base column %q must be %s, got %q", rel, n, shape.typ, typ)
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
	}
	want := append([]string(nil), baseColumns...)
	sort.Strings(names)
	sort.Strings(want)
	if strings.Join(names, ",") != strings.Join(want, ",") {
		c.rulef("base-columns", "%s: base columns must be %v, got %v", rel, want, names)
	}
}

// constraintAllowedFields 声明每种约束允许出现的字段。
// 约束是平铺的 union，必须按 type 收窄，避免把 expr 写在 unique 上。
var constraintAllowedFields = map[string]map[string]bool{
	"check":   {"name": true, "type": true, "expr": true},
	"unique":  {"name": true, "type": true, "columns": true},
	"exclude": {"name": true, "type": true, "expr": true},
}

// constraintKnownFields 是 item_fields 的并集；完全未知的字段交给 validateDoc 报错，
// 这里只处理“字段存在但不适用于当前 type”的情况，避免重复报错。
var constraintKnownFields = map[string]bool{
	"name": true, "type": true, "expr": true, "columns": true,
}

func (c *Checker) checkTableRules(doc map[string]any, rel string, schema *Schema) {
	id, _ := doc["id"].(string)
	if s, ok := doc["summary"].(string); ok && s == "" {
		c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
	}
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
		if n, ok := table["name"].(string); ok && n == "" {
			c.rulef("schema.non-empty", "%s: table.name must not be empty", rel)
		}
		if _, ok := table["comment"]; ok && !hasNonEmptyString(table, "comment") {
			c.rulef("schema.non-empty", "%s: table.comment must not be empty", rel)
		}
		if list, isList := table["extends"].([]any); isList && len(list) == 0 {
			c.rulef("base-only", "%s: table.extends must not be empty", rel)
		}
	}

	columns := asList(doc["columns"])
	if len(columns) == 0 {
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
			if nullable, _ := m["nullable"].(bool); nullable {
				c.rulef("primary-key-nullable", "%s: columns[%d] (%s): primary_key column must not be nullable", rel, i, name)
			}
		}
		if _, ok := m["default"]; ok {
			if _, ok2 := m["default_sql"]; ok2 {
				c.rulef("default-vs-default-sql", "%s: columns[%d] (%s): default and default_sql are mutually exclusive", rel, i, name)
			}
		}
		if t, _ := m["type"].(string); !isColumnType(t, schema.ColumnTypes, schema.ColumnRefs) {
			c.rulef("column-type", "%s: columns[%d] (%s): unsupported type %q (families %v or column_refs.prefixes %v)", rel, i, name, t, familyNames(schema.ColumnTypes), schema.ColumnRefs.Prefixes)
		} else {
			c.checkFamilyParams(m, t, rel, i, name, schema)
		}
	}
	checkCols := func(rule, where string, list []any) {
		for _, x := range list {
			s, ok := x.(string)
			if !ok {
				c.rulef("schema.type", "%s: column name must be string", where)
				continue
			}
			if !allCols[s] {
				c.rulef(rule, "%s: unknown column %q", where, s)
			}
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
		typ, _ := m["type"].(string)
		if typ == "foreign_key" {
			c.rulef("no-foreign-key", "%s: constraints[%d] (%s): foreign_key is forbidden; use application/service rules", rel, i, name)
			continue
		}
		if !constraintNames[name] {
			constraintNames[name] = true
		} else {
			c.rulef("schema.unique-names", "%s: constraints[%d]: duplicate constraint %q", rel, i, name)
		}
		if allowed, known := constraintAllowedFields[typ]; known {
			for k := range m {
				if !constraintKnownFields[k] {
					continue
				}
				if !allowed[k] {
					c.rulef("constraint-fields", "%s: constraints[%d] (%s): field %q is not allowed for type %s", rel, i, name, k, typ)
				}
			}
		}
		switch typ {
		case "check":
			if !hasNonEmptyString(m, "expr") {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): check requires expr", rel, i, name)
			}
			if !strings.HasPrefix(name, "ck_") {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): check name must start with ck_", rel, i, name)
			}
		case "unique":
			cols := asList(m["columns"])
			if len(cols) == 0 {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): unique requires columns", rel, i, name)
			}
			checkCols("constraint-fields", fmt.Sprintf("%s: constraints[%d] (%s)", rel, i, name), cols)
			if !strings.HasPrefix(name, "uq_") {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): unique name must start with uq_", rel, i, name)
			}
		case "exclude":
			if !hasNonEmptyString(m, "expr") {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): exclude requires expr", rel, i, name)
			}
			if !strings.HasPrefix(name, "ex_") {
				c.rulef("constraint-fields", "%s: constraints[%d] (%s): exclude name must start with ex_", rel, i, name)
			}
		default:
			c.rulef("constraint-fields", "%s: constraints[%d] (%s): unknown type %q", rel, i, name, typ)
		}
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
		if _, ok := m["reason"]; ok && !hasNonEmptyString(m, "reason") {
			c.rulef("index-reason", "%s: indexes[%d] (%s): reason must not be empty", rel, i, name)
		}
		hasCols := len(asList(m["columns"])) > 0
		hasExpr := hasNonEmptyString(m, "expression")
		if hasCols == hasExpr {
			c.rulef("index-definition", "%s: indexes[%d] (%s): exactly one of columns/expression is required", rel, i, name)
		}
		if hasCols {
			checkCols("index-definition", fmt.Sprintf("%s: indexes[%d] (%s)", rel, i, name), asList(m["columns"]))
		}
		if order := asList(m["order"]); len(order) > 0 {
			if !hasCols {
				c.rulef("index-order", "%s: indexes[%d] (%s): order requires columns", rel, i, name)
			} else if len(order) != len(asList(m["columns"])) {
				c.rulef("index-order", "%s: indexes[%d] (%s): order length must match columns", rel, i, name)
			}
		}
		checkCols("index-definition", fmt.Sprintf("%s: indexes[%d] (%s).include", rel, i, name), asList(m["include"]))
		unique, _ := m["unique"].(bool)
		if unique {
			if !strings.HasPrefix(name, "uq_") {
				c.rulef("index-definition", "%s: indexes[%d] (%s): unique name must start with uq_", rel, i, name)
			}
			where, _ := m["where"].(string)
			if !strings.Contains(where, "deleted_at IS NULL") {
				c.rulef("unique-scope", "%s: indexes[%d] (%s): unique index must filter deleted_at IS NULL", rel, i, name)
			}
		} else if !strings.HasPrefix(name, "idx_") {
			c.rulef("index-definition", "%s: indexes[%d] (%s): non-unique name must start with idx_", rel, i, name)
		}
	}
	c.checkDoNotSpecificity(doc, rel, strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel)), table, allCols, constraintNames, indexNames)
}

// checkIndexUsedBy 校验索引的 used_by：至少一个查询项，且查询存在并引用当前表。
// 这是“防空索引”的最低可执行约束；不是数据库查询计划器，不判断索引是否真被命中。
func (c *Checker) checkIndexUsedBy(m map[string]any, tableID, rel string, i int, name string) {
	usedBy := asList(m["used_by"])
	if len(usedBy) == 0 {
		c.rulef("index-used-by", "%s: indexes[%d] (%s): used_by must list at least one query item", rel, i, name)
		return
	}
	seen := map[string]bool{}
	for _, raw := range usedBy {
		qid, ok := raw.(string)
		if !ok || qid == "" || !strings.HasPrefix(qid, "data.queries.") {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): used_by must be data.queries.* item IDs, got %v", rel, i, name, raw)
			continue
		}
		if seen[qid] {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): duplicate query %s", rel, i, name, qid)
			continue
		}
		seen[qid] = true
		canonical := c.resolveQueryID(qid)
		if _, ok := c.queryIDs[canonical]; !ok {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): unknown query %s", rel, i, name, qid)
			continue
		}
		if !c.queryTables[canonical][tableID] {
			c.rulef("index-used-by-ref", "%s: indexes[%d] (%s): query %s does not reference table %s", rel, i, name, qid, tableID)
			continue
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
	for i, raw := range asList(doc["do_not"]) {
		s, ok := raw.(string)
		if !ok {
			continue
		}
		found := false
		for tok := range tokens {
			if strings.Contains(s, tok) {
				found = true
				break
			}
		}
		if !found {
			c.rulef("do-not-specific", "%s: do_not[%d] must mention a specific identifier of table %q (file stem, physical name, column, constraint or index)", rel, i, stem)
		}
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

func (c *Checker) checkStability(doc map[string]any, rel string) {
	stability, _ := doc["stability"].(string)
	if stability == "stable" {
		b, err := os.ReadFile(filepath.Join(c.root, rel))
		if err == nil {
			for _, marker := range []string{"TODO", "FIXME"} {
				if strings.Contains(string(b), marker) {
					c.rulef("schema.stability", "%s: stable contract contains %s placeholder", rel, marker)
				}
			}
		}
	}
}

// commonColumnFields 从 schema 的 columns.item_fields 派生通用列字段。
// 类型族扩展字段由 families.params 注入，必须排除，避免把
// “只属于某个族”的字段误认为所有列都可写。
func commonColumnFields(schema *Schema) map[string]bool {
	cols, ok := contractspec.FindField(schema.Fields, "columns")
	if !ok {
		return nil
	}
	familyParams := map[string]bool{}
	for _, fam := range schema.ColumnTypes.ValueTypeFamilies {
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

func familyNames(ct ColumnTypes) []string {
	out := make([]string, 0, len(ct.ValueTypeFamilies))
	for k := range ct.ValueTypeFamilies {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// checkFamilyParams 校验列上的类型族扩展字段：
//   - 该族不允许的扩展字段直接报错；
//   - required: true 的扩展字段必须出现；
//   - 出现时按族定义校验类型、枚举、范围；
//   - default 的 YAML 类型、长度、范围必须与族定义兼容。
func (c *Checker) checkFamilyParams(m map[string]any, t, rel string, i int, name string, schema *Schema) {
	common := commonColumnFields(schema)
	fam, ok := schema.ColumnTypes.ValueTypeFamilies[t]
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
	for k := range fam.Params {
		allowed[k] = true
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
		if def.Required && !present {
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
	c.checkColumnDefaultType(m, t, rel, i, name)
	switch t {
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
			if t == "bytes" {
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
		if t == "decimal" {
			if p, ok := toInt(m["precision"]); ok {
				if s, ok2 := toInt(m["scale"]); ok2 && s > p {
					c.rulef("family-decimal", "%s: columns[%d] (%s): scale > precision", rel, i, name)
				}
			}
		}
		if t == "integer" {
			if unsigned, _ := m["unsigned"].(bool); unsigned {
				if v, ok := toInt(m["min"]); ok && v < 0 {
					c.rulef("family-range", "%s: columns[%d] (%s): unsigned integer min must be >= 0", rel, i, name)
				}
				if v, ok := toInt(m["default"]); ok && v < 0 {
					c.rulef("family-range", "%s: columns[%d] (%s): unsigned integer default must be >= 0", rel, i, name)
				}
			}
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
// 引用列（column_refs 前缀 + .col.）的具体类型暂不递归展开。
func (c *Checker) checkColumnDefaultType(m map[string]any, t, rel string, i int, name string) {
	def, ok := m["default"]
	if !ok || def == nil {
		return
	}
	switch t {
	case "string", "bytes":
		if _, ok := def.(string); !ok {
			c.rulef("default-type", "%s: columns[%d] (%s): default must be string for type %s", rel, i, name, t)
		}
	case "datetime", "timestamp":
		switch def.(type) {
		case string, time.Time:
		default:
			c.rulef("default-type", "%s: columns[%d] (%s): default must be string/time for type %s", rel, i, name, t)
		}
	case "boolean":
		if _, ok := def.(bool); !ok {
			c.rulef("default-type", "%s: columns[%d] (%s): default must be boolean for type %s", rel, i, name, t)
		}
	case "integer":
		if _, ok := toInt(def); !ok {
			c.rulef("default-type", "%s: columns[%d] (%s): default must be integer for type %s", rel, i, name, t)
		}
	case "float", "decimal":
		if _, ok := toFloat(def); !ok {
			c.rulef("default-type", "%s: columns[%d] (%s): default must be number for type %s", rel, i, name, t)
		}
	case "json":
		// JSON 文档允许任意 YAML 结构。
	}
}

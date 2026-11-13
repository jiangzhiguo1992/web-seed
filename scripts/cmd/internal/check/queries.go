// queries.go 校验 data.queries：接口契约（入参/结果/目标表/索引/读写归属）。
// SQL 如何执行不在契约里；checker 只验证可机械判定的事实。
package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

type queryParam struct {
	Name     string
	Type     string
	Column   string
	Nullable bool
}

type queryReturn struct {
	Raw      string
	IsList   bool
	Kind     string // table / entity / scalar / object / invalid
	TableID  string
	EntityID string
}

type queryOutputField struct {
	Name     string
	Type     string
	Nullable bool
}

type queryInfo struct {
	ID            string
	Name          string
	PreviousNames []string
	File          string
	OwnerModule   string
	Targets       []string
	Params        []queryParam
	Return        queryReturn
	ReturnColumns []string
	OutputFields  []queryOutputField
	IndexRef      []string
}

type queryDoc struct {
	rel    string
	fileID string
	doc    map[string]any
	keys   []string
}

var sqlScalarReturns = map[string]bool{
	"boolean": true, "integer": true, "string": true, "float": true,
	"decimal": true, "timestamp": true, "datetime": true, "json": true,
}

// tableSet 返回查询读写的表集合。
func (q *queryInfo) tableSet() map[string]bool {
	out := map[string]bool{}
	for _, t := range q.Targets {
		out[t] = true
	}
	return out
}

// loadQueryIndex 解析查询文件；结构错误在 checkQueries 中报。
func (c *Checker) loadQueryIndex(schema *Schema) {
	c.queries = map[string]*queryInfo{}
	c.queryAliases = map[string]string{}
	c.queryAliasDup = map[string]bool{}
	c.queryDocs = nil
	c.queryOrder = nil
	c.queryIDs = map[string]string{}
	c.queryTables = map[string]map[string]bool{}
	c.queryDupIDs = map[string]bool{}
	if schema.Targets.Dir == "" || schema.Targets.Glob == "" {
		return
	}
	files := glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
	sort.Strings(files)
	for _, file := range files {
		rel := relPath(c.root, file)
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		keys, _ := loadYAMLKeys(file)
		fileID := schema.Targets.FileID(filepath.Base(file))
		if fileID == "" {
			fileID = strings.TrimSuffix(rel, filepath.Ext(rel))
		}
		c.queryDocs = append(c.queryDocs, queryDoc{rel: rel, fileID: fileID, doc: doc, keys: keys})
		owner, _ := doc["owner_module"].(string)
		for _, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			q := parseQuery(m, rel, fileID, owner)
			if q.ID == "" {
				continue
			}
			if _, exists := c.queries[q.ID]; exists {
				c.queryDupIDs[q.ID] = true
				continue
			}
			c.queries[q.ID] = q
			c.queryOrder = append(c.queryOrder, q.ID)
			c.queryIDs[q.ID] = rel
			c.queryTables[q.ID] = q.tableSet()
			for _, prev := range q.PreviousNames {
				oldID := contractspec.QueryID(fileID, prev)
				if oldID == "" || oldID == q.ID {
					c.queryAliasDup[oldID] = true
					continue
				}
				if target, ok := c.queryAliases[oldID]; ok && target != q.ID {
					c.queryAliasDup[oldID] = true
					continue
				}
				c.queryAliases[oldID] = q.ID
			}
		}
	}
	for oldID := range c.queryAliases {
		if _, ok := c.queryIDs[oldID]; ok {
			c.queryAliasDup[oldID] = true
		}
	}
}

func parseQuery(m map[string]any, rel, fileID, owner string) *queryInfo {
	q := &queryInfo{File: rel, OwnerModule: owner}
	q.Name, _ = m["name"].(string)
	q.ID = contractspec.QueryID(fileID, q.Name)
	q.PreviousNames = stringList(m["previous_names"])
	q.Targets = stringList(m["targets"])
	q.IndexRef = stringList(m["index_ref"])
	for _, raw := range asList(m["params"]) {
		pm, ok := asMap(raw)
		if !ok {
			continue
		}
		p := queryParam{}
		p.Name, _ = pm["name"].(string)
		p.Type, _ = pm["type"].(string)
		p.Column, _ = pm["column"].(string)
		p.Nullable, _ = pm["nullable"].(bool)
		q.Params = append(q.Params, p)
	}
	rawReturn, _ := m["returns"].(string)
	q.Return = parseQueryReturn(rawReturn)
	q.ReturnColumns = stringList(m["return_columns"])
	for _, raw := range asList(m["output_fields"]) {
		om, ok := asMap(raw)
		if !ok {
			continue
		}
		f := queryOutputField{}
		f.Name, _ = om["name"].(string)
		f.Type, _ = om["type"].(string)
		f.Nullable, _ = om["nullable"].(bool)
		q.OutputFields = append(q.OutputFields, f)
	}
	return q
}

func parseQueryReturn(raw string) queryReturn {
	r := queryReturn{Raw: strings.TrimSpace(raw)}
	if r.Raw == "" {
		return r
	}
	base := r.Raw
	if strings.HasPrefix(base, "List<") && strings.HasSuffix(base, ">") {
		r.IsList = true
		base = strings.TrimSuffix(strings.TrimPrefix(base, "List<"), ">")
	}
	r.Raw = base
	switch {
	case strings.HasPrefix(base, "data.tables.") && strings.HasSuffix(base, ".row"):
		r.Kind = "table"
		r.TableID = strings.TrimSuffix(base, ".row")
	case strings.HasPrefix(base, "data.entities."):
		r.Kind = "entity"
		r.EntityID = base
	case base == "object":
		r.Kind = "object"
	case sqlScalarReturns[base] || base == "affected_rows":
		r.Kind = "scalar"
	default:
		r.Kind = "invalid"
	}
	return r
}

var queryKeyOrder = []string{
	"id", "summary", "owner_module", "schema_version", "stability", "do_not", "queries",
}

func (c *Checker) checkQueries(schema *Schema) {
	c.checkTargets(schema)
	for _, qd := range c.queryDocs {
		c.checkTargetFileName(schema, filepath.Base(qd.rel), qd.rel)
		c.validateDoc(qd.doc, schema.Fields, qd.rel)
		c.checkStability(qd.doc, qd.rel)
		if s, _ := qd.doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", qd.rel)
		}
		c.checkQueryKeyOrder(qd)
		id, _ := qd.doc["id"].(string)
		want := schema.Targets.FileID(filepath.Base(qd.rel))
		if want != "" && id != want {
			c.rulef("schema.id-from-file", "%s: id must be %s, got %s (file name is the source)", qd.rel, want, id)
		}
		c.checkQueryDoNot(qd)
	}
	for id := range c.queryDupIDs {
		c.rulef("query-name-unique", "duplicate query name/derived id %s", id)
	}
	for id := range c.queryAliasDup {
		c.rulef("query-name-unique", "query previous name collides on derived id %s", id)
	}
	names := map[string]string{}
	for _, id := range c.queryOrder {
		q := c.queries[id]
		c.checkQueryIdentity(q, names)
		c.checkQueryShape(q)
		c.checkQueryTable(q)
		c.checkQueryParamColumns(q)
		c.checkQueryParams(q)
		c.checkQueryReturns(q)
		c.checkQueryOutputFields(q)
		c.checkQueryReturnColumns(q)
	}
}

func (c *Checker) checkQueryKeyOrder(qd queryDoc) {
	pos := map[string]int{}
	for i, k := range queryKeyOrder {
		pos[k] = i
	}
	last, lastKey := -1, ""
	for _, k := range qd.keys {
		i, ok := pos[k]
		if !ok {
			continue
		}
		if i < last {
			c.rulef("query-key-order", "%s: key %s must come before %s", qd.rel, k, lastKey)
		}
		last, lastKey = i, k
	}
}

func (c *Checker) checkQueryDoNot(qd queryDoc) {
	short := strings.TrimSuffix(filepath.Base(qd.rel), filepath.Ext(qd.rel))
	names := map[string]bool{short: true, qd.fileID: true}
	if m, _ := qd.doc["owner_module"].(string); m != "" {
		names[m] = true
	}
	for _, raw := range asList(qd.doc["queries"]) {
		if m, ok := asMap(raw); ok {
			if name, _ := m["name"].(string); name != "" {
				names[name] = true
				names[contractspec.QueryID(qd.fileID, name)] = true
			}
			for _, prev := range stringList(m["previous_names"]) {
				names[prev] = true
				names[contractspec.QueryID(qd.fileID, prev)] = true
			}
		}
	}
	for _, raw := range asList(qd.doc["do_not"]) {
		s, _ := raw.(string)
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			c.rulef("query-do-not-specific", "%s: do_not entries must not be empty", qd.rel)
			continue
		}
		specific := false
		for name := range names {
			if strings.Contains(trimmed, name) {
				specific = true
				break
			}
		}
		if !specific {
			c.rulef("query-do-not-specific", "%s: do_not must mention the query set or one of its queries: %s", qd.rel, trimmed)
		}
	}
}

func (c *Checker) checkQueryIdentity(q *queryInfo, names map[string]string) {
	if q.Name == "" {
		return
	}
	if pascalCase(q.Name) != q.Name {
		c.rulef("query-name", "%s: query name must be PascalCase, got %s", q.File, q.Name)
	}
	if prev, ok := names[q.Name]; ok {
		c.rulef("query-name-unique", "%s: query name %s already used by %s", q.File, q.Name, prev)
	} else {
		names[q.Name] = q.ID
	}
	for _, prev := range q.PreviousNames {
		if prev == q.Name {
			c.rulef("query-name-unique", "%s: previous_names must not repeat current name %s", q.File, prev)
			continue
		}
		if owner, ok := names[prev]; ok {
			c.rulef("query-name-unique", "%s: previous name %s already used by %s", q.File, prev, owner)
			continue
		}
		names[prev] = q.ID
	}
}

// resolveQueryID 把 previous_names 派生的旧 ID 解析到当前 query 项 ID。
func (c *Checker) resolveQueryID(id string) string {
	if _, ok := c.queries[id]; ok {
		return id
	}
	if target, ok := c.queryAliases[id]; ok {
		return target
	}
	return id
}

func pascalCase(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// checkQueryShape 校验 returns 的接口形状；读写归属在 capabilities 层。
func (c *Checker) checkQueryShape(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if q.Return.Raw != "affected_rows" {
		return
	}
	if q.Return.IsList {
		c.rulef("query-shape", "%s: affected_rows must not be a list", where)
	}
	if len(q.ReturnColumns) > 0 || len(q.OutputFields) > 0 {
		c.rulef("query-shape", "%s: affected_rows must not declare return_columns/output_fields", where)
	}
}

func (c *Checker) checkQueryTable(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Targets) == 0 {
		c.rulef("query-table-shape", "%s: targets must not be empty", where)
		return
	}
	seen := map[string]bool{}
	for _, t := range q.Targets {
		if seen[t] {
			c.rulef("query-table-shape", "%s: duplicate target table %s", where, t)
		}
		seen[t] = true
		if t == c.baseID {
			c.rulef("query-table-shape", "%s: targets references data.tables.base", where)
			continue
		}
		if _, ok := c.tableColumns[t]; !ok {
			c.rulef("query-table-shape", "%s: target table %s not found", where, t)
		}
	}
}

// checkQueryParamColumns 校验 params[].column 的列存在性与类型族兼容。
func (c *Checker) checkQueryParamColumns(q *queryInfo) {
	if len(q.Targets) == 0 {
		return // 缺失 targets 由 query-table-shape 报出
	}
	for _, p := range q.Params {
		if p.Column == "" {
			continue
		}
		where := fmt.Sprintf("%s: query %s param %s", q.File, q.ID, p.Name)
		t, col, ok := splitColumnRef(p.Column)
		if !ok {
			c.rulef("query-param-column", "%s: malformed column %q", where, p.Column)
			continue
		}
		if !q.tableSet()[t] {
			c.rulef("query-param-column", "%s: column %s does not belong to query tables", where, p.Column)
			continue
		}
		meta, ok := c.tableColumns[t][col]
		if !ok {
			c.rulef("query-param-column", "%s: column %s not found", where, p.Column)
			continue
		}
		family, _, ok := paramFamily(p.Type)
		if !ok {
			c.rulef("query-param-column", "%s: type %q cannot bind a column", where, p.Type)
			continue
		}
		if !familyCompatible(family, c.resolvedColumnType(meta.typ)) {
			c.rulef("query-param-column", "%s: type %s is not compatible with column %s (%s)", where, p.Type, p.Column, c.resolvedColumnType(meta.typ))
		}
	}
}

// paramFamily 把参数类型映射到 column_types 类型族；数组返回元素类型族。
func paramFamily(typ string) (family string, array bool, ok bool) {
	base := strings.TrimSpace(typ)
	if strings.HasPrefix(base, "List<") && strings.HasSuffix(base, ">") {
		array = true
		base = strings.TrimSuffix(strings.TrimPrefix(base, "List<"), ">")
	}
	base = strings.ToLower(strings.TrimSpace(base))
	switch base {
	case "text", "varchar", "character varying", "char", "bpchar":
		return "string", array, true
	case "integer", "int", "int4", "int8", "bigint", "smallint":
		return "integer", array, true
	case "boolean", "bool":
		return "boolean", array, true
	case "timestamp", "timestamptz", "timestamp with time zone", "datetime":
		return "timestamp", array, true
	case "float", "float4", "float8", "double", "double precision", "real":
		return "float", array, true
	case "decimal", "numeric":
		return "decimal", array, true
	case "json", "jsonb":
		return "json", array, true
	case "bytea", "bytes":
		return "bytes", array, true
	}
	return "", array, false
}

func familyCompatible(param, column string) bool {
	if param == column {
		return true
	}
	return (param == "timestamp" && column == "datetime") || (param == "datetime" && column == "timestamp")
}

func (c *Checker) checkQueryParams(q *queryInfo) {
	seen := map[string]bool{}
	for _, p := range q.Params {
		if seen[p.Name] {
			c.rulef("query-params", "%s: query %s duplicate param %s", q.File, q.ID, p.Name)
		}
		seen[p.Name] = true
		if strings.TrimSpace(p.Type) == "" {
			c.rulef("query-params", "%s: query %s param %s type must not be empty", q.File, q.ID, p.Name)
		}
	}
}

func (c *Checker) checkQueryReturns(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	switch q.Return.Kind {
	case "invalid", "":
		c.rulef("query-returns", "%s: invalid returns %q", where, q.Return.Raw)
		return
	case "table":
		if _, ok := c.tableColumns[q.Return.TableID]; !ok {
			c.rulef("query-returns", "%s: returns unknown table %s", where, q.Return.TableID)
			return
		}
		if len(q.Targets) == 0 || q.Targets[0] != q.Return.TableID {
			c.rulef("query-returns", "%s: table returns must match targets[0] %s", where, q.Return.TableID)
		}
	case "entity":
		e := c.entities[q.Return.EntityID]
		if e == nil {
			c.rulef("query-returns", "%s: returns unknown entity %s", where, q.Return.EntityID)
			return
		}
		if e.Primary != "" && !q.tableSet()[e.Primary] {
			c.rulef("query-returns", "%s: returns entity %s but does not target its primary table %s", where, e.ID, e.Primary)
		}
	case "object":
		// output_fields 在 checkQueryOutputFields 校验
	}
}

func (c *Checker) checkQueryOutputFields(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.OutputFields) == 0 {
		if q.Return.Kind == "object" {
			c.rulef("query-output-fields", "%s: returns object but output_fields is empty", where)
		}
		return
	}
	if q.Return.Kind != "object" {
		c.rulef("query-output-fields", "%s: output_fields requires returns object", where)
	}
	if len(q.ReturnColumns) > 0 {
		c.rulef("query-output-fields", "%s: must not combine output_fields with return_columns", where)
	}
	seen := map[string]bool{}
	for _, f := range q.OutputFields {
		if seen[f.Name] {
			c.rulef("query-output-fields", "%s: duplicate output field %s", where, f.Name)
		}
		seen[f.Name] = true
		if strings.TrimSpace(f.Type) == "" {
			c.rulef("query-output-fields", "%s: output field %s missing type", where, f.Name)
		} else if !strings.HasPrefix(f.Type, "data.enums.") && !c.isEntityType(f.Type) {
			c.rulef("query-output-fields", "%s: output field %s has unsupported type %s", where, f.Name, f.Type)
		}
	}
}

func (c *Checker) checkQueryReturnColumns(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.ReturnColumns) > 0 && q.Return.Kind != "table" && q.Return.Kind != "entity" {
		c.rulef("query-return-columns", "%s: return_columns is only allowed for table/entity returns", where)
		return
	}
	selected := map[string]bool{}
	for _, col := range q.ReturnColumns {
		selected[col] = true
	}
	switch q.Return.Kind {
	case "table":
		cols := c.tableColumns[q.Return.TableID]
		for _, col := range q.ReturnColumns {
			meta, ok := cols[col]
			if !ok {
				c.rulef("query-return-columns", "%s: return column %s not found in %s", where, col, q.Return.TableID)
				continue
			}
			if meta.neverExpose && q.OwnerModule != c.tableOwners[q.Return.TableID] {
				c.rulef("query-sensitive-columns", "%s: returns never_expose column %s outside module %s", where, col, c.tableOwners[q.Return.TableID])
			}
		}
	case "entity":
		e := c.entities[q.Return.EntityID]
		if e == nil {
			return
		}
		for _, col := range q.ReturnColumns {
			f := findEntityFieldByColumn(e, col)
			if f == nil {
				c.rulef("query-return-columns", "%s: return column %s not found in %s", where, col, e.ID)
				continue
			}
			if f.NeverExpose && q.OwnerModule != c.tableOwners[f.ColumnTable] {
				c.rulef("query-sensitive-columns", "%s: returns never_expose field %s outside module %s", where, f.Name, c.tableOwners[f.ColumnTable])
			}
		}
		c.checkEntityCoverage(q, e, selected)
	}
}

// checkEntityCoverage 校验实体返回的投影：来源表必须在查询表集合内，裁剪不得丢失必填字段。
func (c *Checker) checkEntityCoverage(q *queryInfo, e *entityInfo, selected map[string]bool) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	tables := q.tableSet()
	missing := map[string]bool{}
	owner := e.OwnerModule
	if owner == "" {
		owner = c.tableOwners[e.Primary]
	}
	for _, f := range e.Fields {
		if f.IsRelation {
			continue
		}
		if f.SourceComputed != "" {
			// computed 字段无法通过 return_columns 裁剪；跨模块返回 never_expose 直接报错。
			if f.NeverExpose && q.OwnerModule != owner {
				c.rulef("query-sensitive-columns", "%s: returns computed never_expose field %s outside module %s", where, f.Name, owner)
			}
			continue
		}
		if f.Column == "" {
			continue
		}
		if len(selected) == 0 {
			if f.NeverExpose {
				continue
			}
		} else if !selected[f.Column] {
			if !f.NeverExpose && !strings.HasPrefix(f.Type, "*") {
				c.rulef("query-entity-coverage", "%s: omits required field %s", where, f.Name)
			}
			continue
		}
		if !tables[f.ColumnTable] {
			missing[f.ColumnTable] = true
		}
	}
	for _, table := range sortedKeys(missing) {
		c.rulef("query-entity-coverage", "%s: returns %s but does not read its source table %s", where, e.ID, table)
	}
}

func findEntityField(e *entityInfo, name string) *entityFieldInfo {
	for i := range e.Fields {
		if e.Fields[i].Name == name {
			return &e.Fields[i]
		}
	}
	return nil
}

func findEntityFieldByColumn(e *entityInfo, column string) *entityFieldInfo {
	for i := range e.Fields {
		if !e.Fields[i].IsRelation && e.Fields[i].Column == column {
			return &e.Fields[i]
		}
	}
	return nil
}

// splitIndexRef 拆分 data.tables.<t>.<pk|index-name>。
func splitIndexRef(ref string) (table, name string, ok bool) {
	i := strings.LastIndex(ref, ".")
	if i <= 0 || i+1 >= len(ref) {
		return "", "", false
	}
	table, name = ref[:i], ref[i+1:]
	if !strings.HasPrefix(table, "data.tables.") {
		return "", "", false
	}
	return table, name, true
}

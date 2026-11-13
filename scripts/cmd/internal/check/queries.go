// queries.go 校验 data.queries：接口契约（入参/结果/目标表/索引/读写归属）。
// SQL 如何执行不在契约里；checker 只验证可机械判定的事实。
package check

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

type queryParam struct {
	Name         string
	Type         string
	Column       string
	Expr         string
	Nullable     bool
	HasDefault   bool
	DefaultValue any
}

type queryReturn struct {
	Name     string
	Kind     string // column / entity / table / scalar / affected_rows
	Column   string
	Entity   string
	Table    string
	Type     string
	Columns  []string
	Nullable bool
	List     bool
}

type queryInfo struct {
	ID          string
	Name        string
	File        string
	OwnerModule string
	Targets     []string
	Params      []queryParam
	Returns     []queryReturn
	IndexRef    []string
}

type queryDoc struct {
	rel    string
	fileID string
	doc    map[string]any
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
		fileID := schema.Targets.FileID(filepath.Base(file))
		if fileID == "" {
			fileID = strings.TrimSuffix(rel, filepath.Ext(rel))
		}
		c.queryDocs = append(c.queryDocs, queryDoc{rel: rel, fileID: fileID, doc: doc})
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
		}
	}
}

func parseQuery(m map[string]any, rel, fileID, owner string) *queryInfo {
	q := &queryInfo{File: rel, OwnerModule: owner}
	q.Name, _ = m["name"].(string)
	q.ID = contractspec.QueryID(fileID, q.Name)
	q.Targets = stringList(m["tables"])
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
		p.Expr, _ = pm["expr"].(string)
		p.Nullable, _ = pm["nullable"].(bool)
		if v, ok := pm["default"]; ok {
			p.HasDefault, p.DefaultValue = true, v
		}
		q.Params = append(q.Params, p)
	}
	for _, raw := range asList(m["returns"]) {
		rm, ok := asMap(raw)
		if !ok {
			continue
		}
		r := queryReturn{}
		r.Name, _ = rm["name"].(string)
		r.Kind, _ = rm["kind"].(string)
		r.Column, _ = rm["column"].(string)
		r.Entity, _ = rm["entity"].(string)
		r.Table, _ = rm["table"].(string)
		r.Type, _ = rm["type"].(string)
		r.Columns = stringList(rm["columns"])
		r.Nullable, _ = rm["nullable"].(bool)
		r.List, _ = rm["list"].(bool)
		q.Returns = append(q.Returns, r)
	}
	return q
}

func (c *Checker) checkQueries(schema *Schema) {
	c.checkTargets(schema)
	if len(c.queryFiles()) == 0 {
		c.rulef("schema.load", "no contract files match %s/%s", schema.Targets.Dir, schema.Targets.Glob)
	}
	for _, qd := range c.queryDocs {
		c.checkTargetFileName(schema, filepath.Base(qd.rel), qd.rel)
		c.validateDoc(qd.doc, schema.Fields, qd.rel)
		c.checkStability(qd.doc, qd.rel)
		if s, _ := qd.doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", qd.rel)
		}
		if len(asList(qd.doc["queries"])) == 0 {
			c.rulef("schema.non-empty", "%s: queries must not be empty", qd.rel)
		}
		c.checkFieldOrder(qd.rel, schema.Fields, orderRules{top: "query-key-order", nested: "query-key-order"})
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
	names := map[string]string{}
	for _, id := range c.queryOrder {
		q := c.queries[id]
		c.checkQueryIdentity(q, names)
		c.checkQueryShape(q)
		c.checkQueryTable(q)
		c.checkQueryPagination(q)
		c.checkQueryParams(q)
		c.checkQueryParamColumns(q)
		c.checkQueryReturns(q)
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

// checkQueryShape 校验返回形状：affected_rows 单值、table 结果不能与其它项混用。
func (c *Checker) checkQueryShape(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Returns) == 0 {
		c.rulef("query-returns", "%s: returns must not be empty", where)
		return
	}
	hasAffected := false
	hasTable := false
	for _, r := range q.Returns {
		if r.Kind == "affected_rows" {
			hasAffected = true
		}
		if r.Kind == "table" {
			hasTable = true
		}
	}
	if hasAffected && len(q.Returns) > 1 {
		c.rulef("query-shape", "%s: affected_rows must be the only return item", where)
	}
	if hasAffected && len(q.Returns) == 1 && q.Returns[0].List {
		c.rulef("query-shape", "%s: affected_rows must not be a list", where)
	}
	if hasTable && len(q.Returns) > 1 {
		c.rulef("query-shape", "%s: table return must be the only return item", where)
	}
}

func (c *Checker) checkQueryTable(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Targets) == 0 {
		c.rulef("query-table-shape", "%s: tables must not be empty", where)
		return
	}
	seen := map[string]bool{}
	for _, t := range q.Targets {
		if seen[t] {
			c.rulef("query-table-shape", "%s: duplicate target table %s", where, t)
		}
		seen[t] = true
		if t == c.baseID {
			c.rulef("query-table-shape", "%s: tables references data.tables.base", where)
			continue
		}
		if _, ok := c.tableColumns[t]; !ok {
			c.rulef("query-table-shape", "%s: target table %s not found", where, t)
		}
	}
}

// checkQueryPagination 要求 list 返回声明标准分页参数，并校验保留参数形状。
func (c *Checker) checkQueryPagination(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	byName := map[string]queryParam{}
	for _, p := range q.Params {
		byName[p.Name] = p
	}
	for _, name := range []string{"offset", "limit"} {
		if p, ok := byName[name]; ok {
			c.checkPaginationParam(name, p, where)
		}
	}
	hasList := false
	for _, r := range q.Returns {
		if r.List {
			hasList = true
			break
		}
	}
	if !hasList {
		return
	}
	for _, name := range []string{"offset", "limit"} {
		if _, ok := byName[name]; !ok {
			c.rulef("query-pagination", "%s: list return requires %s param", where, name)
		}
	}
}

func (c *Checker) checkPaginationParam(name string, p queryParam, where string) {
	if p.Type != "integer" {
		c.rulef("query-pagination", "%s: %s must have type integer", where, name)
	}
	if !p.Nullable {
		c.rulef("query-pagination", "%s: %s must set nullable:true", where, name)
	}
	if !p.HasDefault {
		c.rulef("query-pagination", "%s: %s must declare default", where, name)
		return
	}
	n, ok := toInt(p.DefaultValue)
	if !ok {
		c.rulef("query-pagination", "%s: %s default must be an integer", where, name)
		return
	}
	if name == "offset" && n != 0 {
		c.rulef("query-pagination", "%s: offset default must be 0", where)
	}
	if name == "limit" && n <= 0 {
		c.rulef("query-pagination", "%s: limit default must be positive", where)
	}
}

func (c *Checker) checkQueryParams(q *queryInfo) {
	seen := map[string]bool{}
	for _, p := range q.Params {
		where := fmt.Sprintf("%s: query %s param %s", q.File, q.ID, p.Name)
		if seen[p.Name] {
			c.rulef("query-params", "%s: duplicate param", where)
		}
		seen[p.Name] = true
		if strings.TrimSpace(p.Type) == "" {
			c.rulef("query-params", "%s: type must not be empty", where)
		} else if p.Column == "" {
			if _, _, _, ok := paramLogicalType(p.Type); !ok {
				c.rulef("query-params", "%s: type %q cannot map to a type family or data.enums.*", where, p.Type)
			}
		}
		if p.Column != "" && p.Expr != "" {
			c.rulef("query-params", "%s: column and expr are mutually exclusive", where)
		}
		if p.Expr != "" && strings.TrimSpace(p.Expr) == "" {
			c.rulef("query-params", "%s: expr must not be blank", where)
		}
		if p.HasDefault && !p.Nullable && !isPaginationParam(p.Name) {
			c.rulef("query-params", "%s: default requires nullable:true (null means use default)", where)
		}
		if p.HasDefault && !isPaginationParam(p.Name) {
			c.checkQueryParamDefault(p, where)
		}
	}
}

// isPaginationParam 标记保留分页参数名；其 default 形状由 checkQueryPagination 统一校验。
func isPaginationParam(name string) bool {
	return name == "offset" || name == "limit"
}

// checkQueryParamDefault 校验 default 的 YAML 类型与 param.type 兼容。
// 枚举默认值可能是 value 或 code，无法脚本判定，交给生成器；数组只校验外层 list。
func (c *Checker) checkQueryParamDefault(p queryParam, where string) {
	if p.DefaultValue == nil {
		return
	}
	family, array, enum, ok := paramLogicalType(p.Type)
	if !ok {
		return
	}
	if array {
		if _, ok := asList2(p.DefaultValue); !ok {
			c.rulef("query-params", "%s: default must be a list for %s", where, p.Type)
		}
		return
	}
	if enum {
		return
	}
	switch family {
	case "string", "bytes":
		if _, ok := p.DefaultValue.(string); !ok {
			c.rulef("query-params", "%s: default must be a string for %s", where, p.Type)
		}
	case "integer":
		if _, ok := toInt(p.DefaultValue); !ok {
			c.rulef("query-params", "%s: default must be an integer for %s", where, p.Type)
		}
	case "float", "decimal":
		if _, ok := toFloat(p.DefaultValue); !ok {
			c.rulef("query-params", "%s: default must be a number for %s", where, p.Type)
		}
	case "boolean":
		if _, ok := p.DefaultValue.(bool); !ok {
			c.rulef("query-params", "%s: default must be a boolean for %s", where, p.Type)
		}
	case "timestamp", "datetime":
		switch p.DefaultValue.(type) {
		case string, time.Time:
		default:
			c.rulef("query-params", "%s: default must be a string/time for %s", where, p.Type)
		}
	case "json":
		// JSON 默认值允许任意 YAML 结构。
	}
}

// checkQueryParamColumns 校验 params[].column 的列存在性与类型族兼容。
func (c *Checker) checkQueryParamColumns(q *queryInfo) {
	if len(q.Targets) == 0 {
		return // 缺失 tables 由 query-table-shape 报出
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
		family, _, _, ok := paramLogicalType(p.Type)
		if !ok {
			c.rulef("query-param-column", "%s: type %q cannot bind a column", where, p.Type)
			continue
		}
		if !familyCompatible(family, c.resolvedColumnType(meta.typ)) {
			c.rulef("query-param-column", "%s: type %s is not compatible with column %s (%s)", where, p.Type, p.Column, c.resolvedColumnType(meta.typ))
		}
	}
}

// paramLogicalType 把参数类型映射到列类型族；数组类型返回元素类型族。
// data.enums.* 持久化为 integer，因此按 integer 列绑定。
func paramLogicalType(typ string) (family string, array, enum, ok bool) {
	base := strings.TrimSpace(typ)
	if strings.HasPrefix(base, "List<") && strings.HasSuffix(base, ">") {
		array = true
		base = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(base, "List<"), ">"))
	}
	if strings.HasPrefix(base, "data.enums.") {
		return "integer", array, true, true
	}
	family, ok = paramFamily(base)
	return family, array, false, ok
}

// paramFamily 把非数组参数类型映射到 column_types 类型族。
func paramFamily(typ string) (family string, ok bool) {
	base := strings.ToLower(strings.TrimSpace(typ))
	switch base {
	case "text", "varchar", "character varying", "char", "bpchar":
		return "string", true
	case "integer", "int", "int4", "int8", "bigint", "smallint":
		return "integer", true
	case "boolean", "bool":
		return "boolean", true
	case "timestamp", "timestamptz", "timestamp with time zone", "datetime":
		return "timestamp", true
	case "float", "float4", "float8", "double", "double precision", "real":
		return "float", true
	case "decimal", "numeric":
		return "decimal", true
	case "json", "jsonb":
		return "json", true
	case "bytea", "bytes":
		return "bytes", true
	}
	return "", false
}

func familyCompatible(param, column string) bool {
	if param == column {
		return true
	}
	return (param == "timestamp" && column == "datetime") || (param == "datetime" && column == "timestamp")
}

// checkQueryReturns 校验 returns 每项的 kind、专属字段、互斥字段与 list 一致性。
func (c *Checker) checkQueryReturns(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Returns) == 0 {
		return // query-returns 已报
	}
	names := map[string]bool{}
	var listSet bool
	var hasList bool
	for i, r := range q.Returns {
		rw := fmt.Sprintf("%s returns[%d]", where, i)
		if r.Name == "" {
			c.rulef("query-returns", "%s: name must not be empty", rw)
		} else if names[r.Name] {
			c.rulef("query-returns", "%s: duplicate return name %s", rw, r.Name)
		}
		names[r.Name] = true
		if hasList && r.List != listSet {
			c.rulef("query-returns", "%s: list must be consistent across return items", rw)
		}
		listSet, hasList = r.List, true

		switch r.Kind {
		case "column":
			if r.Column == "" {
				c.rulef("query-returns", "%s: kind=column requires column", rw)
				continue
			}
			if r.Entity != "" || r.Table != "" || r.Type != "" || len(r.Columns) > 0 {
				c.rulef("query-returns", "%s: kind=column forbids entity/table/type/columns", rw)
			}
			c.checkReturnColumn(rw, q, r.Column, r.Columns)
		case "entity":
			if r.Entity == "" {
				c.rulef("query-returns", "%s: kind=entity requires entity", rw)
				continue
			}
			if r.Column != "" || r.Table != "" || r.Type != "" {
				c.rulef("query-returns", "%s: kind=entity forbids column/table/type", rw)
			}
			e := c.entities[r.Entity]
			if e == nil {
				c.rulef("query-returns", "%s: unknown entity %s", rw, r.Entity)
				continue
			}
			if e.Primary != "" && !q.tableSet()[e.Primary] {
				c.rulef("query-returns", "%s: entity %s primary table %s not in tables", rw, e.ID, e.Primary)
			}
			c.checkEntityReturnColumns(rw, q, e, r.Columns)
		case "table":
			if r.Table == "" {
				c.rulef("query-returns", "%s: kind=table requires table", rw)
				continue
			}
			if r.Column != "" || r.Entity != "" || r.Type != "" {
				c.rulef("query-returns", "%s: kind=table forbids column/entity/type", rw)
			}
			if len(q.Targets) == 0 || q.Targets[0] != r.Table {
				c.rulef("query-returns", "%s: table must match tables[0]", rw)
			}
			if _, ok := c.tableColumns[r.Table]; !ok {
				c.rulef("query-returns", "%s: unknown table %s", rw, r.Table)
				continue
			}
			c.checkTableReturnColumns(rw, q, r.Table, r.Columns)
		case "scalar":
			if r.Type == "" {
				c.rulef("query-returns", "%s: kind=scalar requires type", rw)
				continue
			}
			if r.Column != "" || r.Entity != "" || r.Table != "" || len(r.Columns) > 0 {
				c.rulef("query-returns", "%s: kind=scalar forbids column/entity/table/columns", rw)
			}
			if !c.scalarReturnType(r.Type) {
				c.rulef("query-returns", "%s: unsupported scalar type %s", rw, r.Type)
			}
		case "affected_rows":
			if r.List {
				c.rulef("query-shape", "%s: affected_rows must not be a list", rw)
			}
			if r.Column != "" || r.Entity != "" || r.Table != "" || r.Type != "" || len(r.Columns) > 0 {
				c.rulef("query-shape", "%s: affected_rows forbids column/entity/table/type/columns", rw)
			}
		default:
			c.rulef("query-returns", "%s: invalid kind %q", rw, r.Kind)
		}
	}
}

func (c *Checker) scalarReturnType(typ string) bool {
	base := strings.TrimPrefix(strings.TrimSpace(typ), "*")
	if strings.HasPrefix(base, "data.enums.") {
		return true // 枚举存在性由 checkEnumRefs 全仓校验
	}
	return sqlScalarReturns[base]
}

func (c *Checker) checkReturnColumn(where string, q *queryInfo, ref string, _ []string) {
	t, col, ok := splitColumnRef(ref)
	if !ok {
		c.rulef("query-return-columns", "%s: malformed column %q", where, ref)
		return
	}
	if !q.tableSet()[t] {
		c.rulef("query-return-columns", "%s: column %s does not belong to query tables", where, ref)
		return
	}
	meta, ok := c.tableColumns[t][col]
	if !ok {
		c.rulef("query-return-columns", "%s: column %s not found", where, ref)
		return
	}
	if normalizeSensitivity(meta.sensitivity) == sensitivitySecret && q.OwnerModule != c.tableOwners[t] {
		c.rulef("query-sensitive-columns", "%s: returns secret column %s outside module %s", where, col, c.tableOwners[t])
	}
}

func (c *Checker) checkTableReturnColumns(where string, q *queryInfo, table string, columns []string) {
	for _, col := range columns {
		meta, ok := c.tableColumns[table][col]
		if !ok {
			c.rulef("query-return-columns", "%s: return column %s not found in %s", where, col, table)
			continue
		}
		if normalizeSensitivity(meta.sensitivity) == sensitivitySecret && q.OwnerModule != c.tableOwners[table] {
			c.rulef("query-sensitive-columns", "%s: returns secret column %s outside module %s", where, col, c.tableOwners[table])
		}
	}
}

func (c *Checker) checkEntityReturnColumns(where string, q *queryInfo, e *entityInfo, columns []string) {
	selected := map[string]bool{}
	for _, col := range columns {
		selected[col] = true
		f := findEntityFieldByColumn(e, col)
		if f == nil {
			c.rulef("query-return-columns", "%s: return column %s not found in %s", where, col, e.ID)
			continue
		}
		if normalizeSensitivity(f.Sensitivity) == sensitivitySecret && q.OwnerModule != c.tableOwners[f.ColumnTable] {
			c.rulef("query-sensitive-columns", "%s: returns secret field %s outside module %s", where, f.Name, c.tableOwners[f.ColumnTable])
		}
	}
	c.checkEntityCoverage(q, e, selected)
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
			if normalizeSensitivity(f.Sensitivity) == sensitivitySecret && q.OwnerModule != owner {
				c.rulef("query-sensitive-columns", "%s: returns computed secret field %s outside module %s", where, f.Name, owner)
			}
			continue
		}
		if f.Column == "" {
			continue
		}
		if len(selected) == 0 {
			if normalizeSensitivity(f.Sensitivity) == sensitivitySecret {
				continue
			}
		} else if !selected[f.Column] {
			if normalizeSensitivity(f.Sensitivity) != sensitivitySecret && !strings.HasPrefix(f.Type, "*") {
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

func queryReturnsAffectedRows(q *queryInfo) bool {
	for _, r := range q.Returns {
		if r.Kind == "affected_rows" {
			return true
		}
	}
	return false
}

// resolveQueryID 保留调用点兼容；query ID 已是规范值。
func (c *Checker) resolveQueryID(id string) string { return id }

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

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
	HasExpr      bool
	Nullable     bool
	HasDefault   bool
	DefaultValue any
}

type queryReturn struct {
	Name       string
	Kind       string // scalar / affected_rows / entity / columns / error
	Entity     string
	Type       string
	Columns    []string
	HasColumns bool
	List       bool
	Nullable   bool
}

type queryInfo struct {
	ID          string
	Name        string
	File        string
	OwnerModule string
	Targets     []string
	Writes      []string
	HasTables   bool
	Params      []queryParam
	Returns     []queryReturn
	HasReturns  bool
	IndexRef    []string
	Errors      []string
	UsedBy      []string
}

type queryDoc struct {
	rel    string
	fileID string
	doc    map[string]any
}

// errorSet 返回查询的领域错误集合。
func (q *queryInfo) errorSet() map[string]bool {
	out := map[string]bool{}
	for _, code := range q.Errors {
		out[code] = true
	}
	return out
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
	if _, ok := m["tables"]; ok {
		q.HasTables = true
	}
	for _, raw := range asList(m["tables"]) {
		tm, ok := asMap(raw)
		if !ok {
			continue
		}
		table, _ := tm["table"].(string)
		if table == "" {
			continue
		}
		q.Targets = append(q.Targets, table)
		if write, _ := tm["write"].(bool); write {
			q.Writes = append(q.Writes, table)
		}
	}
	if _, ok := m["returns"]; ok {
		q.HasReturns = true
	}
	q.IndexRef = stringList(m["index_ref"])
	q.Errors = stringList(m["errors"])
	q.UsedBy = stringList(m["used_by"])
	for _, raw := range asList(m["params"]) {
		pm, ok := asMap(raw)
		if !ok {
			continue
		}
		p := queryParam{}
		p.Name, _ = pm["name"].(string)
		p.Type, _ = pm["type"].(string)
		p.Column, _ = pm["column"].(string)
		if _, ok := pm["expr"]; ok {
			p.HasExpr = true
		}
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
		r.Entity, _ = rm["entity"].(string)
		r.Type, _ = rm["type"].(string)
		r.Columns = stringList(rm["columns"])
		_, r.HasColumns = rm["columns"]
		r.List, _ = rm["list"].(bool)
		r.Nullable, _ = rm["nullable"].(bool)
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
		if _, present := qd.doc["queries"]; present && len(asList(qd.doc["queries"])) == 0 {
			c.rulef("schema.non-empty", "%s: queries must not be empty", qd.rel)
		}
		c.checkFieldOrder(qd.rel, schema.Fields, orderRules{top: "query-key-order", nested: "query-key-order"})
		id, _ := qd.doc["id"].(string)
		want := schema.Targets.FileID(filepath.Base(qd.rel))
		if want != "" && id != "" && id != want {
			c.rulef("schema.id-from-file", "%s: id must be %s, got %s (file name is the source)", qd.rel, want, id)
		}
		c.checkQueryDoNot(qd)
	}
	for id := range c.queryDupIDs {
		c.rulef("query-name-unique", "duplicate query name/derived id %s", id)
	}
	for _, id := range c.queryOrder {
		q := c.queries[id]
		c.checkQueryShape(q)
		c.checkQueryTable(q)
		c.checkQueryPagination(q)
		c.checkQueryParams(q)
		c.checkQueryParamColumns(q)
		c.checkQueryReturns(q)
		c.checkQueryErrors(q)
	}
}

func (c *Checker) checkQueryErrors(q *queryInfo) {
	baseline := c.dataAccessErrors()
	for _, code := range q.Errors {
		if baseline[code] {
			c.rulef("query-errors", "%s: query %s must not repeat data-access baseline error %s", q.File, q.ID, code)
			continue
		}
		if !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
			c.rulef("query-errors", "%s: query %s references unknown error %s", q.File, q.ID, code)
		}
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

// checkQueryShape 校验返回形状：affected_rows 必须是唯一返回项且不能是列表。
func (c *Checker) checkQueryShape(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Returns) == 0 {
		if q.HasReturns {
			c.rulef("query-returns", "%s: returns must not be empty", where)
		}
		return
	}
	nonErr := make([]queryReturn, 0, len(q.Returns))
	for _, r := range q.Returns {
		if r.Kind != "error" {
			nonErr = append(nonErr, r)
		}
	}
	if len(nonErr) == 0 {
		c.rulef("query-shape", "%s: returns must contain at least one non-error item", where)
		return
	}
	hasAffected := false
	hasColumns := false
	for _, r := range nonErr {
		switch r.Kind {
		case "affected_rows":
			hasAffected = true
		case "columns":
			hasColumns = true
		}
	}
	if hasAffected && len(nonErr) > 1 {
		c.rulef("query-shape", "%s: affected_rows must be the only non-error return item", where)
	}
	if hasAffected && len(nonErr) == 1 && nonErr[0].List {
		c.rulef("query-shape", "%s: affected_rows must not be a list", where)
	}
	if hasColumns && len(nonErr) > 1 {
		c.rulef("query-shape", "%s: columns return must be the only non-error return item", where)
	}
}

func (c *Checker) checkQueryTable(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Targets) == 0 {
		if q.HasTables {
			c.rulef("query-table-shape", "%s: tables must not be empty", where)
		}
		return
	}
	seen := map[string]bool{}
	for _, t := range q.Targets {
		if seen[t] {
			c.rulef("query-table-shape", "%s: duplicate table %s", where, t)
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
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if len(q.Params) == 0 || q.Params[0].Name != "ctx" || q.Params[0].Type != "context.Context" {
		c.rulef("query-params", "%s: params[0] must be ctx context.Context", where)
	}
	seen := map[string]bool{}
	for i, p := range q.Params {
		pw := fmt.Sprintf("%s: query %s param %s", q.File, q.ID, p.Name)
		if seen[p.Name] {
			c.rulef("query-params", "%s: duplicate param", pw)
		}
		seen[p.Name] = true
		if p.Name == "ctx" {
			if i != 0 {
				c.rulef("query-params", "%s: ctx must be the first param", pw)
			}
			if p.Column != "" || p.HasExpr || p.HasDefault || p.Nullable {
				c.rulef("query-params", "%s: ctx must not bind column/expr/default/nullable", pw)
			}
			continue
		}
		if p.Type != "" && p.Column == "" {
			if _, _, _, ok := paramLogicalType(p.Type); !ok {
				c.rulef("query-params", "%s: type %q cannot map to a type family or data.enums.*", pw, p.Type)
			}
		}
		if p.Column != "" && p.HasExpr {
			c.rulef("query-params", "%s: column and expr are mutually exclusive", pw)
		}
		if p.HasExpr && strings.TrimSpace(p.Expr) == "" {
			c.rulef("query-params", "%s: expr must not be blank", pw)
		}
		if p.HasDefault && !p.Nullable && !isPaginationParam(p.Name) {
			c.rulef("query-params", "%s: default requires nullable:true (null means use default)", pw)
		}
		if p.HasDefault && !isPaginationParam(p.Name) {
			for _, msg := range c.paramDefaultErrors(p.Type, p.DefaultValue, pw) {
				c.rulef("query-params", "%s", msg)
			}
		}
	}
}

// isPaginationParam 标记保留分页参数名；其 default 形状由 checkQueryPagination 统一校验。
func isPaginationParam(name string) bool {
	return name == "offset" || name == "limit"
}

// paramDefaultErrors 返回 default 与 param.type 不兼容的问题描述。
// 枚举默认值可能是 value 或 code，无法脚本判定，交给生成器；数组只校验外层 list。
func (c *Checker) paramDefaultErrors(typ string, value any, where string) []string {
	if value == nil {
		return nil
	}
	family, array, enum, ok := paramLogicalType(typ)
	if !ok {
		return nil
	}
	if array {
		if _, ok := asList2(value); !ok {
			return []string{fmt.Sprintf("%s: default must be a list for %s", where, typ)}
		}
		return nil
	}
	if enum {
		return nil
	}
	bad := func(want string) []string {
		return []string{fmt.Sprintf("%s: default must be %s for %s", where, want, typ)}
	}
	switch family {
	case "string", "bytes":
		if _, ok := value.(string); !ok {
			return bad("a string")
		}
	case "integer":
		if _, ok := toInt(value); !ok {
			return bad("an integer")
		}
	case "float", "decimal":
		if _, ok := toFloat(value); !ok {
			return bad("a number")
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad("a boolean")
		}
	case "timestamp", "datetime":
		switch value.(type) {
		case string, time.Time:
		default:
			return bad("a string/time")
		}
	case "json":
		// JSON 默认值允许任意 YAML 结构。
	}
	return nil
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
		if normalizeSensitivity(meta.sensitivity) == sensitivitySecret && q.OwnerModule != c.tableOwners[t] {
			c.rulef("query-sensitive-columns", "%s: param binds secret column %s outside module %s", where, col, c.tableOwners[t])
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

// paramFamily 把非数组参数/标量返回类型映射到 type_families 类型族。
func paramFamily(typ string) (family string, ok bool) {
	base := strings.ToLower(strings.TrimSpace(typ))
	switch base {
	case "string", "text", "varchar", "character varying", "char", "bpchar":
		return "string", true
	case "integer", "int", "int4", "int8", "bigint", "smallint":
		return "integer", true
	case "boolean", "bool":
		return "boolean", true
	case "timestamp", "timestamptz", "timestamp with time zone":
		return "timestamp", true
	case "datetime", "date", "time":
		return "datetime", true
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
	last := q.Returns[len(q.Returns)-1]
	if last.Name != "err" || last.Kind != "error" {
		c.rulef("query-returns", "%s: returns must end with {name: err, kind: error}", where)
	}
	names := map[string]bool{}
	var listSet bool
	var hasList bool
	for i, r := range q.Returns {
		rw := fmt.Sprintf("%s returns[%d]", where, i)
		if r.Name != "" && names[r.Name] {
			c.rulef("query-returns", "%s: duplicate return name %s", rw, r.Name)
		}
		names[r.Name] = true

		if r.Kind == "error" {
			if r.Name != "err" {
				c.rulef("query-returns", "%s: kind=error must be named err", rw)
			}
			if i != len(q.Returns)-1 {
				c.rulef("query-returns", "%s: kind=error must be the last return item", rw)
			}
			if r.List || r.Type != "" || r.Entity != "" || len(r.Columns) > 0 || r.Nullable {
				c.rulef("query-returns", "%s: kind=error forbids list/type/entity/columns/nullable", rw)
			}
			continue
		}
		if r.Name == "err" {
			c.rulef("query-returns", "%s: err return must use kind=error", rw)
		}
		if hasList && r.List != listSet {
			c.rulef("query-returns", "%s: list must be consistent across return items", rw)
		}
		listSet, hasList = r.List, true

		switch r.Kind {
		case "entity":
			if r.Entity == "" {
				c.rulef("query-returns", "%s: kind=entity requires entity", rw)
				continue
			}
			if r.Type != "" || len(r.Columns) > 0 {
				c.rulef("query-returns", "%s: kind=entity forbids type/columns", rw)
			}
			e := c.entities[r.Entity]
			if e == nil {
				c.rulef("query-returns", "%s: unknown entity %s", rw, r.Entity)
				continue
			}
			if e.Primary != "" && !q.tableSet()[e.Primary] {
				c.rulef("query-returns", "%s: entity %s primary table %s not in tables", rw, e.ID, e.Primary)
			}
			c.checkEntitySecretProjection(rw, q, e)
		case "columns":
			if len(r.Columns) == 0 {
				c.rulef("query-returns", "%s: kind=columns requires columns", rw)
				continue
			}
			if r.Entity != "" || r.Type != "" {
				c.rulef("query-returns", "%s: kind=columns forbids entity/type", rw)
			}
			c.checkColumnProjection(rw, q, r.Columns)
		case "scalar":
			if r.Type == "" {
				c.rulef("query-returns", "%s: kind=scalar requires type", rw)
				continue
			}
			if r.Entity != "" || len(r.Columns) > 0 {
				c.rulef("query-returns", "%s: kind=scalar forbids entity/columns", rw)
			}
			if !c.scalarReturnType(r.Type) {
				c.rulef("query-returns", "%s: unsupported scalar type %s", rw, r.Type)
			}
		case "affected_rows":
			if r.Entity != "" || r.Type != "" || len(r.Columns) > 0 {
				c.rulef("query-shape", "%s: affected_rows forbids entity/type/columns", rw)
			}
			if r.Nullable {
				c.rulef("query-shape", "%s: affected_rows forbids nullable", rw)
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
	_, ok := paramFamily(base)
	return ok
}

// checkEntitySecretProjection 校验 entity 默认投影：computed secret 跨模块一律拒绝；
// 列来源的 secret 字段由默认投影排除，不在此报错。
func (c *Checker) checkEntitySecretProjection(where string, q *queryInfo, e *entityInfo) {
	owner := e.OwnerModule
	if owner == "" {
		owner = c.tableOwners[e.Primary]
	}
	for _, f := range e.Fields {
		if f.IsRelation || f.SourceComputed == "" {
			continue
		}
		if normalizeSensitivity(f.Sensitivity) == sensitivitySecret && q.OwnerModule != owner {
			c.rulef("query-sensitive-columns", "%s: returns computed secret field %s outside module %s", where, f.Name, owner)
		}
	}
}

// checkColumnProjection 校验 kind=columns 的列引用、所属查询表与 secret 作用域。
func (c *Checker) checkColumnProjection(where string, q *queryInfo, columns []string) {
	for _, ref := range columns {
		t, col, ok := splitColumnRef(ref)
		if !ok {
			c.rulef("query-return-columns", "%s: column %q must be data.tables.<t>.col.<c>", where, ref)
			continue
		}
		if !q.tableSet()[t] {
			c.rulef("query-return-columns", "%s: column %s does not belong to query tables", where, ref)
			continue
		}
		meta, ok := c.tableColumns[t][col]
		if !ok {
			c.rulef("query-return-columns", "%s: column %s not found", where, ref)
			continue
		}
		if normalizeSensitivity(meta.sensitivity) == sensitivitySecret && q.OwnerModule != c.tableOwners[t] {
			c.rulef("query-sensitive-columns", "%s: returns secret column %s outside module %s", where, col, c.tableOwners[t])
		}
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

func queryReturnsAffectedRows(q *queryInfo) bool {
	for _, r := range q.Returns {
		if r.Kind == "affected_rows" {
			return true
		}
	}
	return false
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

// checkQueryUsedBy 对账 query.used_by 与 method.queries 的反向索引。
func (c *Checker) checkQueryUsedBy() {
	expected := map[string]map[string]bool{}
	for _, f := range glob(c.root, "contracts/capabilities/services/*.yaml") {
		doc, err := loadYAML(f)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["services"]) {
			item, _ := asMap(raw)
			name, _ := item["name"].(string)
			serviceID := fileID + "." + name
			for _, rawStep := range asList(item["steps"]) {
				step, _ := asMap(rawStep)
				var qids []string
				for _, rawQ := range stepQueryItems(step) {
					qo, _ := asMap(rawQ)
					qid, _ := qo["query"].(string)
					if qid != "" {
						qids = append(qids, qid)
					}
				}
				for _, qid := range qids {
					if expected[qid] == nil {
						expected[qid] = map[string]bool{}
					}
					expected[qid][serviceID] = true
				}
			}
		}
	}
	for _, qid := range c.queryOrder {
		q := c.queries[qid]
		if q == nil {
			continue
		}
		want := expected[qid]
		if want == nil {
			want = map[string]bool{}
		}
		got := map[string]bool{}
		for _, u := range q.UsedBy {
			got[u] = true
		}
		if !sameSet(got, want) {
			c.rulef("query-used-by", "%s: used_by must be %v, got %v", qid, sortedKeys(want), sortedKeys(got))
		}
		if len(want) == 0 {
			c.rulef("query-unused", "%s: query is not referenced by any service step", qid)
		}
	}
}

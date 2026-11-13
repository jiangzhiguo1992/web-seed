// queries.go 校验 data.queries：ID/名称、目标表、参数、返回、伪 SQL 子句与写归属。
// 同时建立查询索引，供 index.used_by 与跨文件引用校验使用。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type queryParam struct {
	Name     string
	Type     string
	Column   string
	Nullable bool
}

// queryPredicate 是 where 的结构化谓词；字符串项仍是软表达式。
type queryPredicate struct {
	Column   string
	Op       string
	Param    string
	Value    any
	HasValue bool
	raw      map[string]any
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
	Expr     string
}

type queryJoin struct {
	Table string
	Type  string
	On    []string
}

type queryInfo struct {
	ID            string
	Name          string
	FileID        string
	File          string
	OwnerModule   string
	Kind          string
	Statement     string
	Targets       []string
	Joins         []queryJoin
	Params        []queryParam
	Return        queryReturn
	ReturnColumns []string
	OutputFields  []queryOutputField
	Expr          string
	Values        map[string]string
	Where         []string
	Predicates    []queryPredicate
	BadWhere      []any
	OrderBy       []string
	Limit         string
	IndexRef      []string
	Semantics     string
	Doc           map[string]any
}

type queryDoc struct {
	rel    string
	fileID string
	doc    map[string]any
	keys   []string
}

var sqlParamAtRe = regexp.MustCompile(`@([a-z_][a-z0-9_]*)`)
var sqlAliasSuffixRe = regexp.MustCompile(`(?is)\s+as\s+[a-z_][a-z0-9_]*\s*$`)
var sqlParamNargRe = regexp.MustCompile(`sqlc\.narg\('([a-z_][a-z0-9_]*)'\)`)
var sqlParamArgRe = regexp.MustCompile(`sqlc\.arg\('([a-z_][a-z0-9_]*)'\)`)

var sqlScalarReturns = map[string]bool{
	"boolean": true, "integer": true, "string": true, "float": true,
	"decimal": true, "timestamp": true, "datetime": true, "json": true,
}

// exprText 把所有伪 SQL 表达式拼成一段，供参数扫描与索引列提及校验使用。
func (q *queryInfo) exprText() string {
	parts := []string{q.Expr, q.Limit}
	parts = append(parts, q.Where...)
	for _, p := range q.Predicates {
		parts = append(parts, p.Column)
	}
	parts = append(parts, q.OrderBy...)
	for _, j := range q.Joins {
		parts = append(parts, j.On...)
	}
	for col, v := range q.Values {
		parts = append(parts, col, v)
	}
	for _, f := range q.OutputFields {
		parts = append(parts, f.Expr)
	}
	return strings.Join(parts, "\n")
}

// tableSet 返回查询读写的表集合：targets + joins。
func (q *queryInfo) tableSet() map[string]bool {
	out := map[string]bool{}
	for _, t := range q.Targets {
		out[t] = true
	}
	for _, j := range q.Joins {
		if j.Table != "" {
			out[j.Table] = true
		}
	}
	return out
}

// loadQueryIndex 解析查询文件；结构错误在 checkQueries 中报。
func (c *Checker) loadQueryIndex(schema *Schema) {
	c.queries = map[string]*queryInfo{}
	c.queryDocs = nil
	c.queryOrder = nil
	c.queryDupIDs = map[string]bool{}
	c.queryIDs = map[string]string{}
	c.queryTables = map[string]map[string]bool{}
	c.queryText = map[string]string{}
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
			c.queryText[q.ID] = q.exprText()
			c.queryTables[q.ID] = q.tableSet()
		}
	}
}

func parseQuery(m map[string]any, rel, fileID, owner string) *queryInfo {
	q := &queryInfo{File: rel, FileID: fileID, OwnerModule: owner, Doc: m}
	q.ID, _ = m["id"].(string)
	q.Name, _ = m["name"].(string)
	q.Kind, _ = m["kind"].(string)
	q.Statement, _ = m["statement"].(string)
	q.Semantics, _ = m["semantics"].(string)
	q.Targets = stringList(m["targets"])
	q.Expr = stringAt(m, "expr")
	q.Limit = stringAt(m, "limit")
	for _, raw := range asList(m["where"]) {
		switch v := raw.(type) {
		case string:
			q.Where = append(q.Where, v)
		case map[string]any:
			q.Predicates = append(q.Predicates, parsePredicate(v))
		default:
			q.BadWhere = append(q.BadWhere, raw)
		}
	}
	q.OrderBy = stringList(m["order_by"])
	q.IndexRef = stringList(m["index_ref"])
	for _, raw := range asList(m["joins"]) {
		jm, ok := asMap(raw)
		if !ok {
			continue
		}
		j := queryJoin{Table: stringAt(jm, "table"), Type: stringAt(jm, "type"), On: stringList(jm["on"])}
		if j.Type == "" {
			j.Type = "inner"
		}
		q.Joins = append(q.Joins, j)
	}
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
		f.Expr, _ = om["expr"].(string)
		q.OutputFields = append(q.OutputFields, f)
	}
	q.Values = map[string]string{}
	if vm, ok := asMap(m["values"]); ok {
		for k, raw := range vm {
			if s, ok := raw.(string); ok {
				q.Values[k] = s
			}
		}
	}
	return q
}

// parsePredicate 解析结构化谓词；值域与列引用在 checkQueryWhere 中校验。
func parsePredicate(m map[string]any) queryPredicate {
	p := queryPredicate{raw: m}
	p.Column, _ = m["column"].(string)
	p.Op, _ = m["op"].(string)
	p.Param, _ = m["param"].(string)
	p.Value, p.HasValue = m["value"]
	return p
}

func stringAt(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

func parseQueryReturn(raw string) queryReturn {
	r := queryReturn{Raw: strings.TrimSpace(raw)}
	if r.Raw == "" {
		return r
	}
	base := r.Raw
	if strings.HasPrefix(base, "[]") {
		r.IsList = true
		base = strings.TrimPrefix(base, "[]")
	}
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
		c.rulef("query-id-unique", "duplicate query id %s", id)
	}
	names := map[string]string{}
	for _, id := range c.queryOrder {
		q := c.queries[id]
		if strings.TrimSpace(q.Semantics) == "" {
			c.rulef("schema.non-empty", "%s: query %s semantics must not be empty", q.File, q.ID)
		}
		c.checkQueryIdentity(q, names)
		c.checkQueryShape(q)
		c.checkQueryTable(q)
		c.checkQueryValues(q)
		c.checkQueryWhere(q)
		c.checkQueryOrderBy(q)
		c.checkQueryParamColumns(q)
		c.checkQueryParams(q)
		c.checkQueryReturns(q)
		c.checkQueryOutputFields(q)
		c.checkQueryReturnColumns(q)
		c.checkQueryKind(q)
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
			if id, _ := m["id"].(string); id != "" {
				names[id] = true
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
	if !strings.HasPrefix(q.ID, q.FileID+".") {
		c.rulef("query-id-prefix", "%s: query id must start with %s., got %s", q.File, q.FileID, q.ID)
	}
	last := q.ID
	if i := strings.LastIndex(q.ID, "."); i >= 0 {
		last = q.ID[i+1:]
	}
	wantName := pascalCase(last)
	if q.Name != wantName {
		c.rulef("query-name", "%s: query %s name must be %s, got %s", q.File, q.ID, wantName, q.Name)
	}
	if q.Name == "" {
		return
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

// hasWhere 判断是否声明了任意形式的 WHERE（软表达式或结构化谓词）。
func hasWhere(q *queryInfo) bool {
	return len(q.Where) > 0 || len(q.Predicates) > 0 || len(q.BadWhere) > 0
}

// checkQueryShape 校验 statement 与子句/返回形态的匹配。
func (c *Checker) checkQueryShape(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	switch q.Statement {
	case "select":
		if len(q.Values) > 0 {
			c.rulef("query-shape", "%s: select must not declare values", where)
		}
	case "insert":
		if len(q.Values) == 0 {
			c.rulef("query-shape", "%s: insert requires values", where)
		}
		if hasWhere(q) || len(q.OrderBy) > 0 || q.Limit != "" {
			c.rulef("query-shape", "%s: insert must not declare where/order_by/limit", where)
		}
	case "update":
		if len(q.Values) == 0 || !hasWhere(q) {
			c.rulef("query-shape", "%s: update requires values and where", where)
		}
		if len(q.OrderBy) > 0 || q.Limit != "" {
			c.rulef("query-shape", "%s: update must not declare order_by/limit", where)
		}
	case "delete":
		if len(q.Values) > 0 {
			c.rulef("query-shape", "%s: delete must not declare values", where)
		}
		if !hasWhere(q) {
			c.rulef("query-shape", "%s: delete requires where (no full-table delete)", where)
		}
		if len(q.OrderBy) > 0 || q.Limit != "" {
			c.rulef("query-shape", "%s: delete must not declare order_by/limit", where)
		}
	default:
		c.rulef("query-shape", "%s: statement must be select/insert/update/delete", where)
		return
	}
	if len(q.Joins) > 0 && q.Statement != "select" {
		c.rulef("query-shape", "%s: joins are only allowed for select", where)
	}
	switch q.Return.Kind {
	case "scalar":
		if q.Return.Raw == "affected_rows" {
			if q.Expr != "" {
				c.rulef("query-shape", "%s: affected_rows must not declare expr", where)
			}
		} else {
			if q.Expr == "" {
				c.rulef("query-shape", "%s: scalar returns require expr", where)
			} else if sqlAliasSuffixRe.MatchString(q.Expr) {
				c.rulef("query-shape", "%s: expr must not declare AS alias", where)
			}
			if len(q.Values) > 0 {
				c.rulef("query-shape", "%s: scalar returns must not declare values", where)
			}
			if q.Statement != "select" {
				c.rulef("query-shape", "%s: scalar returns must use select", where)
			}
		}
	case "object", "table", "entity":
		if q.Expr != "" {
			c.rulef("query-shape", "%s: %s returns must not declare expr", where, q.Return.Kind)
		}
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
	if q.Statement != "select" && len(q.Targets) != 1 {
		c.rulef("query-table-shape", "%s: write statements require exactly one target", where)
	}
	joinSeen := map[string]bool{}
	for _, j := range q.Joins {
		if j.Table == "" || len(j.On) == 0 {
			c.rulef("query-table-shape", "%s: join requires table/on", where)
		}
		if joinSeen[j.Table] {
			c.rulef("query-table-shape", "%s: duplicate join table %s", where, j.Table)
		}
		joinSeen[j.Table] = true
		if !seen[j.Table] {
			c.rulef("query-table-shape", "%s: join table %s is not listed in targets", where, j.Table)
		}
		if j.Table == q.Targets[0] {
			c.rulef("query-table-shape", "%s: join table %s must not be the primary target", where, j.Table)
		}
	}
	if q.Statement == "select" {
		for _, t := range q.Targets[1:] {
			if !joinSeen[t] {
				c.rulef("query-table-shape", "%s: target %s has no join declaration", where, t)
			}
		}
	}
}

// checkQueryValues 校验写语句的列赋值：键必须是目标表列，表达式非空。
func (c *Checker) checkQueryValues(q *queryInfo) {
	if len(q.Values) == 0 {
		return
	}
	if len(q.Targets) == 0 {
		return // 缺失 targets 由 query-table-shape 报出，避免越界
	}
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	target := q.Targets[0]
	cols := make([]string, 0, len(q.Values))
	for col := range q.Values {
		cols = append(cols, col)
	}
	sort.Strings(cols)
	for _, col := range cols {
		if !c.columnInTables(col, []string{target}) {
			c.rulef("query-values", "%s: column %s not found in %s", where, col, target)
		}
		if strings.TrimSpace(q.Values[col]) == "" {
			c.rulef("query-values", "%s: value for %s must not be empty", where, col)
		}
	}
}

var predicateOps = map[string]bool{
	"eq": true, "ne": true, "lt": true, "lte": true, "gt": true, "gte": true,
	"in": true, "is_null": true, "is_not_null": true,
}

// checkQueryWhere 校验结构化谓词；字符串项仍是软表达式，不解析。
func (c *Checker) checkQueryWhere(q *queryInfo) {
	if len(q.Targets) == 0 {
		return // 缺失 targets 由 query-table-shape 报出
	}
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	for i, raw := range q.BadWhere {
		c.rulef("query-where", "%s: where[%d] must be a string expression or an object predicate, got %T", where, i, raw)
	}
	params := map[string]queryParam{}
	for _, p := range q.Params {
		params[p.Name] = p
	}
	for i, p := range q.Predicates {
		w := fmt.Sprintf("%s: where[%d]", where, i)
		allowed := map[string]bool{"column": true, "op": true, "param": true, "value": true}
		for k := range p.raw {
			if !allowed[k] {
				c.rulef("query-where", "%s: unknown predicate field %q", w, k)
			}
		}
		if p.Column == "" {
			c.rulef("query-where", "%s: column is required", w)
			continue
		}
		table, col, kind := c.resolveQueryColumn(q, p.Column)
		switch kind {
		case "missing":
			c.rulef("query-where", "%s: column %s not found in query tables", w, p.Column)
			continue
		case "ambiguous":
			c.rulef("query-where", "%s: column %s is ambiguous; use data.tables.<t>.col.<c>", w, p.Column)
			continue
		}
		meta, ok := c.tableColumns[table][col]
		if !ok {
			c.rulef("query-where", "%s: column %s not found", w, p.Column)
			continue
		}
		family := c.resolvedColumnType(meta.typ)
		if !predicateOps[p.Op] {
			c.rulef("query-where", "%s: unsupported op %q", w, p.Op)
			continue
		}
		switch p.Op {
		case "is_null", "is_not_null":
			if p.Param != "" || p.HasValue {
				c.rulef("query-where", "%s: %s must not declare param/value", w, p.Op)
			}
			if p.Op == "is_null" && !meta.nullable {
				c.rulef("query-where", "%s: is_null requires a nullable column", w)
			}
		case "in":
			if p.Param == "" || p.HasValue {
				c.rulef("query-where", "%s: in requires param and no value", w)
				continue
			}
			pp, ok := params[p.Param]
			if !ok {
				c.rulef("query-where", "%s: unknown param %s", w, p.Param)
				continue
			}
			if !strings.HasPrefix(pp.Type, "[]") {
				c.rulef("query-where", "%s: in requires an array param", w)
				continue
			}
			c.checkPredicateParam(pp, table, col, family, w)
		default:
			hasParam, hasValue := p.Param != "", p.HasValue
			if hasParam == hasValue {
				c.rulef("query-where", "%s: exactly one of param/value is required", w)
				continue
			}
			if hasParam {
				pp, ok := params[p.Param]
				if !ok {
					c.rulef("query-where", "%s: unknown param %s", w, p.Param)
					continue
				}
				if strings.HasPrefix(pp.Type, "[]") {
					c.rulef("query-where", "%s: comparison param must not be an array", w)
					continue
				}
				c.checkPredicateParam(pp, table, col, family, w)
				continue
			}
			if !valueMatchesFamily(p.Value, family) {
				c.rulef("query-where", "%s: value %v is not compatible with column type %s", w, p.Value, family)
			}
		}
	}
}

// checkPredicateParam 校验结构化谓词引用的参数：必须绑定同名列且类型族兼容。
func (c *Checker) checkPredicateParam(p queryParam, table, col, family, where string) {
	if p.Nullable {
		c.rulef("query-where", "%s: predicate param %s must not be nullable; use a string expression for optional filters", where, p.Name)
	}
	if p.Column == "" {
		c.rulef("query-where", "%s: param %s in a predicate must declare column", where, p.Name)
		return
	}
	t, ccol, ok := splitColumnRef(p.Column)
	if !ok || t != table || ccol != col {
		c.rulef("query-where", "%s: param %s column %s must match predicate column %s.col.%s", where, p.Name, p.Column, table, col)
		return
	}
	pfam, _, ok := paramFamily(p.Type)
	if !ok {
		c.rulef("query-param-column", "%s: param %s type %q cannot bind a column", where, p.Name, p.Type)
		return
	}
	if !familyCompatible(pfam, family) {
		c.rulef("query-param-column", "%s: param %s type %s is not compatible with column %s", where, p.Name, p.Type, family)
	}
}

// resolveQueryColumn 解析谓词列：裸列名要求唯一，或写完整 data.tables.<t>.col.<c>。
func (c *Checker) resolveQueryColumn(q *queryInfo, ref string) (table, col, kind string) {
	if strings.Contains(ref, ".col.") {
		t, ccol, ok := splitColumnRef(ref)
		if !ok || !q.tableSet()[t] {
			return "", "", "missing"
		}
		if _, ok := c.tableColumns[t][ccol]; !ok {
			return "", "", "missing"
		}
		return t, ccol, ""
	}
	tables := make([]string, 0, len(q.tableSet()))
	for t := range q.tableSet() {
		tables = append(tables, t)
	}
	sort.Strings(tables)
	found := ""
	for _, t := range tables {
		if _, ok := c.tableColumns[t][ref]; ok {
			if found != "" {
				return "", "", "ambiguous"
			}
			found = t
		}
	}
	if found == "" {
		return "", "", "missing"
	}
	return found, ref, ""
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

// paramFamily 把 sqlc 参数类型映射到 column_types 类型族；数组返回元素类型族。
func paramFamily(typ string) (family string, array bool, ok bool) {
	array = strings.HasPrefix(typ, "[]")
	base := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(typ, "[]")))
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

func valueMatchesFamily(v any, family string) bool {
	switch family {
	case "string", "bytes", "timestamp", "datetime":
		_, ok := v.(string)
		return ok
	case "integer":
		_, ok := toInt(v)
		return ok
	case "float", "decimal":
		if _, ok := toFloat(v); ok {
			return true
		}
		_, ok := toInt(v)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "json":
		return true
	}
	return false
}

var orderByRe = regexp.MustCompile(`(?i)^\s*([a-z_][a-z0-9_]*(?:\.[a-z_][a-z0-9_]*)?)\s+(asc|desc)\s*$`)

// checkQueryOrderBy 只允许 <列> ASC|DESC（可带 <表> 限定）；列必须属于查询的表。
func (c *Checker) checkQueryOrderBy(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	tables := q.tableSet()
	for _, item := range q.OrderBy {
		m := orderByRe.FindStringSubmatch(item)
		if m == nil {
			c.rulef("query-order-by", "%s: %q must be <column> ASC|DESC", where, item)
			continue
		}
		qualifier, col := "", m[1]
		if i := strings.LastIndex(col, "."); i >= 0 {
			qualifier, col = col[:i], col[i+1:]
		}
		if qualifier != "" {
			table := c.resolveOrderTable(q, qualifier)
			if table == "" {
				c.rulef("query-order-by", "%s: qualifier %s is not a query table", where, qualifier)
				continue
			}
			if _, ok := c.tableColumns[table][col]; !ok {
				c.rulef("query-order-by", "%s: order column %s not found in %s", where, col, table)
			}
			continue
		}
		matches := 0
		for t := range tables {
			if _, ok := c.tableColumns[t][col]; ok {
				matches++
			}
		}
		if matches == 0 {
			c.rulef("query-order-by", "%s: order column %s not found in query tables", where, col)
		} else if matches > 1 {
			c.rulef("query-order-by", "%s: order column %s is ambiguous; use <table>.<column>", where, col)
		}
	}
}

// resolveOrderTable 接受完整表 ID、data.tables 后缀或物理表名。
func (c *Checker) resolveOrderTable(q *queryInfo, qualifier string) string {
	for t := range q.tableSet() {
		if t == qualifier || strings.TrimPrefix(t, "data.tables.") == qualifier {
			return t
		}
		if table, ok := asMap(c.tableDocs[t]["table"]); ok {
			if name, _ := table["name"].(string); name != "" && name == qualifier {
				return t
			}
		}
	}
	return ""
}

func (c *Checker) checkQueryParams(q *queryInfo) {
	seen := map[string]bool{}
	for _, p := range q.Params {
		if seen[p.Name] {
			c.rulef("query-params", "%s: query %s duplicate param %s", q.File, q.ID, p.Name)
		}
		seen[p.Name] = true
		if strings.TrimSpace(p.Type) == "" {
			c.rulef("schema.non-empty", "%s: query %s param %s type must not be empty", q.File, q.ID, p.Name)
		}
	}
	uses := sqlParamUses(q.exprText())
	predUses := map[string]bool{}
	for _, p := range q.Predicates {
		if p.Param != "" {
			predUses[p.Param] = true
		}
	}
	for _, p := range q.Params {
		u, ok := uses[p.Name]
		if !ok {
			// 结构化谓词由生成器按列/op 拼接 SQL；nullable 已在 checkQueryWhere 拒绝，
			// 因此这里只需确认参数被谓词引用，不扫描 narg/arg 文本。
			if !predUses[p.Name] {
				c.rulef("query-param-sql", "%s: query %s param %s is not used in expressions", q.File, q.ID, p.Name)
			}
			continue
		}
		if p.Nullable {
			if !u.narg || u.at || u.arg {
				c.rulef("query-param-nullable", "%s: query %s nullable param %s must use sqlc.narg only", q.File, q.ID, p.Name)
			}
			continue
		}
		if u.narg {
			c.rulef("query-param-nullable", "%s: query %s non-nullable param %s must not use sqlc.narg", q.File, q.ID, p.Name)
		}
	}
	for name := range uses {
		if !seen[name] {
			c.rulef("query-param-sql", "%s: query %s param %s is not declared", q.File, q.ID, name)
		}
	}
}

type sqlParamUse struct {
	at, arg, narg bool
}

func sqlParamUses(sql string) map[string]sqlParamUse {
	raw := stripSQLComments(sql)
	out := map[string]sqlParamUse{}
	// sqlc.narg/arg 的引号是函数语法，不能按字符串字面量剥离。
	for _, m := range sqlParamNargRe.FindAllStringSubmatch(raw, -1) {
		u := out[m[1]]
		u.narg = true
		out[m[1]] = u
	}
	for _, m := range sqlParamArgRe.FindAllStringSubmatch(raw, -1) {
		u := out[m[1]]
		u.arg = true
		out[m[1]] = u
	}
	// @name 扫描避开真正的字符串字面量。
	for _, m := range sqlParamAtRe.FindAllStringSubmatch(stripSQLStrings(raw), -1) {
		u := out[m[1]]
		u.at = true
		out[m[1]] = u
	}
	return out
}

func stripSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// stripSQLStrings 去掉单引号字符串字面量，避免参数/表名扫描把字符串内容当 SQL。
func stripSQLStrings(sql string) string {
	var b strings.Builder
	b.Grow(len(sql))
	inString := false
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		if c == '\'' {
			if inString && i+1 < len(sql) && sql[i+1] == '\'' {
				i++ // 转义的单引号
				continue
			}
			inString = !inString
			continue
		}
		if !inString {
			b.WriteByte(c)
		}
	}
	return b.String()
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
		if c.entities[q.Return.EntityID] == nil {
			c.rulef("query-returns", "%s: returns unknown entity %s", where, q.Return.EntityID)
			return
		}
	case "object":
		// output_fields 在 checkQueryOutputFields 校验
	}
	switch {
	case q.Kind == "many" && !q.Return.IsList:
		c.rulef("query-returns-kind", "%s: kind many requires [] returns", where)
	case q.Kind == "one" && q.Return.IsList:
		c.rulef("query-returns-kind", "%s: kind one must not return a list", where)
	case q.Kind == "execrows" && q.Return.Raw != "affected_rows":
		c.rulef("query-returns-kind", "%s: kind execrows requires affected_rows", where)
	case q.Return.Raw == "affected_rows" && q.Kind != "execrows":
		c.rulef("query-returns-kind", "%s: affected_rows requires kind execrows", where)
	case q.Kind != "execrows" && q.Statement != "" && q.Statement != "select":
		if q.Return.Kind != "table" && q.Return.Kind != "entity" {
			c.rulef("query-returns-kind", "%s: write with kind %s must return a table row or entity", where, q.Kind)
		}
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
		if f.Type == "" {
			c.rulef("query-output-fields", "%s: output field %s missing type", where, f.Name)
		} else if !strings.HasPrefix(f.Type, "data.enums.") && !c.isEntityType(f.Type) {
			c.rulef("query-output-fields", "%s: output field %s has unsupported type %s", where, f.Name, f.Type)
		}
		if strings.TrimSpace(f.Expr) == "" {
			c.rulef("query-output-fields", "%s: output field %s missing expr", where, f.Name)
		} else if sqlAliasSuffixRe.MatchString(f.Expr) {
			c.rulef("query-output-fields", "%s: output field %s expr must not declare AS alias; use the field name", where, f.Name)
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

func (c *Checker) checkQueryKind(q *queryInfo) {
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	if q.Kind == "one" && q.Statement == "select" && (q.Return.Kind == "table" || q.Return.Kind == "entity") {
		table := q.Return.TableID
		if q.Return.Kind == "entity" {
			if e := c.entities[q.Return.EntityID]; e != nil {
				table = e.Primary
			}
		}
		if q.Limit != "1" && (table == "" || !c.hasUniqueIndexRef(q, table)) {
			c.rulef("query-kind-sql", "%s: kind one requires a unique index ref on %s or limit 1", where, table)
		}
	}
	if q.Kind == "execrows" && q.Statement == "select" {
		c.rulef("query-kind-sql", "%s: kind execrows must write, got select", where)
	}
	if q.Statement != "select" && q.OwnerModule != "" {
		if len(q.Targets) != 1 {
			return // 缺失/多目标由 query-table-shape 报出
		}
		target := q.Targets[0]
		if owner := c.tableOwners[target]; owner != "" && owner != q.OwnerModule {
			c.rulef("query-write-owner", "%s: owner_module %s cannot write %s (owner_module %s)", where, q.OwnerModule, target, owner)
		}
	}
}

// hasUniqueIndexRef 判断 one 查询是否命中指定表的主键或唯一索引。
func (c *Checker) hasUniqueIndexRef(q *queryInfo, table string) bool {
	if table == "" {
		return false
	}
	for _, ref := range q.IndexRef {
		t, name, ok := splitIndexRef(ref)
		if !ok || t != table {
			continue
		}
		if name == "pk" {
			return true
		}
		for _, idx := range c.tableIndexes[table] {
			if idx.name == name && idx.unique {
				return true
			}
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

func (c *Checker) columnInTables(col string, tables []string) bool {
	for _, t := range tables {
		if _, ok := c.tableColumns[t][col]; ok {
			return true
		}
	}
	return false
}

func sqlHasWord(sql, word string) bool {
	if word == "" {
		return false
	}
	re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(word) + `\b`)
	// 只去注释：列名/表名可能出现在 sqlc.narg('x') 的引号里，剥字符串会漏检。
	return re.MatchString(stripSQLComments(sql))
}

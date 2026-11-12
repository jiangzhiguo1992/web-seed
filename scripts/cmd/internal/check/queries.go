// queries.go 校验 data.queries：ID/名称、表、参数、返回、SQL 形态与写归属。
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
	FileID        string
	File          string
	Module        string
	Kind          string
	Tables        []string
	HasTable      bool
	HasTables     bool
	Params        []queryParam
	Return        queryReturn
	ReturnColumns []string
	OutputFields  []queryOutputField
	IndexColumns  [][]string
	Semantics     string
	SQL           querySQL
	Doc           map[string]any
}

// querySQL 是结构化 SQL：子句是字符串表达式，表和返回列是结构化引用。
type querySQL struct {
	Statement string
	From      string
	Into      string
	Distinct  bool
	Select    []string
	Where     []string
	OrderBy   []string
	Limit     string
	Columns   []string
	Values    []string
	Set       []string
	Returning []string
	Joins     []queryJoin
	Raw       string
}

type queryJoin struct {
	Table string
	Type  string
	On    []string
}

type queryDoc struct {
	rel    string
	fileID string
	doc    map[string]any
}

var sqlParamAtRe = regexp.MustCompile(`@([a-z_][a-z0-9_]*)`)
var sqlParamNargRe = regexp.MustCompile(`sqlc\.narg\('([a-z_][a-z0-9_]*)'\)`)
var sqlParamArgRe = regexp.MustCompile(`sqlc\.arg\('([a-z_][a-z0-9_]*)'\)`)

var sqlScalarReturns = map[string]bool{
	"boolean": true, "integer": true, "string": true, "float": true,
	"decimal": true, "timestamp": true, "datetime": true, "json": true,
}

// loadQueryIndex 解析查询文件；结构错误在 checkQueries 中报。
func (c *Checker) loadQueryIndex(schema *Schema) {
	c.queries = map[string]*queryInfo{}
	c.queryDocs = nil
	c.queryOrder = nil
	c.queryDupIDs = map[string]bool{}
	c.queryIDs = map[string]string{}
	c.queryTables = map[string]map[string]bool{}
	c.querySQL = map[string]string{}
	c.queryIndexCols = map[string][][]string{}
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
		module, _ := doc["module"].(string)
		for _, raw := range asList(doc["queries"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			q := parseQuery(m, rel, fileID, module)
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
			c.querySQL[q.ID] = q.SQL.Text()
			c.queryIndexCols[q.ID] = q.IndexColumns
			tables := map[string]bool{}
			for _, t := range q.Tables {
				tables[t] = true
			}
			c.queryTables[q.ID] = tables
		}
	}
}

func parseQuery(m map[string]any, rel, fileID, module string) *queryInfo {
	q := &queryInfo{File: rel, FileID: fileID, Module: module, Doc: m}
	q.ID, _ = m["id"].(string)
	q.Name, _ = m["name"].(string)
	q.Kind, _ = m["kind"].(string)
	q.Semantics, _ = m["semantics"].(string)
	q.SQL = parseQuerySQL(m["sql"])
	q.HasTable = false
	q.HasTables = false
	if s, ok := m["table"].(string); ok && s != "" {
		q.HasTable = true
	}
	if _, ok := m["tables"]; ok {
		q.HasTables = true
	}
	q.Tables = tableRefs(m)
	for _, raw := range asList(m["params"]) {
		pm, ok := asMap(raw)
		if !ok {
			continue
		}
		p := queryParam{}
		p.Name, _ = pm["name"].(string)
		p.Type, _ = pm["type"].(string)
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
	q.IndexColumns = stringGroups(m["index_columns"])
	return q
}

func parseQuerySQL(v any) querySQL {
	m, ok := asMap(v)
	if !ok {
		if s, ok := v.(string); ok {
			return querySQL{Raw: s}
		}
		return querySQL{}
	}
	q := querySQL{
		Statement: stringAt(m, "statement"),
		From:      stringAt(m, "from"),
		Into:      stringAt(m, "into"),
		Limit:     stringAt(m, "limit"),
		Select:    stringList(m["select"]),
		Where:     stringList(m["where"]),
		OrderBy:   stringList(m["order_by"]),
		Columns:   stringList(m["columns"]),
		Values:    stringList(m["values"]),
		Set:       stringList(m["set"]),
		Returning: stringList(m["returning"]),
	}
	q.Distinct, _ = m["distinct"].(bool)
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
	return q
}

func stringAt(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return strings.TrimSpace(s)
}

// Text 把所有表达式拼成一段，供参数/词法级校验使用；不是可执行 SQL。
func (q querySQL) Text() string {
	if q.Raw != "" {
		return q.Raw
	}
	var parts []string
	parts = append(parts, q.Select...)
	parts = append(parts, q.Where...)
	parts = append(parts, q.OrderBy...)
	parts = append(parts, q.Columns...)
	parts = append(parts, q.Values...)
	parts = append(parts, q.Set...)
	parts = append(parts, q.Returning...)
	for _, j := range q.Joins {
		parts = append(parts, j.On...)
	}
	if q.Limit != "" {
		parts = append(parts, q.Limit)
	}
	return strings.Join(parts, "\n")
}

// TableRefs 返回 SQL 子句声明的表；from 是主表，into 用于 INSERT。
func (q querySQL) TableRefs() []string {
	out := []string{}
	if q.From != "" {
		out = append(out, q.From)
	}
	if q.Into != "" {
		out = append(out, q.Into)
	}
	for _, j := range q.Joins {
		if j.Table != "" {
			out = append(out, j.Table)
		}
	}
	return out
}

// SelectedColumns 返回 select/returning 中的纯列名；表达式不参与实体覆盖判断。
func (q querySQL) SelectedColumns() []string {
	out := []string{}
	for _, item := range append(append([]string{}, q.Select...), q.Returning...) {
		if simpleSQLIdent(item) {
			out = append(out, item)
		}
	}
	return out
}

// SelectAliases 返回 select/returning 中的输出名：纯列名或 AS 别名。
func (q querySQL) SelectAliases() []string {
	out := []string{}
	for _, item := range append(append([]string{}, q.Select...), q.Returning...) {
		item = strings.TrimSpace(item)
		if simpleSQLIdent(item) {
			out = append(out, item)
			continue
		}
		if _, alias, ok := splitSQLAlias(item); ok {
			out = append(out, alias)
		}
	}
	return out
}

var simpleSQLIdentRe = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

func simpleSQLIdent(s string) bool {
	return simpleSQLIdentRe.MatchString(strings.TrimSpace(s))
}

var sqlAliasRe = regexp.MustCompile(`(?is)^(.+?)\s+as\s+([a-z_][a-z0-9_]*)$`)

func splitSQLAlias(s string) (expr, alias string, ok bool) {
	m := sqlAliasRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", "", false
	}
	return strings.TrimSpace(m[1]), m[2], true
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

func (c *Checker) checkQueries(schema *Schema) {
	c.checkTargets(schema)
	for _, qd := range c.queryDocs {
		c.checkTargetFileName(schema, filepath.Base(qd.rel), qd.rel)
		c.validateDoc(qd.doc, schema.Fields, qd.rel)
		c.checkStability(qd.doc, qd.rel)
		if s, _ := qd.doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", qd.rel)
		}
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
		if strings.TrimSpace(q.SQL.Text()) == "" && strings.TrimSpace(q.SQL.Raw) == "" {
			c.rulef("schema.non-empty", "%s: query %s sql must not be empty", q.File, q.ID)
		}
		if strings.TrimSpace(q.Semantics) == "" {
			c.rulef("schema.non-empty", "%s: query %s semantics must not be empty", q.File, q.ID)
		}
		c.checkQueryIdentity(q, names)
		c.checkQuerySQLShape(q)
		c.checkQueryTable(q)
		c.checkQueryParams(q)
		c.checkQueryIndexSQL(q)
		c.checkQueryReturns(q)
		c.checkQueryReturnColumns(q)
		c.checkQueryKindSQL(q)
	}
}

func (c *Checker) checkQueryDoNot(qd queryDoc) {
	short := strings.TrimSuffix(filepath.Base(qd.rel), filepath.Ext(qd.rel))
	names := map[string]bool{short: true, qd.fileID: true}
	if m, _ := qd.doc["module"].(string); m != "" {
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

// checkQuerySQLShape 校验 sql 对象：statement 与子句匹配、表和列存在、
// select/returning 对表/实体返回必须是纯列名、禁止 SELECT *。
func (c *Checker) checkQuerySQLShape(q *queryInfo) {
	s := q.SQL
	if s.Raw != "" {
		c.rulef("query-sql-shape", "%s: query %s sql must be an object, not a string", q.File, q.ID)
		return
	}
	where := fmt.Sprintf("%s: query %s", q.File, q.ID)
	switch s.Statement {
	case "select":
		if s.From == "" || len(s.Select) == 0 {
			c.rulef("query-sql-shape", "%s: select requires from and select", where)
		}
		if s.Into != "" || len(s.Columns) > 0 || len(s.Values) > 0 || len(s.Set) > 0 || len(s.Returning) > 0 {
			c.rulef("query-sql-shape", "%s: select must not declare insert/update/returning fields", where)
		}
	case "insert":
		if s.Into == "" || len(s.Columns) == 0 || len(s.Values) == 0 {
			c.rulef("query-sql-shape", "%s: insert requires into/columns/values", where)
		}
		if s.From != "" || len(s.Select) > 0 || len(s.Set) > 0 {
			c.rulef("query-sql-shape", "%s: insert must not declare from/select/set", where)
		}
		if len(s.Columns) != len(s.Values) {
			c.rulef("query-sql-shape", "%s: insert columns/values length mismatch (%d/%d)", where, len(s.Columns), len(s.Values))
		}
	case "update":
		if s.From == "" || len(s.Set) == 0 {
			c.rulef("query-sql-shape", "%s: update requires from/set", where)
		}
		if len(s.Where) == 0 {
			c.rulef("query-sql-shape", "%s: update requires where (no full-table update)", where)
		}
		if s.Into != "" || len(s.Select) > 0 || len(s.Columns) > 0 || len(s.Values) > 0 {
			c.rulef("query-sql-shape", "%s: update must not declare into/select/columns/values", where)
		}
	case "delete":
		if s.From == "" || len(s.Where) == 0 {
			c.rulef("query-sql-shape", "%s: delete requires from/where", where)
		}
		if s.Into != "" || len(s.Select) > 0 || len(s.Columns) > 0 || len(s.Values) > 0 || len(s.Set) > 0 {
			c.rulef("query-sql-shape", "%s: delete must not declare into/select/columns/values/set", where)
		}
	default:
		c.rulef("query-sql-shape", "%s: statement must be select/insert/update/delete", where)
		return
	}
	if s.Distinct && s.Statement != "select" {
		c.rulef("query-sql-shape", "%s: distinct is only allowed for select", where)
	}
	if s.Limit != "" && s.Statement != "select" {
		c.rulef("query-sql-shape", "%s: limit is only allowed for select", where)
	}

	declared := map[string]bool{}
	for _, t := range q.Tables {
		declared[t] = true
	}
	sqlTables := map[string]bool{}
	for _, t := range s.TableRefs() {
		if t == c.baseID {
			c.rulef("query-table-shape", "%s: sql references data.tables.base", where)
			continue
		}
		if _, ok := c.tableColumns[t]; !ok {
			c.rulef("query-sql-shape", "%s: sql table %s not found", where, t)
			continue
		}
		if !declared[t] {
			c.rulef("query-table-sql", "%s: sql table %s is not declared in table/tables", where, t)
		}
		sqlTables[t] = true
	}
	for _, t := range q.Tables {
		if !sqlTables[t] {
			c.rulef("query-table-sql", "%s: declared table %s does not appear in sql", where, t)
		}
	}

	target := s.From
	if target == "" {
		target = s.Into
	}
	plainRequired := q.Return.Kind == "table" || q.Return.Kind == "entity"
	for _, item := range append(append([]string{}, s.Select...), s.Returning...) {
		if hasSelectStar(item) {
			c.rulef("query-no-select-star", "%s: select/returning must not use *", where)
		}
		if plainRequired && !simpleSQLIdent(item) {
			c.rulef("query-sql-shape", "%s: table/entity returns require plain column select/returning, got %q", where, item)
		}
	}
	for _, col := range s.SelectedColumns() {
		if !c.columnInTables(col, s.TableRefs()) {
			c.rulef("query-sql-shape", "%s: selected column %s not found in sql tables", where, col)
		}
	}
	for _, col := range s.Columns {
		if target != "" && !c.columnInTables(col, []string{target}) {
			c.rulef("query-sql-shape", "%s: insert column %s not found in %s", where, col, target)
		}
	}
	for _, assignment := range s.Set {
		col := strings.TrimSpace(strings.SplitN(assignment, "=", 2)[0])
		if !simpleSQLIdent(col) {
			c.rulef("query-sql-shape", "%s: update set must start with a column name, got %q", where, assignment)
			continue
		}
		if target != "" && !c.columnInTables(col, []string{target}) {
			c.rulef("query-sql-shape", "%s: update column %s not found in %s", where, col, target)
		}
	}
	for _, j := range s.Joins {
		if j.Table == "" || len(j.On) == 0 {
			c.rulef("query-sql-shape", "%s: join requires table/on", where)
		}
	}
}

func hasSelectStar(item string) bool {
	clean := strings.TrimSpace(item)
	if strings.Contains(strings.ToLower(clean), "count(*)") {
		return false
	}
	return clean == "*" || strings.HasPrefix(clean, "*") || strings.HasSuffix(clean, ".*") || strings.Contains(clean, ", *")
}

func (c *Checker) columnInTables(col string, tables []string) bool {
	for _, t := range tables {
		if _, ok := c.tableColumns[t][col]; ok {
			return true
		}
	}
	return false
}

func (c *Checker) checkQueryTable(q *queryInfo) {
	if q.HasTable == q.HasTables {
		c.rulef("query-table-shape", "%s: query %s requires exactly one of table/tables", q.File, q.ID)
	}
	if len(q.Tables) == 0 {
		c.rulef("query-table-shape", "%s: query %s table/tables must not be empty", q.File, q.ID)
		return
	}
	seen := map[string]bool{}
	for _, t := range q.Tables {
		if t == c.baseID {
			c.rulef("query-table-shape", "%s: query %s references data.tables.base", q.File, q.ID)
			continue
		}
		if _, ok := c.tableColumns[t]; !ok {
			c.rulef("query-table-shape", "%s: query %s table %s not found", q.File, q.ID, t)
			continue
		}
		if seen[t] {
			c.rulef("query-table-shape", "%s: query %s duplicate table %s", q.File, q.ID, t)
		}
		seen[t] = true
	}
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
	uses := sqlParamUses(q.SQL.Text())
	for _, p := range q.Params {
		u, ok := uses[p.Name]
		if !ok {
			c.rulef("query-param-sql", "%s: query %s param %s is not used in SQL", q.File, q.ID, p.Name)
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
	declared := map[string]bool{}
	for _, p := range q.Params {
		declared[p.Name] = true
	}
	for name := range uses {
		if !declared[name] {
			c.rulef("query-param-sql", "%s: query %s SQL param %s is not declared", q.File, q.ID, name)
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

// checkQueryIndexSQL 防止 index_columns 声明了 SQL 根本没用的列。
func (c *Checker) checkQueryIndexSQL(q *queryInfo) {
	for _, group := range q.IndexColumns {
		for _, col := range group {
			if !sqlHasWord(q.SQL.Text(), col) {
				c.rulef("query-index-columns", "%s: query %s index_columns references %s but SQL never mentions it", q.File, q.ID, col)
			}
		}
	}
}

func (c *Checker) checkQueryReturns(q *queryInfo) {
	switch q.Return.Kind {
	case "invalid", "":
		c.rulef("query-returns", "%s: query %s has invalid returns %q", q.File, q.ID, q.Return.Raw)
		return
	case "table":
		if _, ok := c.tableColumns[q.Return.TableID]; !ok {
			c.rulef("query-returns", "%s: query %s returns unknown table %s", q.File, q.ID, q.Return.TableID)
			return
		}
		target := q.SQL.From
		if target == "" {
			target = q.SQL.Into
		}
		if len(q.Tables) != 1 || q.Tables[0] != q.Return.TableID || target != q.Return.TableID {
			c.rulef("query-returns", "%s: query %s table returns must match its single table %s", q.File, q.ID, q.Return.TableID)
		}
	case "entity":
		if c.entities[q.Return.EntityID] == nil {
			c.rulef("query-returns", "%s: query %s returns unknown entity %s", q.File, q.ID, q.Return.EntityID)
			return
		}
	case "object":
		// output_fields 在学生效验
	}
	switch {
	case q.Kind == "many" && !q.Return.IsList:
		c.rulef("query-returns-kind", "%s: query %s kind many requires [] returns", q.File, q.ID)
	case q.Kind == "one" && q.Return.IsList:
		c.rulef("query-returns-kind", "%s: query %s kind one must not return a list", q.File, q.ID)
	case q.Kind == "execrows" && q.Return.Raw != "affected_rows":
		c.rulef("query-returns-kind", "%s: query %s kind execrows requires affected_rows", q.File, q.ID)
	case q.Return.Raw == "affected_rows" && q.Kind != "execrows":
		c.rulef("query-returns-kind", "%s: query %s affected_rows requires kind execrows", q.File, q.ID)
	}
}

func (c *Checker) checkQueryReturnColumns(q *queryInfo) {
	if len(q.OutputFields) > 0 {
		if q.Return.Kind != "object" {
			c.rulef("query-output-fields", "%s: query %s output_fields requires returns object", q.File, q.ID)
		}
		if len(q.ReturnColumns) > 0 {
			c.rulef("query-output-fields", "%s: query %s must not combine output_fields with return_columns", q.File, q.ID)
		}
		aliases := map[string]bool{}
		for _, a := range q.SQL.SelectAliases() {
			aliases[a] = true
		}
		seen := map[string]bool{}
		for _, f := range q.OutputFields {
			if seen[f.Name] {
				c.rulef("query-output-fields", "%s: query %s duplicate output field %s", q.File, q.ID, f.Name)
			}
			seen[f.Name] = true
			switch {
			case f.Type == "":
				c.rulef("query-output-fields", "%s: query %s output field %s missing type", q.File, q.ID, f.Name)
			case strings.HasPrefix(f.Type, "data.enums."):
				// 枚举存在性由全局 enum 引用扫描校验。
			case !entityScalarType(f.Type):
				c.rulef("query-output-fields", "%s: query %s output field %s has unsupported type %s", q.File, q.ID, f.Name, f.Type)
			}
			if f.Name != "" && !aliases[f.Name] {
				c.rulef("query-output-fields", "%s: query %s output field %s is not selected in SQL", q.File, q.ID, f.Name)
			}
		}
	}
	if q.Return.Kind == "object" && len(q.OutputFields) == 0 {
		c.rulef("query-output-fields", "%s: query %s returns object but output_fields is empty", q.File, q.ID)
	}
	selected := q.SQL.SelectedColumns()
	selectedSet := map[string]bool{}
	for _, col := range selected {
		selectedSet[col] = true
	}
	if len(q.ReturnColumns) == 0 {
		c.checkSensitiveWithoutColumns(q, selectedSet)
		c.checkEntityCoverage(q, selected, selected)
		return
	}
	switch q.Return.Kind {
	case "table":
		cols := c.tableColumns[q.Return.TableID]
		for _, col := range q.ReturnColumns {
			meta, ok := cols[col]
			if !ok {
				c.rulef("query-return-columns", "%s: query %s return column %s not found in %s", q.File, q.ID, col, q.Return.TableID)
				continue
			}
			if !selectedSet[col] {
				c.rulef("query-return-columns", "%s: query %s return column %s is not selected in SQL", q.File, q.ID, col)
			}
			if meta.neverExpose && q.Module != c.tableOwners[q.Return.TableID] {
				c.rulef("query-sensitive-columns", "%s: query %s returns never_expose column %s outside module %s", q.File, q.ID, col, c.tableOwners[q.Return.TableID])
			}
		}
	case "entity":
		e := c.entities[q.Return.EntityID]
		if e == nil {
			return
		}
		for _, col := range q.ReturnColumns {
			f := findEntityField(e, col)
			if f == nil {
				c.rulef("query-return-columns", "%s: query %s return field %s not found in %s", q.File, q.ID, col, e.ID)
				continue
			}
			if !selectedSet[col] {
				c.rulef("query-return-columns", "%s: query %s return field %s is not selected in SQL", q.File, q.ID, col)
			}
			if f.NeverExpose && q.Module != c.tableOwners[f.ColumnTable] {
				c.rulef("query-sensitive-columns", "%s: query %s returns never_expose field %s outside module %s", q.File, q.ID, col, c.tableOwners[f.ColumnTable])
			}
		}
		c.checkEntityCoverage(q, selected, q.ReturnColumns)
	default:
		c.rulef("query-return-columns", "%s: query %s return_columns is only allowed for table/entity returns", q.File, q.ID)
	}
}

func (c *Checker) checkSensitiveWithoutColumns(q *queryInfo, selected map[string]bool) {
	switch q.Return.Kind {
	case "table":
		for col, meta := range c.tableColumns[q.Return.TableID] {
			if meta.neverExpose && selected[col] {
				c.rulef("query-sensitive-columns", "%s: query %s must list return_columns to exclude never_expose column %s", q.File, q.ID, col)
			}
		}
	case "entity":
		e := c.entities[q.Return.EntityID]
		if e == nil {
			return
		}
		for _, f := range e.Fields {
			if f.NeverExpose && selected[f.Name] {
				c.rulef("query-sensitive-columns", "%s: query %s must list return_columns to exclude never_expose field %s", q.File, q.ID, f.Name)
			}
		}
	}
}

func (c *Checker) checkEntityCoverage(q *queryInfo, selected, required []string) {
	e := c.entities[q.Return.EntityID]
	if e == nil {
		return
	}
	sel := map[string]bool{}
	for _, s := range selected {
		sel[s] = true
	}
	req := map[string]bool{}
	for _, s := range required {
		req[s] = true
	}
	for _, f := range e.Fields {
		if !sel[f.Name] {
			continue
		}
		if f.ColumnTable != "" && !containsString(q.Tables, f.ColumnTable) {
			c.rulef("query-entity-coverage", "%s: query %s returns %s but does not read its source table %s", q.File, q.ID, e.ID, f.ColumnTable)
		}
	}
	for _, f := range e.Fields {
		if f.Required && !f.NeverExpose && !req[f.Name] {
			c.rulef("query-entity-coverage", "%s: query %s omits required field %s", q.File, q.ID, f.Name)
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

func (c *Checker) checkQueryKindSQL(q *queryInfo) {
	s := q.SQL
	switch q.Kind {
	case "one", "many":
		if s.Statement != "select" && len(s.Returning) == 0 {
			c.rulef("query-kind-sql", "%s: query %s kind %s write SQL must use RETURNING", q.File, q.ID, q.Kind)
		}
	case "execrows":
		if s.Statement == "select" || s.Statement == "" {
			c.rulef("query-kind-sql", "%s: query %s kind execrows must write, got %s", q.File, q.ID, s.Statement)
		}
	}
	if q.Kind == "one" && (q.Return.Kind == "table" || q.Return.Kind == "entity") && s.Statement == "select" {
		if s.Limit != "1" && !c.hasUniquePrefix(q) {
			c.rulef("query-one-unique", "%s: query %s kind one requires a unique index prefix or limit 1", q.File, q.ID)
		}
	}
	if isWriteQuery(q) && q.Module != "" {
		target := s.Into
		if s.Statement != "insert" {
			target = s.From
		}
		if target == "" {
			c.rulef("query-write-owner", "%s: query %s cannot determine write target", q.File, q.ID)
			return
		}
		if !containsString(q.Tables, target) {
			c.rulef("query-write-owner", "%s: query %s write target %s is not declared in table/tables", q.File, q.ID, target)
			return
		}
		if owner := c.tableOwners[target]; owner != "" && owner != q.Module {
			c.rulef("query-write-owner", "%s: query %s module %s cannot write %s (owner_module %s)", q.File, q.ID, q.Module, target, owner)
		}
	}
}

func isWriteQuery(q *queryInfo) bool {
	return q.SQL.Statement != "" && q.SQL.Statement != "select"
}

func (c *Checker) hasUniquePrefix(q *queryInfo) bool {
	if len(q.Tables) != 1 {
		return false
	}
	tableID := q.Tables[0]
	for _, group := range q.IndexColumns {
		if hasPrefix(group, c.tablePrimaryKey[tableID]) {
			return true
		}
		for _, idx := range c.tableIndexes[tableID] {
			if idx.unique && hasPrefix(group, idx.columns) {
				return true
			}
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

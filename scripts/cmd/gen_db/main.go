// Command gen_db 从 contracts/data/tables 生成 PostgreSQL 目标 schema 与初始迁移基线。
//
// 输出：
//   - apps/api/schema.sql：当前契约的 PostgreSQL 目标态 DDL（sqlc 输入）
//   - apps/api/migrations/0001_baseline.sql：空库基线（goose Up/Down）
//   - apps/api/internal/generated/constraint_errors_gen.go：CHECK 约束 → 错误码映射
//
// 迁移文件是生成物，禁止手改；表结构变更必须改 contracts/data/tables 后重新生
// 成。live_only 只用于唯一索引，用 partial unique index（WHERE deleted_at IS NULL）
// 把软删行排除在唯一性之外。
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

const (
	schemaPath           = "apps/api/schema.sql"
	migrationPath        = "apps/api/migrations/0001_baseline.sql"
	constraintErrorsPath = "apps/api/internal/generated/constraint_errors_gen.go"
)

func main() {
	root := flag.String("root", ".", "repository root")
	check := flag.Bool("check", false, "verify generated files are up to date")
	flag.Parse()
	if err := run(*root, *check); err != nil {
		fmt.Fprintln(os.Stderr, "gen-db:", err)
		os.Exit(1)
	}
}

func run(root string, check bool) error {
	schema, _, err := contractspec.LoadSchemaByType(root, "data.tables")
	if err != nil {
		return err
	}
	tables, err := loadTables(root, schema.Targets, schema.ValueTypes.CustomTypes)
	if err != nil {
		return err
	}
	postgresTables, err := loadEngineTables(root, "postgres")
	if err != nil {
		return err
	}
	filtered := make([]table, 0, len(tables))
	for _, t := range tables {
		if postgresTables[t.ID] {
			filtered = append(filtered, t)
		}
	}
	tables = filtered
	if err := validatePGTypes(root, tables); err != nil {
		return err
	}
	schemaSQL, migrationSQL, err := render(tables)
	if err != nil {
		return err
	}
	constraintErrorsGo := renderConstraintErrors(tables)
	if check {
		for _, f := range []struct {
			path string
			want []byte
		}{
			{filepath.Join(root, schemaPath), schemaSQL},
			{filepath.Join(root, migrationPath), migrationSQL},
			{filepath.Join(root, constraintErrorsPath), constraintErrorsGo},
		} {
			got, err := os.ReadFile(f.path)
			if err != nil {
				return fmt.Errorf("%s: %w", f.path, err)
			}
			if string(got) != string(f.want) {
				return fmt.Errorf("%s is stale; run make gen-db", f.path)
			}
		}
		return nil
	}
	if err := writeFile(filepath.Join(root, schemaPath), schemaSQL); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, migrationPath), migrationSQL); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(root, constraintErrorsPath), constraintErrorsGo); err != nil {
		return err
	}
	fmt.Printf("generated %s\n", schemaPath)
	fmt.Printf("generated %s\n", migrationPath)
	fmt.Printf("generated %s\n", constraintErrorsPath)
	return nil
}

// validatePGTypes 是 DDL 的硬约束：生成出的每个 PostgreSQL 类型都必须
// 出现在 _types.yaml 某个 basics 族的 pg 兼容集合里；新增生成分支而未登记会直接失败。
func validatePGTypes(root string, tables []table) error {
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts", "_meta"))
	if err != nil {
		return err
	}
	declared := map[string]bool{}
	for _, fam := range shared.TypeFamilies {
		for _, ct := range fam.Types {
			for _, pg := range ct.PG {
				declared[strings.ToLower(strings.TrimSpace(pg))] = true
			}
		}
	}
	for _, t := range tables {
		for _, c := range t.Columns {
			sqlType, err := columnType(c)
			if err != nil {
				return err
			}
			base := strings.ToLower(strings.TrimSpace(strings.SplitN(sqlType, "(", 2)[0]))
			if !declared[base] {
				return fmt.Errorf("%s.%s: PostgreSQL type %q is not declared in contracts/_meta/_types.yaml pg sets", t.ID, c.Name, base)
			}
		}
	}
	return nil
}

// loadEngineTables 读取 storage.yaml 中某个引擎的权威表 ID，DDL 只生成该引擎的表。
func loadEngineTables(root, engine string) (map[string]bool, error) {
	doc, err := loadYAML(filepath.Join(root, "contracts/data/storage.yaml"))
	if err != nil {
		return nil, err
	}
	rawTables, ok := asMap(doc["tables"])
	if !ok {
		return nil, fmt.Errorf("contracts/data/storage.yaml: tables must be a map")
	}
	rawList, ok := rawTables[engine]
	if !ok {
		return nil, fmt.Errorf("contracts/data/storage.yaml: tables.%s is required", engine)
	}
	list, ok := rawList.([]any)
	if !ok {
		return nil, fmt.Errorf("contracts/data/storage.yaml: tables.%s must be a list", engine)
	}
	out := map[string]bool{}
	for _, raw := range list {
		entry, _ := asMap(raw)
		if _, isArchive := entry["archive_of"]; isArchive {
			continue
		}
		if id, _ := entry["table"].(string); id != "" {
			out[id] = true
		}
	}
	return out, nil
}

// renderConstraintErrors 生成 CHECK 约束名到领域错误码的映射，供 API 层把
// PostgreSQL 约束错误翻译成契约错误。
func renderConstraintErrors(tables []table) []byte {
	mapping := map[string]string{}
	for _, t := range tables {
		for _, ck := range t.Checks {
			if ck.Name != "" && ck.OnViolation != "" {
				mapping[ck.Name] = ck.OnViolation
			}
		}
	}
	keys := make([]string, 0, len(mapping))
	for name := range mapping {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("// Code generated by make gen-db; DO NOT EDIT.\n")
	b.WriteString("// Source: contracts/data/tables/*.yaml\n\n")
	b.WriteString("package generated\n\n")
	b.WriteString("// ConstraintErrors maps PostgreSQL CHECK constraint names to platform error codes.\n")
	b.WriteString("var ConstraintErrors = map[string]string{\n")
	for _, name := range keys {
		b.WriteString(fmt.Sprintf("\t%q: %q,\n", name, mapping[name]))
	}
	b.WriteString("}\n")
	out, err := format.Source([]byte(b.String()))
	if err != nil {
		return []byte(b.String())
	}
	return out
}

type table struct {
	ID      string
	Name    string
	Comment string
	Columns []column
	Checks  []check
	Indexes []index
}

type column struct {
	Name        string
	Type        string
	Kind        string
	Mode        string
	Timezone    bool
	TimezoneSet bool
	FracSeconds int
	Length      int
	Nullable    bool
	PrimaryKey  bool
	Default     any
	DefaultSQL  string
	Bits        int
	Unsigned    bool
	MaxLen      int
	MinLen      int
	Fixed       bool
	Precision   int
	Scale       int
	Comment     string
}

type check struct {
	Name        string
	Expr        string
	OnViolation string
}

type index struct {
	Name       string
	Columns    []string
	Expression string
	Unique     bool
	LiveOnly   bool
}

func loadTables(root string, targets contractspec.Targets, customTypes map[string]contractspec.CustomValueType) ([]table, error) {
	basePath := filepath.Join(root, targets.Dir, "_base.yaml")
	baseDoc, err := loadYAML(basePath)
	if err != nil {
		return nil, err
	}
	baseCols, err := parseColumns(baseDoc["columns"], customTypes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", basePath, err)
	}

	files, err := filepath.Glob(filepath.Join(root, targets.Dir, targets.Glob))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	docs := map[string]map[string]any{}
	ids := map[string]string{}
	for _, file := range files {
		if filepath.Base(file) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" {
			id = targets.FileID(filepath.Base(file))
		}
		docs[id] = doc
		ids[id] = file
	}

	var out []table
	for _, id := range sortedKeys(docs) {
		doc := docs[id]
		cols, err := resolveColumns(id, docs, baseCols, customTypes, map[string]bool{})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ids[id], err)
		}
		t, err := parseTable(id, doc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ids[id], err)
		}
		t.Columns = cols
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no table contracts under %s", targets.Dir)
	}
	return out, nil
}

func resolveColumns(id string, docs map[string]map[string]any, baseCols []column, customTypes map[string]contractspec.CustomValueType, visiting map[string]bool) ([]column, error) {
	if id == "data.tables.base" {
		return append([]column{}, baseCols...), nil
	}
	if visiting[id] {
		return nil, fmt.Errorf("cyclic table inheritance at %s", id)
	}
	doc, ok := docs[id]
	if !ok {
		return nil, fmt.Errorf("unknown table %s", id)
	}
	visiting[id] = true
	defer delete(visiting, id)

	tm, _ := asMap(doc["table"])
	var cols []column
	for _, parent := range stringList(tm["extends"]) {
		parentCols, err := resolveColumns(parent, docs, baseCols, customTypes, visiting)
		if err != nil {
			return nil, err
		}
		cols = append(cols, parentCols...)
	}
	own, err := parseColumns(doc["columns"], customTypes)
	if err != nil {
		return nil, err
	}
	cols = append(cols, own...)
	return mergeColumns(cols)
}

func parseTable(id string, doc map[string]any) (table, error) {
	tm, _ := asMap(doc["table"])
	name, _ := tm["name"].(string)
	if name == "" {
		return table{}, fmt.Errorf("table.name required")
	}
	comment, _ := doc["summary"].(string)
	t := table{ID: id, Name: name, Comment: comment}
	for _, raw := range asList(doc["constraints"]) {
		m, _ := asMap(raw)
		name, _ := m["name"].(string)
		expr, _ := m["expr"].(string)
		onViolation, _ := m["on_violation"].(string)
		if name != "" && expr != "" {
			t.Checks = append(t.Checks, check{Name: name, Expr: expr, OnViolation: onViolation})
		}
	}
	for _, raw := range asList(doc["indexes"]) {
		m, _ := asMap(raw)
		name, _ := m["name"].(string)
		if name == "" {
			continue
		}
		idx := index{
			Name:       name,
			Columns:    stringList(m["columns"]),
			Expression: stringValue(m["expression"]),
			Unique:     boolValue(m["unique"]),
			LiveOnly:   boolValue(m["live_only"]),
		}
		t.Indexes = append(t.Indexes, idx)
	}
	return t, nil
}

func parseColumns(raw any, customTypes map[string]contractspec.CustomValueType) ([]column, error) {
	var out []column
	for _, item := range asList(raw) {
		m, _ := asMap(item)
		c := column{
			Name:        stringValue(m["name"]),
			Type:        stringValue(m["type"]),
			Kind:        stringValue(m["kind"]),
			Mode:        stringValue(m["mode"]),
			Timezone:    boolValue(m["timezone"]),
			FracSeconds: intValue(m["fractional_seconds"]),
			Nullable:    boolValue(m["nullable"]),
			PrimaryKey:  boolValue(m["primary_key"]),
			Default:     m["default"],
			DefaultSQL:  stringValue(m["default_sql"]),
			Bits:        intValue(m["bits"]),
			Unsigned:    boolValue(m["unsigned"]),
			MaxLen:      intValue(m["max_len"]),
			MinLen:      intValue(m["min_len"]),
			Fixed:       boolValue(m["fixed"]),
			Precision:   intValue(m["precision"]),
			Scale:       intValue(m["scale"]),
			Comment:     stringValue(m["comment"]),
		}
		if c.Name == "" || c.Type == "" {
			return nil, fmt.Errorf("column name/type required")
		}
		if _, ok := m["timezone"]; ok {
			c.TimezoneSet = true
		}
		if family, variant, ok := contractspec.SplitBasicsRef(c.Type); ok {
			c.Type = family
			if family == "timestamp" && variant != "" {
				c.TimezoneSet = true
				c.Timezone = variant == "time"
			}
			if family == "datetime" && variant != "" {
				c.TimezoneSet = true
				c.Timezone = variant == "timezone"
			}
			if c.Bits == 0 && variant != "" {
				c.Bits, _ = strconv.Atoi(variant)
			}
		}
		if nt, ok := customTypes[c.Type]; ok {
			c.Type = nt.Family
			applyCustomParams(&c, nt.Params)
		}
		out = append(out, c)
	}
	return out, nil
}

func applyCustomParams(c *column, params map[string]any) {
	for k, v := range params {
		switch k {
		case "bits":
			c.Bits = intValue(v)
		case "unsigned":
			c.Unsigned = boolValue(v)
		case "min_len":
			c.MinLen = intValue(v)
		case "max_len":
			c.MaxLen = intValue(v)
		case "fixed":
			c.Fixed = boolValue(v)
		case "precision":
			c.Precision = intValue(v)
		case "scale":
			c.Scale = intValue(v)
		case "kind":
			c.Kind = stringValue(v)
		case "mode":
			c.Mode = stringValue(v)
		case "timezone":
			c.Timezone = boolValue(v)
			c.TimezoneSet = true
		case "fractional_seconds":
			c.FracSeconds = intValue(v)
		case "default":
			if c.Default == nil {
				c.Default = v
			}
		}
	}
}

func mergeColumns(cols []column) ([]column, error) {
	seen := map[string]bool{}
	var out []column
	for _, c := range cols {
		if seen[c.Name] {
			return nil, fmt.Errorf("duplicate column %s", c.Name)
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	return out, nil
}

// render 生成 PostgreSQL 目标态 DDL 与 goose 基线。
// live_only 唯一索引用 partial unique index 表达。
func render(tables []table) ([]byte, []byte, error) {
	var schema strings.Builder
	schema.WriteString("-- Code generated by make gen-db; DO NOT EDIT.\n")
	schema.WriteString("-- Source: contracts/data/tables/*.yaml\n")
	schema.WriteString("-- PostgreSQL; live_only uses partial unique index (deleted_at IS NULL).\n\n")

	var migration strings.Builder
	migration.WriteString("-- Code generated by make gen-db; DO NOT EDIT.\n")
	migration.WriteString("-- PostgreSQL; live_only uses partial unique index (deleted_at IS NULL).\n")
	migration.WriteString("-- +goose Up\n\n")

	for _, t := range tables {
		ddl, err := renderTable(t)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", t.ID, err)
		}
		schema.WriteString(ddl)
		schema.WriteString("\n")
		migration.WriteString(ddl)
		migration.WriteString("\n")
	}
	migration.WriteString("-- +goose Down\n\n")
	for i := len(tables) - 1; i >= 0; i-- {
		migration.WriteString(fmt.Sprintf("DROP TABLE IF EXISTS %s;\n", pgIdent(tables[i].Name)))
	}
	return []byte(schema.String()), []byte(migration.String()), nil
}

func renderTable(t table) (string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE %s (\n", pgIdent(t.Name)))
	var lines []string
	var pk []string
	for _, c := range t.Columns {
		def, err := columnSQL(c)
		if err != nil {
			return "", err
		}
		lines = append(lines, "  "+def)
		if c.PrimaryKey {
			pk = append(pk, pgIdent(c.Name))
		}
	}
	for _, ck := range t.Checks {
		lines = append(lines, fmt.Sprintf("  CONSTRAINT %s CHECK (%s)", pgIdent(ck.Name), ck.Expr))
	}
	if len(pk) > 0 {
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}
	b.WriteString(strings.Join(lines, ",\n"))
	b.WriteString("\n);\n")
	if t.Comment != "" {
		b.WriteString(fmt.Sprintf("COMMENT ON TABLE %s IS %s;\n", pgIdent(t.Name), quoteString(t.Comment)))
	}
	for _, c := range t.Columns {
		if c.Comment != "" {
			b.WriteString(fmt.Sprintf("COMMENT ON COLUMN %s.%s IS %s;\n", pgIdent(t.Name), pgIdent(c.Name), quoteString(c.Comment)))
		}
	}
	for _, idx := range t.Indexes {
		b.WriteString(renderIndex(t.Name, idx))
	}
	return b.String(), nil
}

func columnSQL(c column) (string, error) {
	typ, err := columnType(c)
	if err != nil {
		return "", err
	}
	parts := []string{pgIdent(c.Name), typ}
	if !c.Nullable {
		parts = append(parts, "NOT NULL")
	}
	if c.DefaultSQL != "" {
		parts = append(parts, "DEFAULT ("+c.DefaultSQL+")")
	} else if c.Default != nil {
		d, err := literal(c.Default)
		if err != nil {
			return "", fmt.Errorf("column %s default: %w", c.Name, err)
		}
		parts = append(parts, "DEFAULT "+d)
	}
	return strings.Join(parts, " "), nil
}

func defaultKind(kind, fallback string) string {
	if kind == "" {
		return fallback
	}
	return kind
}

func columnType(c column) (string, error) {
	switch c.Type {
	case "integer":
		if c.Unsigned {
			return "", fmt.Errorf("column %s: PostgreSQL has no unsigned integer; use a range CHECK", c.Name)
		}
		bits := c.Bits
		if bits == 0 {
			bits = 32
		}
		switch bits {
		case 8, 16:
			return "SMALLINT", nil
		case 32:
			return "INTEGER", nil
		case 64:
			return "BIGINT", nil
		default:
			return "", fmt.Errorf("column %s: unsupported integer bits %d", c.Name, bits)
		}
	case "string":
		switch c.Kind {
		case "tsvector":
			return "TSVECTOR", nil
		case "tsquery":
			return "TSQUERY", nil
		}
		if c.MaxLen > 0 {
			if c.Fixed {
				return fmt.Sprintf("CHAR(%d)", c.MaxLen), nil
			}
			return fmt.Sprintf("VARCHAR(%d)", c.MaxLen), nil
		}
		return "TEXT", nil
	case "timestamp":
		prec := c.FracSeconds
		if prec == 0 {
			prec = 6
		}
		if !c.TimezoneSet || c.Timezone {
			return fmt.Sprintf("TIMESTAMPTZ(%d)", prec), nil
		}
		return fmt.Sprintf("TIMESTAMP(%d)", prec), nil
	case "datetime":
		mode := c.Mode
		if mode == "" {
			mode = "datetime"
		}
		switch mode {
		case "date":
			return "DATE", nil
		case "time":
			if c.Timezone {
				return "TIMETZ(6)", nil
			}
			return "TIME(6)", nil
		case "datetime":
			if c.Timezone {
				return "TIMESTAMPTZ(6)", nil
			}
			return "TIMESTAMP(6)", nil
		default:
			return "", fmt.Errorf("column %s: unsupported datetime mode %s", c.Name, mode)
		}
	case "boolean":
		return "BOOLEAN", nil
	case "float":
		if c.Bits == 32 {
			return "REAL", nil
		}
		return "DOUBLE PRECISION", nil
	case "decimal":
		if c.Precision <= 0 {
			return "", fmt.Errorf("column %s: decimal precision required", c.Name)
		}
		return fmt.Sprintf("NUMERIC(%d,%d)", c.Precision, c.Scale), nil
	case "json":
		if c.Kind == "json" {
			return "JSON", nil
		}
		return "JSONB", nil
	case "bytes":
		return "BYTEA", nil
	case "bit":
		kind := strings.ToUpper(defaultKind(c.Kind, "varbit"))
		if c.Length > 0 {
			return fmt.Sprintf("%s(%d)", kind, c.Length), nil
		}
		return kind, nil
	case "xml":
		return "XML", nil
	default:
		return "", fmt.Errorf("column %s: unsupported type %s", c.Name, c.Type)
	}
}

func literal(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return quoteString(x), nil
	case bool:
		if x {
			return "TRUE", nil
		}
		return "FALSE", nil
	case int:
		return fmt.Sprint(x), nil
	case int64:
		return fmt.Sprint(x), nil
	case float64:
		return fmt.Sprintf("%v", x), nil
	default:
		return "", fmt.Errorf("unsupported literal %T", v)
	}
}

func renderIndex(tableName string, idx index) string {
	kind := "INDEX"
	if idx.Unique {
		kind = "UNIQUE INDEX"
	}
	var keys []string
	for _, c := range idx.Columns {
		keys = append(keys, pgIdent(c))
	}
	if idx.Expression != "" {
		keys = append(keys, "("+idx.Expression+")")
	}
	out := fmt.Sprintf("CREATE %s %s ON %s (%s)", kind, pgIdent(idx.Name), pgIdent(tableName), strings.Join(keys, ", "))
	if idx.LiveOnly {
		out += " WHERE " + pgIdent("deleted_at") + " IS NULL"
	}
	return out + ";\n"
}

func pgIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

func quoteString(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func writeFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func stringList(v any) []string {
	var out []string
	for _, raw := range asList(v) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}

func boolValue(v any) bool {
	b, _ := v.(bool)
	return b
}

func intValue(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		return int(x)
	}
	return 0
}

func sortedKeys(m map[string]map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

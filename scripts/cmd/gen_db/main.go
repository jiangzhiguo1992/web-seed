// Command gen_db 从 contracts/data/tables 生成 MySQL 目标 schema 与初始迁移基线。
//
// 输出：
//   - apps/api/schema.sql：当前契约的目标态 DDL（sqlc 输入）
//   - apps/api/migrations/0001_baseline.sql：空库基线（goose Up/Down）
//
// 迁移文件是生成物，禁止手改；表结构变更必须改 contracts/data/tables 后重新生
// 成。live_only 索引用 MySQL 8.0.13+ 的 functional unique index 表达
// （deleted_at IS NULL 的表达式键 + 业务列），而不是 PostgreSQL 的 partial index。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

const (
	schemaPath    = "apps/api/schema.sql"
	migrationPath = "apps/api/migrations/0001_baseline.sql"
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
	tables, err := loadTables(root, schema.Targets)
	if err != nil {
		return err
	}
	schemaSQL, migrationSQL, err := render(tables)
	if err != nil {
		return err
	}
	if check {
		for _, f := range []struct {
			path string
			want []byte
		}{
			{filepath.Join(root, schemaPath), schemaSQL},
			{filepath.Join(root, migrationPath), migrationSQL},
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
	fmt.Printf("generated %s\n", schemaPath)
	fmt.Printf("generated %s\n", migrationPath)
	return nil
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
	Name       string
	Type       string
	Nullable   bool
	PrimaryKey bool
	Default    any
	DefaultSQL string
	Bits       int
	Unsigned   bool
	MaxLen     int
	MinLen     int
	Fixed      bool
	Precision  int
	Scale      int
	Comment    string
}

type check struct {
	Name string
	Expr string
}

type index struct {
	Name       string
	Columns    []string
	Expression string
	Unique     bool
	LiveOnly   bool
}

func loadTables(root string, targets contractspec.Targets) ([]table, error) {
	basePath := filepath.Join(root, targets.Dir, "_base.yaml")
	baseDoc, err := loadYAML(basePath)
	if err != nil {
		return nil, err
	}
	baseCols, err := parseColumns(baseDoc["columns"])
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
		cols, err := resolveColumns(id, docs, baseCols, map[string]bool{})
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

func resolveColumns(id string, docs map[string]map[string]any, baseCols []column, visiting map[string]bool) ([]column, error) {
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
		parentCols, err := resolveColumns(parent, docs, baseCols, visiting)
		if err != nil {
			return nil, err
		}
		cols = append(cols, parentCols...)
	}
	own, err := parseColumns(doc["columns"])
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
	comment, _ := tm["comment"].(string)
	t := table{ID: id, Name: name, Comment: comment}
	for _, raw := range asList(doc["constraints"]) {
		m, _ := asMap(raw)
		name, _ := m["name"].(string)
		expr, _ := m["expr"].(string)
		if name != "" && expr != "" {
			t.Checks = append(t.Checks, check{Name: name, Expr: expr})
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

func parseColumns(raw any) ([]column, error) {
	var out []column
	for _, item := range asList(raw) {
		m, _ := asMap(item)
		c := column{
			Name:       stringValue(m["name"]),
			Type:       stringValue(m["type"]),
			Nullable:   boolValue(m["nullable"]),
			PrimaryKey: boolValue(m["primary_key"]),
			Default:    m["default"],
			DefaultSQL: stringValue(m["default_sql"]),
			Bits:       intValue(m["bits"]),
			Unsigned:   boolValue(m["unsigned"]),
			MaxLen:     intValue(m["max_len"]),
			MinLen:     intValue(m["min_len"]),
			Fixed:      boolValue(m["fixed"]),
			Precision:  intValue(m["precision"]),
			Scale:      intValue(m["scale"]),
			Comment:    stringValue(m["comment"]),
		}
		if c.Name == "" || c.Type == "" {
			return nil, fmt.Errorf("column name/type required")
		}
		out = append(out, c)
	}
	return out, nil
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

func render(tables []table) ([]byte, []byte, error) {
	var schema strings.Builder
	schema.WriteString("-- Code generated by make gen-db; DO NOT EDIT.\n")
	schema.WriteString("-- Source: contracts/data/tables/*.yaml\n")
	schema.WriteString("-- Requires MySQL 8.0.16+ (CHECK constraints, functional indexes).\n\n")

	var migration strings.Builder
	migration.WriteString("-- Code generated by make gen-db; DO NOT EDIT.\n")
	migration.WriteString("-- Requires MySQL 8.0.16+ (CHECK constraints, functional indexes).\n")
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
		migration.WriteString(fmt.Sprintf("DROP TABLE IF EXISTS `%s`;\n", tables[i].Name))
	}
	return []byte(schema.String()), []byte(migration.String()), nil
}

func renderTable(t table) (string, error) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("CREATE TABLE `%s` (\n", t.Name))
	var lines []string
	var pk []string
	for _, c := range t.Columns {
		def, err := columnSQL(c)
		if err != nil {
			return "", err
		}
		lines = append(lines, "  "+def)
		if c.PrimaryKey {
			pk = append(pk, quoteIdent(c.Name))
		}
	}
	for _, ck := range t.Checks {
		lines = append(lines, fmt.Sprintf("  CONSTRAINT `%s` CHECK (%s)", ck.Name, ck.Expr))
	}
	if len(pk) > 0 {
		lines = append(lines, "  PRIMARY KEY ("+strings.Join(pk, ", ")+")")
	}
	b.WriteString(strings.Join(lines, ",\n"))
	b.WriteString("\n) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")
	if t.Comment != "" {
		b.WriteString(" COMMENT=" + quoteString(t.Comment))
	}
	b.WriteString(";\n")

	for _, idx := range t.Indexes {
		b.WriteString(renderIndex(t.Name, idx))
	}
	return b.String(), nil
}

func columnSQL(c column) (string, error) {
	typ, err := mysqlType(c)
	if err != nil {
		return "", err
	}
	parts := []string{"`" + c.Name + "`", typ}
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
	if c.Comment != "" {
		parts = append(parts, "COMMENT "+quoteString(c.Comment))
	}
	return strings.Join(parts, " "), nil
}

func mysqlType(c column) (string, error) {
	switch c.Type {
	case "integer":
		bits := c.Bits
		if bits == 0 {
			bits = 32
		}
		var typ string
		switch bits {
		case 8:
			typ = "TINYINT"
		case 16:
			typ = "SMALLINT"
		case 32:
			typ = "INT"
		case 64:
			typ = "BIGINT"
		default:
			return "", fmt.Errorf("column %s: unsupported integer bits %d", c.Name, bits)
		}
		if c.Unsigned {
			typ += " UNSIGNED"
		}
		return typ, nil
	case "string":
		if c.MaxLen > 0 {
			if c.Fixed {
				return fmt.Sprintf("CHAR(%d)", c.MaxLen), nil
			}
			return fmt.Sprintf("VARCHAR(%d)", c.MaxLen), nil
		}
		return "TEXT", nil
	case "timestamp":
		return "DATETIME(6)", nil
	case "datetime":
		return "DATETIME(6)", nil
	case "boolean":
		return "TINYINT(1)", nil
	case "float":
		if c.Bits == 32 {
			return "FLOAT", nil
		}
		return "DOUBLE", nil
	case "decimal":
		if c.Precision <= 0 {
			return "", fmt.Errorf("column %s: decimal precision required", c.Name)
		}
		return fmt.Sprintf("DECIMAL(%d,%d)", c.Precision, c.Scale), nil
	case "json":
		return "JSON", nil
	case "bytes":
		if c.MaxLen > 0 {
			if c.Fixed {
				return fmt.Sprintf("BINARY(%d)", c.MaxLen), nil
			}
			return fmt.Sprintf("VARBINARY(%d)", c.MaxLen), nil
		}
		return "BLOB", nil
	default:
		return "", fmt.Errorf("column %s: unsupported type %s", c.Name, c.Type)
	}
}

func renderIndex(tableName string, idx index) string {
	kind := "INDEX"
	if idx.Unique {
		kind = "UNIQUE INDEX"
	}
	var keys []string
	if idx.LiveOnly {
		keys = append(keys, "(CASE WHEN `deleted_at` IS NULL THEN 1 ELSE NULL END)")
	}
	for _, c := range idx.Columns {
		keys = append(keys, quoteIdent(c))
	}
	if idx.Expression != "" {
		keys = append(keys, "("+idx.Expression+")")
	}
	return fmt.Sprintf("CREATE %s `%s` ON `%s` (%s);\n", kind, idx.Name, tableName, strings.Join(keys, ", "))
}

func literal(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return quoteString(x), nil
	case bool:
		if x {
			return "1", nil
		}
		return "0", nil
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

func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
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

// render.go 从 _meta 字段规范渲染 contract-new 的最小骨架。
package contractspec

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type tableScaffold struct {
	SchemaVersion int             `yaml:"schema_version"`
	ID            string          `yaml:"id"`
	Summary       string          `yaml:"summary"`
	Stability     string          `yaml:"stability"`
	OwnerModule   string          `yaml:"owner_module"`
	Extends       []string        `yaml:"extends"`
	DoNot         []string        `yaml:"do_not"`
	Table         tableDef        `yaml:"table"`
	Columns       []columnDef     `yaml:"columns"`
	Constraints   []constraintDef `yaml:"constraints"`
	Indexes       []any           `yaml:"indexes"`
	UsedBy        usedByDef       `yaml:"used_by"`
}

type tableDef struct {
	Comment   string `yaml:"comment"`
	SizeClass string `yaml:"size_class"`
}

type columnDef struct {
	Name     string         `yaml:"name"`
	Type     string         `yaml:"type"`
	Nullable bool           `yaml:"nullable"`
	Default  any            `yaml:"default,omitempty"`
	Comment  string         `yaml:"comment,omitempty"`
	Params   map[string]any `yaml:",inline"`
}

type constraintDef struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Column string `yaml:"column"`
	Enum   string `yaml:"enum"`
}

type usedByDef struct {
	Entities     []string `yaml:"entities"`
	Queries      []string `yaml:"queries"`
	Capabilities []string `yaml:"capabilities"`
}

type enumItemScaffold struct {
	ID          string              `yaml:"id"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	DBType      string              `yaml:"db_type"`
	GoType      string              `yaml:"go_type"`
	TSType      string              `yaml:"ts_type"`
	Values      []enumValueScaffold `yaml:"values"`
}

type enumValueScaffold struct {
	Value       string `yaml:"value"`
	Description string `yaml:"description"`
}

func RenderTable(s *Schema, name, module, summary string) ([]byte, error) {
	return RenderTableWithColumnType(s, name, module, summary, "")
}

func RenderTableWithColumnType(s *Schema, name, module, summary, columnTypeOverride string) ([]byte, error) {
	if s == nil || s.Type != "data.tables" {
		return nil, fmt.Errorf("RenderTable: want data.tables schema")
	}
	top := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true,
		"owner_module": true, "extends": true, "do_not": true, "table": true,
		"columns": true, "constraints": true, "indexes": true, "used_by": true,
	}
	if err := requireSupported(s.Fields, top, "data.tables"); err != nil {
		return nil, err
	}
	tableField, _ := FindField(s.Fields, "table")
	if err := requireSupported(tableField.Fields, map[string]bool{"comment": true, "size_class": true}, "data.tables.table"); err != nil {
		return nil, err
	}
	columnField, _ := FindField(s.Fields, "columns")
	if err := requireSupported(columnField.ItemFields, map[string]bool{
		"name": true, "type": true, "nullable": true, "comment": true,
	}, "data.tables.columns"); err != nil {
		return nil, err
	}
	constraintsField, _ := FindField(s.Fields, "constraints")
	if err := requireSupported(constraintsField.ItemFields, map[string]bool{
		"name": true, "type": true, "column": true, "enum": true,
	}, "data.tables.constraints"); err != nil {
		return nil, err
	}
	usedByField, _ := FindField(s.Fields, "used_by")
	if err := requireSupported(usedByField.Fields, map[string]bool{
		"entities": true, "queries": true, "capabilities": true,
	}, "data.tables.used_by"); err != nil {
		return nil, err
	}
	enumName := name + "_status"
	columnName := firstExample(columnField.ItemFields, "name", "name")
	columnType := defaultString(columnField.ItemFields, "type", "string")
	if columnTypeOverride != "" {
		if _, ok := s.ColumnTypes.ValueTypeFamilies[columnTypeOverride]; !ok {
			return nil, fmt.Errorf("RenderTable: unknown column type %q (not in families)", columnTypeOverride)
		}
		columnType = columnTypeOverride
	}
	columnParams, err := scaffoldFamilyParams(s, columnType)
	if err != nil {
		return nil, err
	}
	columnNullable := defaultBool(columnField.ItemFields, "nullable", false)
	sizeClass := nestedDefaultString(s.Fields, "table", "size_class", "small")

	doc := tableScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            "data.tables." + name,
		Summary:       summary,
		Stability:     defaultString(s.Fields, "stability", "draft"),
		OwnerModule:   module,
		Extends:       defaultList(s.Fields, "extends", []string{"data.tables.base"}),
		DoNot:         []string{},
		Table: tableDef{
			Comment:   summary,
			SizeClass: sizeClass,
		},
		Columns: []columnDef{
			{Name: columnName, Type: columnType, Nullable: columnNullable, Comment: "TODO: 列说明", Params: columnParams},
			{Name: "status", Type: "data.enums." + enumName, Nullable: false, Default: "active", Comment: "TODO: 状态说明"},
		},
		Constraints: []constraintDef{
			{Name: "ck_" + name + "_status", Type: "enum", Column: "status", Enum: "data.enums." + enumName},
		},
		Indexes: []any{},
		UsedBy:  usedByDef{Entities: []string{}, Queries: []string{}, Capabilities: []string{}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n#\n# 使用 make contract-new 生成；替换 TODO 占位符。\n# 字段规范见 contracts/_meta/data.tables.yaml。\n\n", summary)
	return append([]byte(header), b...), nil
}

func RenderEnumItem(s *Schema, name string) ([]byte, error) {
	if s == nil || s.Type != "data.enums" {
		return nil, fmt.Errorf("RenderEnumItem: want data.enums schema")
	}
	enumsField, _ := FindField(s.Fields, "enums")
	if err := requireSupported(enumsField.ItemFields, map[string]bool{
		"id": true, "name": true, "description": true, "db_type": true,
		"go_type": true, "ts_type": true, "values": true,
	}, "data.enums.enums"); err != nil {
		return nil, err
	}
	valuesField, _ := FindField(enumsField.ItemFields, "values")
	if err := requireSupported(valuesField.ItemFields, map[string]bool{"value": true, "description": true}, "data.enums.enums.values"); err != nil {
		return nil, err
	}
	dbType := defaultString(enumsField.ItemFields, "db_type", "text")
	pc := pascal(name)
	doc := enumItemScaffold{
		ID:          "data.enums." + name,
		Name:        name,
		Description: name,
		DBType:      dbType,
		GoType:      pc,
		TSType:      pc,
		Values:      []enumValueScaffold{{Value: name, Description: name}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := "# 追加到 contracts/data/enums.yaml 的 enums 列表。\n\n"
	return append([]byte(header), b...), nil
}

func scaffoldFamilyParams(s *Schema, columnType string) (map[string]any, error) {
	fam, ok := s.ColumnTypes.ValueTypeFamilies[columnType]
	if !ok {
		return nil, nil
	}
	hasRequired := false
	for _, def := range fam.Params {
		if def.Required {
			hasRequired = true
			break
		}
	}
	// 只有含 required 参数的类型族才需要骨架扩展字段；
	// string/integer 等可选参数不强行写入，避免生成无意义的 0/false。
	if !hasRequired {
		return nil, nil
	}
	out := map[string]any{}
	for name, def := range fam.Params {
		if def.Default != nil {
			out[name] = def.Default
			continue
		}
		if len(def.Examples) > 0 {
			out[name] = def.Examples[0]
			continue
		}
		if def.Required {
			return nil, fmt.Errorf("scaffold %s.%s: required param has no default/examples", columnType, name)
		}
	}
	return out, nil
}

func firstExample(fields []Field, name, fallback string) string {
	for _, f := range fields {
		if f.Name != name {
			continue
		}
		if len(f.Examples) > 0 {
			if s, ok := f.Examples[0].(string); ok && s != "" {
				return s
			}
		}
		if s, ok := f.Default.(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func defaultString(fields []Field, name, fallback string) string {
	f, ok := FindField(fields, name)
	if !ok {
		return fallback
	}
	if s, ok := f.Default.(string); ok && s != "" {
		return s
	}
	if s, ok := f.Const.(string); ok && s != "" {
		return s
	}
	if len(f.Enum) > 0 {
		if s, ok := f.Enum[0].(string); ok && s != "" {
			return s
		}
	}
	return fallback
}

func nestedDefaultString(fields []Field, parent, child, fallback string) string {
	p, ok := FindField(fields, parent)
	if !ok {
		return fallback
	}
	return defaultString(p.Fields, child, fallback)
}

func defaultList(fields []Field, name string, fallback []string) []string {
	f, ok := FindField(fields, name)
	if !ok {
		return fallback
	}
	switch v := f.Default.(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) > 0 {
			return out
		}
	case []string:
		if len(v) > 0 {
			return v
		}
	case string:
		if v != "" {
			return []string{v}
		}
	}
	return fallback
}

func defaultBool(fields []Field, name string, fallback bool) bool {
	f, ok := FindField(fields, name)
	if !ok {
		return fallback
	}
	if b, ok := f.Default.(bool); ok {
		return b
	}
	return fallback
}

func intConst(fields []Field, name string, fallback int) int {
	f, ok := FindField(fields, name)
	if !ok {
		return fallback
	}
	switch v := f.Const.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	}
	return fallback
}

func pascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func requireSupported(fields []Field, supported map[string]bool, where string) error {
	for _, f := range fields {
		if f.Required && !supported[f.Name] {
			return fmt.Errorf("%s: required field %q has no scaffold rule", where, f.Name)
		}
	}
	return nil
}

func marshal2(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

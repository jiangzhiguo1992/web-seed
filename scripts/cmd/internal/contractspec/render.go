// render.go 从 _meta 字段规范渲染 contract-new 的最小骨架。
package contractspec

import (
	"bytes"
	"fmt"
	"regexp"
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
	Comment  string         `yaml:"comment,omitempty"`
	Params   map[string]any `yaml:",inline"`
}

type constraintDef struct {
	Name    string   `yaml:"name"`
	Type    string   `yaml:"type"`
	Expr    string   `yaml:"expr,omitempty"`
	Columns []string `yaml:"columns,omitempty"`
}

type usedByDef struct {
	Entities []string `yaml:"entities"`
	Queries  []string `yaml:"queries"`
}

type enumItemScaffold struct {
	ID          string              `yaml:"id"`
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	GoType      string              `yaml:"go_type"`
	TSType      string              `yaml:"ts_type"`
	UsedBy      enumUsedByScaffold  `yaml:"used_by"`
	Values      []enumValueScaffold `yaml:"values"`
}

type enumUsedByScaffold struct {
	Entities []string `yaml:"entities"`
	Tables   []string `yaml:"tables"`
}

type enumValueScaffold struct {
	Value       string `yaml:"value"`
	Code        int    `yaml:"code"`
	Description string `yaml:"description"`
}

type entityScaffold struct {
	SchemaVersion int                   `yaml:"schema_version"`
	ID            string                `yaml:"id"`
	Summary       string                `yaml:"summary"`
	Stability     string                `yaml:"stability"`
	Extends       []string              `yaml:"extends"`
	Table         string                `yaml:"table"`
	Identity      string                `yaml:"identity"`
	Fields        []entityFieldScaffold `yaml:"fields"`
	UsedBy        entityUsedByScaffold  `yaml:"used_by"`
}

type entityFieldScaffold struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Required bool   `yaml:"required,omitempty"`
	Mutable  *bool  `yaml:"mutable,omitempty"`
}

type entityUsedByScaffold struct {
	Queries  []string `yaml:"queries"`
	Entities []string `yaml:"entities"`
}

type queryFileScaffold struct {
	SchemaVersion int                 `yaml:"schema_version"`
	ID            string              `yaml:"id"`
	Summary       string              `yaml:"summary"`
	Stability     string              `yaml:"stability"`
	Module        string              `yaml:"module"`
	Queries       []queryItemScaffold `yaml:"queries"`
}

type queryItemScaffold struct {
	ID           string               `yaml:"id"`
	Name         string               `yaml:"name"`
	Kind         string               `yaml:"kind"`
	Table        string               `yaml:"table"`
	Params       []queryParamScaffold `yaml:"params"`
	Returns      string               `yaml:"returns"`
	Semantics    string               `yaml:"semantics"`
	IndexColumns [][]string           `yaml:"index_columns"`
	SQL          querySQLScaffold     `yaml:"sql"`
}

type querySQLScaffold struct {
	Statement string   `yaml:"statement"`
	From      string   `yaml:"from"`
	Select    []string `yaml:"select"`
	Where     []string `yaml:"where,omitempty"`
}

type queryParamScaffold struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

func RenderTable(s *Schema, name, module, summary string) ([]byte, error) {
	return RenderTableWithColumnType(s, name, module, summary, "")
}

func RenderTableWithColumnType(s *Schema, name, module, summary, columnTypeOverride string) ([]byte, error) {
	if s == nil || s.Type != "data.tables" {
		return nil, fmt.Errorf("RenderTable: want data.tables schema")
	}
	if !validName(name) {
		return nil, fmt.Errorf("RenderTable: invalid name %q (snake_case required)", name)
	}
	if !validName(module) {
		return nil, fmt.Errorf("RenderTable: invalid module %q (snake_case required)", module)
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderTable: summary must not be empty")
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
	if err := requireSupported(tableField.Fields, map[string]bool{"comment": true, "size_class": true}, s.Type+".table"); err != nil {
		return nil, err
	}
	columnField, _ := FindField(s.Fields, "columns")
	if err := requireSupported(columnField.ItemFields, map[string]bool{
		"name": true, "type": true, "nullable": true, "comment": true, "redundancy": true,
	}, s.Type+".columns"); err != nil {
		return nil, err
	}
	constraintsField, _ := FindField(s.Fields, "constraints")
	if err := requireSupported(constraintsField.ItemFields, map[string]bool{
		"name": true, "type": true, "expr": true, "columns": true,
	}, s.Type+".constraints"); err != nil {
		return nil, err
	}
	usedByField, _ := FindField(s.Fields, "used_by")
	if err := requireSupported(usedByField.Fields, map[string]bool{
		"entities": true, "queries": true,
	}, s.Type+".used_by"); err != nil {
		return nil, err
	}
	tableID := s.Targets.FileID(name + ".yaml")
	if tableID == "" {
		return nil, fmt.Errorf("RenderTable: cannot derive id from targets.id")
	}
	const baseTableID = "data.tables.base"
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
		ID:            tableID,
		Summary:       summary,
		Stability:     defaultString(s.Fields, "stability", "draft"),
		OwnerModule:   module,
		Extends:       []string{baseTableID},
		DoNot:         []string{},
		Table: tableDef{
			Comment:   "TODO: 表说明",
			SizeClass: sizeClass,
		},
		Columns: []columnDef{
			{Name: columnName, Type: columnType, Nullable: columnNullable, Comment: "TODO: 列说明", Params: columnParams},
		},
		Constraints: []constraintDef{},
		Indexes:     []any{},
		UsedBy:      usedByDef{Entities: []string{}, Queries: []string{}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n#\n# 使用 make contract-new 生成；替换 TODO 占位符。\n# 字段规范见 %s。\n\n", summary, s.ID)
	return append([]byte(header), b...), nil
}

func RenderEnumItem(s *Schema, name string) ([]byte, error) {
	if s == nil || s.Type != "data.enums" {
		return nil, fmt.Errorf("RenderEnumItem: want data.enums schema")
	}
	if !validName(name) {
		return nil, fmt.Errorf("RenderEnumItem: invalid name %q (snake_case required)", name)
	}
	if s.Targets.ID == "" || s.Targets.File == "" {
		return nil, fmt.Errorf("RenderEnumItem: targets.id and targets.file must not be empty")
	}
	enumsField, _ := FindField(s.Fields, "enums")
	if err := requireSupported(enumsField.ItemFields, map[string]bool{
		"id": true, "name": true, "description": true,
		"go_type": true, "ts_type": true, "used_by": true, "values": true,
	}, s.Type+".enums"); err != nil {
		return nil, err
	}
	usedByField, _ := FindField(enumsField.ItemFields, "used_by")
	if err := requireSupported(usedByField.Fields, map[string]bool{"entities": true, "tables": true}, s.Type+".enums.used_by"); err != nil {
		return nil, err
	}
	valuesField, _ := FindField(enumsField.ItemFields, "values")
	if err := requireSupported(valuesField.ItemFields, map[string]bool{"value": true, "code": true, "description": true}, s.Type+".enums.values"); err != nil {
		return nil, err
	}
	pc := pascal(name)
	doc := enumItemScaffold{
		ID:          s.Targets.ID + "." + name,
		Name:        name,
		Description: "TODO: 枚举用途",
		GoType:      pc,
		TSType:      pc,
		UsedBy:      enumUsedByScaffold{Entities: []string{}, Tables: []string{}},
		Values:      []enumValueScaffold{{Value: name, Code: -1, Description: "TODO: 枚举值说明"}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# 追加到 %s 的 enums 列表（每条以 `- ` 开头并缩进 2 空格）。\n# 示例 value/code 仅占位：替换后再提交；code 必须 >= 0。\n\n", s.Targets.File)
	return append([]byte(header), b...), nil
}

// RenderEntity 生成单表读模型的最小骨架；目标表必须已存在。
func RenderEntity(s *Schema, name, table, summary string) ([]byte, error) {
	if s == nil || s.Type != "data.entities" {
		return nil, fmt.Errorf("RenderEntity: want data.entities schema")
	}
	if !validName(name) || !validName(table) {
		return nil, fmt.Errorf("RenderEntity: invalid name/table (snake_case required)")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderEntity: summary must not be empty")
	}
	top := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true,
		"extends": true, "table": true, "identity": true, "fields": true, "used_by": true,
	}
	if err := requireSupported(s.Fields, top, "data.entities"); err != nil {
		return nil, err
	}
	fieldsField, ok := FindField(s.Fields, "fields")
	if !ok {
		return nil, fmt.Errorf("RenderEntity: fields is missing in schema")
	}
	if err := requireSupported(fieldsField.ItemFields, map[string]bool{
		"name": true, "type": true, "required": true, "mutable": true,
	}, "data.entities.fields"); err != nil {
		return nil, err
	}
	usedByField, ok := FindField(s.Fields, "used_by")
	if !ok {
		return nil, fmt.Errorf("RenderEntity: used_by is missing in schema")
	}
	if err := requireSupported(usedByField.Fields, map[string]bool{
		"queries": true, "entities": true,
	}, "data.entities.used_by"); err != nil {
		return nil, err
	}
	entityID := s.Targets.FileID(name + ".yaml")
	if entityID == "" {
		return nil, fmt.Errorf("RenderEntity: cannot derive id from targets.id")
	}
	doc := entityScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            entityID,
		Summary:       summary,
		Stability:     defaultString(s.Fields, "stability", "draft"),
		Extends:       []string{"data.entities.base"},
		Table:         "data.tables." + table,
		Identity:      "id",
		Fields:        []entityFieldScaffold{},
		UsedBy:        entityUsedByScaffold{Queries: []string{}, Entities: []string{}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n#\n# 使用 make contract-new 生成；基类字段已由 _base 继承，追加业务字段。\n# 字段规范见 %s。\n\n", summary, s.ID)
	return append([]byte(header), b...), nil
}

// RenderQuery 生成查询文件的最小合法骨架：SELECT EXISTS 只依赖主键 id。
func RenderQuery(s *Schema, name, module, table, physical, summary string) ([]byte, error) {
	if s == nil || s.Type != "data.queries" {
		return nil, fmt.Errorf("RenderQuery: want data.queries schema")
	}
	if !validName(name) || !validName(table) || !validName(module) {
		return nil, fmt.Errorf("RenderQuery: invalid name/module/table (snake_case required)")
	}
	if !validName(physical) {
		return nil, fmt.Errorf("RenderQuery: invalid physical table name %q (snake_case required)", physical)
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderQuery: summary must not be empty")
	}
	top := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true,
		"module": true, "queries": true,
	}
	if err := requireSupported(s.Fields, top, "data.queries"); err != nil {
		return nil, err
	}
	queriesField, ok := FindField(s.Fields, "queries")
	if !ok {
		return nil, fmt.Errorf("RenderQuery: queries is missing in schema")
	}
	if err := requireSupported(queriesField.ItemFields, map[string]bool{
		"id": true, "name": true, "kind": true, "table": true, "params": true,
		"returns": true, "semantics": true, "index_columns": true, "sql": true,
	}, "data.queries.queries"); err != nil {
		return nil, err
	}
	paramsField, ok := FindField(queriesField.ItemFields, "params")
	if !ok {
		return nil, fmt.Errorf("RenderQuery: params is missing in schema")
	}
	if err := requireSupported(paramsField.ItemFields, map[string]bool{
		"name": true, "type": true,
	}, "data.queries.queries.params"); err != nil {
		return nil, err
	}
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderQuery: cannot derive id from targets.id")
	}
	queryID := fileID + ".exists_" + name
	doc := queryFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     defaultString(s.Fields, "stability", "draft"),
		Module:        module,
		Queries: []queryItemScaffold{{
			ID:           queryID,
			Name:         "Exists" + pascal(name),
			Kind:         "one",
			Table:        "data.tables." + table,
			Params:       []queryParamScaffold{{Name: "id", Type: "text"}},
			Returns:      "boolean",
			Semantics:    "TODO: 查询语义",
			IndexColumns: [][]string{{"id"}},
			SQL: querySQLScaffold{
				Statement: "select",
				From:      "data.tables." + table,
				Select:    []string{fmt.Sprintf("EXISTS (SELECT 1 FROM %s WHERE id = @id)", physical)},
			},
		}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n#\n# 使用 make contract-new 生成；把 EXISTS 换成真实查询并保留 index_columns。\n# 字段规范见 %s。\n\n", summary, s.ID)
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

var snakeNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func validName(s string) bool {
	return snakeNamePattern.MatchString(s)
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
	var raw bytes.Buffer
	enc := yaml.NewEncoder(&raw)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(raw.Bytes(), &doc); err != nil {
		return nil, err
	}
	quoteScaffoldProse(&doc)
	var buf bytes.Buffer
	enc = yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var scaffoldProseKeys = map[string]bool{
	"comment": true, "description": true, "note": true, "reason": true,
	"summary": true, "text": true, "title": true,
	"semantics": true, "caution": true, "generates": true,
}

var scaffoldProseListKeys = map[string]bool{"do_not": true}

// quoteScaffoldProse 让 contract-new 产物满足 contract-check 的双引号规则。
func quoteScaffoldProse(n *yaml.Node) {
	if n == nil {
		return
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			quoteScaffoldProse(child)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if scaffoldProseKeys[k.Value] && v.Kind == yaml.ScalarNode {
				v.Style = yaml.DoubleQuotedStyle
			}
			if scaffoldProseListKeys[k.Value] && v.Kind == yaml.SequenceNode {
				for _, item := range v.Content {
					if item.Kind == yaml.ScalarNode {
						item.Style = yaml.DoubleQuotedStyle
					}
				}
			}
			quoteScaffoldProse(v)
		}
	}
}

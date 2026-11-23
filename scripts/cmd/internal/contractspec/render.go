// render.go 从 _meta 字段规范渲染 contract-new 的最小骨架。
package contractspec

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type tableScaffold struct {
	ID            string          `yaml:"id"`
	Summary       string          `yaml:"summary"`
	OwnerModule   string          `yaml:"owner_module"`
	SchemaVersion int             `yaml:"schema_version"`
	Stability     string          `yaml:"stability"`
	DoNot         []string        `yaml:"do_not"`
	Table         tableDef        `yaml:"table"`
	Columns       []columnDef     `yaml:"columns"`
	Constraints   []constraintDef `yaml:"constraints"`
	Indexes       []any           `yaml:"indexes"`
}

type tableDef struct {
	Name         string   `yaml:"name"`
	Extends      []string `yaml:"extends"`
	Entities     []string `yaml:"entities"`
	ReferencedBy []string `yaml:"referenced_by"`
	Queries      []string `yaml:"queries"`
}

type columnDef struct {
	Name     string         `yaml:"name"`
	Type     string         `yaml:"type"`
	Nullable bool           `yaml:"nullable"`
	Comment  string         `yaml:"comment,omitempty"`
	Params   map[string]any `yaml:",inline"`
}

type constraintDef struct {
	Name        string   `yaml:"name"`
	Expr        string   `yaml:"expr"`
	OnViolation string   `yaml:"on_violation"`
	UsedBy      []string `yaml:"used_by"`
}

type enumItemScaffold struct {
	Name        string              `yaml:"name"`
	Description string              `yaml:"description"`
	GoType      string              `yaml:"go_type"`
	TSType      string              `yaml:"ts_type"`
	Values      []enumValueScaffold `yaml:"values"`
	UsedBy      []string            `yaml:"used_by"`
}

type enumValueScaffold struct {
	Value       string `yaml:"value"`
	Code        int    `yaml:"code"`
	Description string `yaml:"description"`
}

type entityScaffold struct {
	ID            string                `yaml:"id"`
	Summary       string                `yaml:"summary"`
	OwnerModule   string                `yaml:"owner_module"`
	SchemaVersion int                   `yaml:"schema_version"`
	Stability     string                `yaml:"stability"`
	DoNot         []string              `yaml:"do_not"`
	Model         entityModelScaffold   `yaml:"model"`
	Fields        []entityFieldScaffold `yaml:"fields"`
}

type entityModelScaffold struct {
	Name       string   `yaml:"name"`
	Extends    []string `yaml:"extends"`
	Tables     []string `yaml:"tables"`
	IncludedBy []string `yaml:"included_by"`
	Queries    []string `yaml:"queries"`
}

type entityFieldScaffold struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
}

type queryFileScaffold struct {
	ID            string              `yaml:"id"`
	Summary       string              `yaml:"summary"`
	OwnerModule   string              `yaml:"owner_module"`
	SchemaVersion int                 `yaml:"schema_version"`
	Stability     string              `yaml:"stability"`
	DoNot         []string            `yaml:"do_not"`
	Queries       []queryItemScaffold `yaml:"queries"`
}

type queryItemScaffold struct {
	Name        string                  `yaml:"name"`
	Description string                  `yaml:"description"`
	Tables      []queryTableScaffold    `yaml:"tables"`
	Params      []queryParamScaffold    `yaml:"params"`
	Returns     []queryReturnScaffold   `yaml:"returns"`
	Errors      []string                `yaml:"errors"`
	IndexRef    []queryIndexRefScaffold `yaml:"index_ref"`
	UsedBy      []string                `yaml:"used_by"`
}

type queryIndexRefScaffold struct {
	Index   string
	Columns []string
}

// MarshalYAML 以 flow mapping 输出，保持 index_ref 一行一项。
func (r queryIndexRefScaffold) MarshalYAML() (any, error) {
	cols := &yaml.Node{Kind: yaml.SequenceNode, Style: yaml.FlowStyle}
	for _, c := range r.Columns {
		cols.Content = append(cols.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: c})
	}
	return &yaml.Node{Kind: yaml.MappingNode, Style: yaml.FlowStyle, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "index"},
		{Kind: yaml.ScalarNode, Value: r.Index},
		{Kind: yaml.ScalarNode, Value: "columns"},
		cols,
	}}, nil
}

type queryTableScaffold struct {
	Table string `yaml:"table"`
	Write bool   `yaml:"write,omitempty"`
}

type queryReturnScaffold struct {
	Name string `yaml:"name"`
	Kind string `yaml:"kind"`
	Type string `yaml:"type,omitempty"`
}

type queryParamScaffold struct {
	Name   string `yaml:"name"`
	Type   string `yaml:"type"`
	Column string `yaml:"column,omitempty"`
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
		"owner_module": true, "do_not": true, "table": true,
		"columns": true, "constraints": true, "indexes": true,
	}
	if err := requireSupported(s.Fields, top, "data.tables"); err != nil {
		return nil, err
	}
	tableField, _ := FindField(s.Fields, "table")
	if err := requireSupported(tableField.Fields, map[string]bool{"name": true, "extends": true, "entities": true, "queries": true, "referenced_by": true}, s.Type+".table"); err != nil {
		return nil, err
	}
	columnField, _ := FindField(s.Fields, "columns")
	if err := requireSupported(columnField.ItemFields, map[string]bool{
		"name": true, "type": true, "nullable": true, "comment": true, "redundancy": true, "references": true,
	}, s.Type+".columns"); err != nil {
		return nil, err
	}
	constraintsField, _ := FindField(s.Fields, "constraints")
	if err := requireSupported(constraintsField.ItemFields, map[string]bool{
		"name": true, "expr": true, "on_violation": true, "used_by": true,
	}, s.Type+".constraints"); err != nil {
		return nil, err
	}
	tableID := s.Targets.FileID(name + ".yaml")
	if tableID == "" {
		return nil, fmt.Errorf("RenderTable: cannot derive id from targets.id")
	}
	const baseTableID = "data.tables.base"
	columnName := firstExample(columnField.ItemFields, "name", "name")
	columnType := defaultString(columnField.ItemFields, "type", "_types.string")
	if columnTypeOverride != "" {
		name := strings.TrimPrefix(columnTypeOverride, "_types.")
		_, familyOK := s.ValueTypes.TypeFamilies[name]
		_, customOK := s.ValueTypes.CustomTypes[name]
		if !familyOK && !customOK {
			return nil, fmt.Errorf("RenderTable: unknown column type %q (not a family or custom type)", columnTypeOverride)
		}
		columnType = "_types." + name
	}
	columnParams, err := scaffoldFamilyParams(s, columnType)
	if err != nil {
		return nil, err
	}
	columnNullable := defaultBool(columnField.ItemFields, "nullable", false)
	doc := tableScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            tableID,
		Summary:       summary,
		Stability:     scaffoldStability,
		OwnerModule:   module,
		DoNot:         []string{},
		Table: tableDef{
			Name:         name,
			Extends:      []string{baseTableID},
			Entities:     []string{},
			Queries:      []string{},
			ReferencedBy: []string{},
		},
		Columns: []columnDef{
			{Name: columnName, Type: columnType, Nullable: columnNullable, Comment: "TODO: 列说明", Params: columnParams},
		},
		Constraints: []constraintDef{},
		Indexes:     []any{},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n\n", summary)
	return append([]byte(header), b...), nil
}

func RenderEnumItem(s *Schema, name, module string) ([]byte, error) {
	if s == nil || s.Type != "data.enums" {
		return nil, fmt.Errorf("RenderEnumItem: want data.enums schema")
	}
	if !validName(name) {
		return nil, fmt.Errorf("RenderEnumItem: invalid name %q (snake_case required)", name)
	}
	if !validName(module) {
		return nil, fmt.Errorf("RenderEnumItem: invalid module %q (snake_case required)", module)
	}
	if s.Targets.Dir == "" {
		return nil, fmt.Errorf("RenderEnumItem: targets.dir must not be empty")
	}
	target := filepath.Join(s.Targets.Dir, module+".yaml")
	enumsField, _ := FindField(s.Fields, "enums")
	if err := requireSupported(enumsField.ItemFields, map[string]bool{
		"name": true, "description": true,
		"go_type": true, "ts_type": true, "used_by": true, "values": true,
	}, s.Type+".enums"); err != nil {
		return nil, err
	}
	valuesField, _ := FindField(enumsField.ItemFields, "values")
	if err := requireSupported(valuesField.ItemFields, map[string]bool{"value": true, "code": true, "description": true}, s.Type+".enums.values"); err != nil {
		return nil, err
	}
	pc := pascal(name)
	doc := enumItemScaffold{
		Name:        name,
		Description: "TODO: 枚举用途",
		GoType:      pc,
		TSType:      pc,
		Values:      []enumValueScaffold{{Value: name, Code: -1, Description: "TODO: 枚举值说明"}},
		UsedBy:      []string{},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# 追加到 %s 的 enums 列表；文件不存在时先创建 id: data.enums.%s、owner_module: %s；示例 value/code 与 used_by 需替换。\n\n", target, module, module)
	return append([]byte(header), b...), nil
}

// RenderEntity 生成单表读模型的最小骨架；tableID 是目标表契约 ID。
func RenderEntity(s *Schema, name, tableID, ownerModule, summary string) ([]byte, error) {
	if s == nil || s.Type != "data.entities" {
		return nil, fmt.Errorf("RenderEntity: want data.entities schema")
	}
	if !validName(name) || strings.TrimSpace(tableID) == "" {
		return nil, fmt.Errorf("RenderEntity: invalid name/tableID")
	}
	if !validName(ownerModule) {
		return nil, fmt.Errorf("RenderEntity: invalid owner_module (snake_case required)")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderEntity: summary must not be empty")
	}
	top := map[string]bool{
		"id": true, "summary": true, "owner_module": true, "schema_version": true,
		"stability": true, "do_not": true, "model": true, "fields": true,
	}
	if err := requireSupported(s.Fields, top, "data.entities"); err != nil {
		return nil, err
	}
	modelField, ok := FindField(s.Fields, "model")
	if !ok {
		return nil, fmt.Errorf("RenderEntity: model is missing in schema")
	}
	if err := requireSupported(modelField.Fields, map[string]bool{
		"name": true, "extends": true, "tables": true, "queries": true, "included_by": true,
	}, "data.entities.model"); err != nil {
		return nil, err
	}
	fieldsField, ok := FindField(s.Fields, "fields")
	if !ok {
		return nil, fmt.Errorf("RenderEntity: fields is missing in schema")
	}
	if err := requireSupported(fieldsField.ItemFields, map[string]bool{
		"name": true, "json_name": true, "description": true, "type": true,
		"source": true, "sensitivity": true,
	}, "data.entities.fields"); err != nil {
		return nil, err
	}
	entityID := s.Targets.FileID(name + ".yaml")
	if entityID == "" {
		return nil, fmt.Errorf("RenderEntity: cannot derive id from targets.id")
	}
	doc := entityScaffold{
		ID:            entityID,
		Summary:       summary,
		OwnerModule:   ownerModule,
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		Stability:     scaffoldStability,
		DoNot:         []string{},
		Model: entityModelScaffold{
			Name:       lowerCamel(name),
			Extends:    []string{"data.entities.base"},
			Tables:     []string{tableID},
			Queries:    []string{},
			IncludedBy: []string{},
		},
		Fields: []entityFieldScaffold{},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n\n", summary)
	return append([]byte(header), b...), nil
}

// RenderQuery 生成查询文件的最小合法骨架：SELECT EXISTS 只依赖主键 id。
func RenderQuery(s *Schema, name, module, tableID, summary string) ([]byte, error) {
	if s == nil || s.Type != "data.queries" {
		return nil, fmt.Errorf("RenderQuery: want data.queries schema")
	}
	if !validName(name) || !validName(module) || strings.TrimSpace(tableID) == "" {
		return nil, fmt.Errorf("RenderQuery: invalid name/module/tableID")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderQuery: summary must not be empty")
	}
	top := map[string]bool{
		"schema_version": true, "id": true, "summary": true, "stability": true,
		"owner_module": true, "do_not": true, "queries": true,
	}
	if err := requireSupported(s.Fields, top, "data.queries"); err != nil {
		return nil, err
	}
	queriesField, ok := FindField(s.Fields, "queries")
	if !ok {
		return nil, fmt.Errorf("RenderQuery: queries is missing in schema")
	}
	if err := requireSupported(queriesField.ItemFields, map[string]bool{
		"name": true, "description": true, "tables": true,
		"params": true, "returns": true, "errors": true, "index_ref": true, "used_by": true,
	}, "data.queries.queries"); err != nil {
		return nil, err
	}
	tablesField, ok := FindField(queriesField.ItemFields, "tables")
	if !ok {
		return nil, fmt.Errorf("RenderQuery: tables is missing in schema")
	}
	if err := requireSupported(tablesField.ItemFields, map[string]bool{
		"table": true, "write": true,
	}, "data.queries.queries.tables"); err != nil {
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
	returnsField, ok := FindField(queriesField.ItemFields, "returns")
	if !ok {
		return nil, fmt.Errorf("RenderQuery: returns is missing in schema")
	}
	if err := requireSupported(returnsField.ItemFields, map[string]bool{
		"name": true, "kind": true,
	}, "data.queries.queries.returns"); err != nil {
		return nil, err
	}
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderQuery: cannot derive id from targets.id")
	}
	doc := queryFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		OwnerModule:   module,
		DoNot:         []string{},
		Queries: []queryItemScaffold{{
			Name:        "Exists" + pascal(name),
			Description: "TODO: 查询语义",
			Tables:      []queryTableScaffold{{Table: tableID}},
			Params:      []queryParamScaffold{{Name: "ctx", Type: "context.Context"}, {Name: "id", Type: "_types.integer", Column: tableID + ".col.id"}},
			Returns:     []queryReturnScaffold{{Name: "exists", Kind: "scalar", Type: "_types.boolean"}, {Name: "err", Kind: "error"}},
			Errors:      []string{},
			IndexRef:    []queryIndexRefScaffold{{Index: tableID + ".pk", Columns: []string{"id"}}},
			UsedBy:      []string{},
		}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	header := fmt.Sprintf("# %s\n\n", summary)
	return append([]byte(header), b...), nil
}

func scaffoldFamilyParams(s *Schema, columnType string) (map[string]any, error) {
	columnType = strings.TrimPrefix(columnType, "_types.")
	if _, custom := s.ValueTypes.CustomTypes[columnType]; custom {
		return nil, nil
	}
	fam, ok := s.ValueTypes.TypeFamilies[columnType]
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

// scaffoldStability 固定 draft：stable 的进入/退出必须由用户手动修改，生成器不得写入 stable。
const scaffoldStability = "draft"

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

func lowerCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		if i == 0 {
			parts[i] = strings.ToLower(p[:1]) + p[1:]
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

// ServiceInterfaceName 按 owner_module + file 派生 Go interface 名：
// owner 与 file 相同（account/account）时折叠为 AccountService，否则为 AccountAuthService。
func ServiceInterfaceName(ownerModule, file string) string {
	owner := pascal(ownerModule)
	stem := pascal(file)
	if strings.EqualFold(owner, stem) {
		return owner + "Service"
	}
	return owner + stem + "Service"
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
	"comment": true, "description": true, "note": true,
	"summary": true, "text": true, "title": true,
	"semantics": true, "caution": true, "generates": true,
	"when": true,
}

var scaffoldProseListKeys = map[string]bool{"common_mistakes": true, "do_not": true}

// ScaffoldProseKeys 返回 contract-new 会强制双引号的字段名，供 checker 侧做漂移检查。
func ScaffoldProseKeys() []string {
	keys := make([]string, 0, len(scaffoldProseKeys))
	for key := range scaffoldProseKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ScaffoldProseListKeys 返回 contract-new 会强制列表项双引号的字段名。
func ScaffoldProseListKeys() []string {
	keys := make([]string, 0, len(scaffoldProseListKeys))
	for key := range scaffoldProseListKeys {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

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

type serviceFileScaffold struct {
	ID            string                           `yaml:"id"`
	Summary       string                           `yaml:"summary"`
	OwnerModule   string                           `yaml:"owner_module"`
	SchemaVersion int                              `yaml:"schema_version"`
	Stability     string                           `yaml:"stability"`
	DoNot         []string                         `yaml:"do_not"`
	Schemas       map[string]serviceSchemaScaffold `yaml:"schemas,omitempty"`
	Services      []serviceItemScaffold            `yaml:"services"`
}

type serviceSchemaScaffold struct {
	Type       string                    `yaml:"type,omitempty"`
	Properties []servicePropertyScaffold `yaml:"properties,omitempty"`
	Items      *serviceSchemaScaffold    `yaml:"items,omitempty"`
	Ref        string                    `yaml:"ref,omitempty"`
	MinItems   *int                      `yaml:"min_items,omitempty"`
	MaxItems   *int                      `yaml:"max_items,omitempty"`
}

// servicePropertyScaffold 是 object.properties[] 的槽位条目：值域用 schema，
// 方向/存在性/可空/默认值只在这里出现。
type servicePropertyScaffold struct {
	Name        string                `yaml:"name"`
	Schema      serviceSchemaScaffold `yaml:"schema"`
	Required    bool                  `yaml:"required,omitempty"`
	Nullable    bool                  `yaml:"nullable,omitempty"`
	ReadOnly    bool                  `yaml:"read_only,omitempty"`
	WriteOnly   bool                  `yaml:"write_only,omitempty"`
	Default     any                   `yaml:"default,omitempty"`
	Description string                `yaml:"description"`
}

type serviceItemScaffold struct {
	Name       string              `yaml:"name"`
	Summary    string              `yaml:"summary"`
	Visibility string              `yaml:"visibility"`
	TxMode     string              `yaml:"tx_mode"`
	Params     []methodArgScaffold `yaml:"params"`
	Returns    []methodArgScaffold `yaml:"returns"`
	Steps      []stepScaffold      `yaml:"steps"`
}

type stepScaffold struct {
	ID      string            `yaml:"id"`
	Kind    string            `yaml:"kind"`
	Summary string            `yaml:"summary"`
	Input   []inputScaffold   `yaml:"input,omitempty"`
	Output  []outputScaffold  `yaml:"output,omitempty"`
	Errors  []string          `yaml:"errors,omitempty"`
	Rules   []ruleRefScaffold `yaml:"rules"`
	Goto    string            `yaml:"goto"`
}

type ruleRefScaffold struct {
	Rule string `yaml:"rule"`
}

type inputScaffold struct {
	Name        string `yaml:"name"`
	Value       string `yaml:"value"`
	Description string `yaml:"description"`
}

type outputScaffold struct {
	Name        string `yaml:"name"`
	Type        string `yaml:"type"`
	Description string `yaml:"description"`
}

type methodArgScaffold struct {
	Name     string `yaml:"name"`
	Type     string `yaml:"type"`
	Value    string `yaml:"value,omitempty"`
	Nullable bool   `yaml:"nullable,omitempty"`
	Default  any    `yaml:"default,omitempty"`
}

type ruleFileScaffold struct {
	ID            string         `yaml:"id"`
	Summary       string         `yaml:"summary"`
	OwnerModule   string         `yaml:"owner_module"`
	SchemaVersion int            `yaml:"schema_version"`
	Stability     string         `yaml:"stability"`
	DoNot         []string       `yaml:"do_not"`
	Rules         []ruleScaffold `yaml:"rules"`
}

type ruleScaffold struct {
	ID            string   `yaml:"id"`
	Kind          string   `yaml:"kind"`
	Summary       string   `yaml:"summary"`
	OnViolation   string   `yaml:"on_violation"`
	ImplementedBy []string `yaml:"implemented_by"`
}

type lifecycleFileScaffold struct {
	ID            string                     `yaml:"id"`
	Summary       string                     `yaml:"summary"`
	OwnerModule   string                     `yaml:"owner_module"`
	SchemaVersion int                        `yaml:"schema_version"`
	Stability     string                     `yaml:"stability"`
	DoNot         []string                   `yaml:"do_not"`
	Subjects      []lifecycleSubjectScaffold `yaml:"subjects"`
	States        lifecycleStatesScaffold    `yaml:"states"`
	Create        lifecycleActionScaffold    `yaml:"create"`
}

type lifecycleActionScaffold struct {
	ImplementedBy []string `yaml:"implemented_by"`
}

type lifecycleStatesScaffold struct {
	Values  []string `yaml:"values"`
	Initial string   `yaml:"initial"`
	Final   []string `yaml:"final,omitempty"`
}

type lifecycleSubjectScaffold struct {
	Entity string `yaml:"entity"`
	Field  string `yaml:"field"`
}

type eventsFileScaffold struct {
	ID            string               `yaml:"id"`
	Summary       string               `yaml:"summary"`
	OwnerModule   string               `yaml:"owner_module"`
	SchemaVersion int                  `yaml:"schema_version"`
	Stability     string               `yaml:"stability"`
	DoNot         []string             `yaml:"do_not"`
	Topics        []eventTopicScaffold `yaml:"topics,omitempty"`
}

type eventTopicScaffold struct {
	ID         string               `yaml:"id"`
	Name       string               `yaml:"name"`
	Version    int                  `yaml:"version"`
	Summary    string               `yaml:"summary"`
	Idempotent bool                 `yaml:"idempotent"`
	Publish    eventPublishScaffold `yaml:"publish"`
}

type eventPublishScaffold struct {
	Payload   eventPayloadScaffold `yaml:"payload"`
	EmittedBy []string             `yaml:"emitted_by"`
}

type eventPayloadScaffold struct {
	Type       string                    `yaml:"type"`
	Properties []servicePropertyScaffold `yaml:"properties"`
}

type httpMountScaffold struct {
	APIVersion string   `yaml:"api_version"`
	Path       string   `yaml:"path"`
	Audience   []string `yaml:"audience"`
}

type httpResponseScaffold struct {
	Description string         `yaml:"description"`
	Headers     map[string]any `yaml:"headers"`
}

type httpOperationScaffold struct {
	ID       string                          `yaml:"id"`
	Name     string                          `yaml:"name"`
	Service  string                          `yaml:"service"`
	Summary  string                          `yaml:"summary"`
	UsedBy   []string                        `yaml:"used_by"`
	Security []any                           `yaml:"security"`
	Request  map[string]any                  `yaml:"request"`
	Response map[string]httpResponseScaffold `yaml:"response"`
}

type httpFileScaffold struct {
	ID            string            `yaml:"id"`
	Summary       string            `yaml:"summary"`
	OwnerModule   string            `yaml:"owner_module"`
	SchemaVersion int               `yaml:"schema_version"`
	Stability     string            `yaml:"stability"`
	DoNot         []string          `yaml:"do_not"`
	Mount         httpMountScaffold `yaml:"mount"`
	Paths         map[string]any    `yaml:"paths"`
}

// RenderService 生成 cap.services 的最小合法骨架。
func RenderService(s *Schema, name, module, summary string) ([]byte, error) {
	if s == nil || s.Type != "cap.services" {
		return nil, fmt.Errorf("RenderService: want cap.services schema")
	}
	if !validName(name) || !validName(module) {
		return nil, fmt.Errorf("RenderService: invalid name/module")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderService: summary must not be empty")
	}
	if err := requireSupported(s.Fields, map[string]bool{
		"id": true, "summary": true, "owner_module": true, "schema_version": true,
		"stability": true, "do_not": true,
		"schemas": true, "services": true,
	}, "cap.services"); err != nil {
		return nil, err
	}
	if f, ok := FindField(s.Fields, "services"); ok {
		if err := requireSupported(f.ItemFields, map[string]bool{
			"name": true, "summary": true, "visibility": true, "params": true,
			"returns": true, "tx_mode": true, "steps": true, "used_by": true,
		}, "cap.services.services"); err != nil {
			return nil, err
		}
		if steps, ok := FindField(f.ItemFields, "steps"); ok {
			if err := requireSupported(steps.ItemFields, map[string]bool{
				"id": true, "kind": true, "summary": true,
				"input": true, "output": true, "errors": true,
				"events": true, "rules": true, "lifecycles": true,
				"goto": true, "when": true, "query": true, "service": true,
			}, "cap.services.steps"); err != nil {
				return nil, err
			}
			if output, ok := FindField(steps.ItemFields, "output"); ok {
				if err := requireSupported(output.ItemFields, map[string]bool{"name": true, "type": true, "description": true, "nullable": true}, "cap.services.steps.output"); err != nil {
					return nil, err
				}
			}
			if input, ok := FindField(steps.ItemFields, "input"); ok {
				if err := requireSupported(input.ItemFields, map[string]bool{"name": true, "value": true, "description": true}, "cap.services.steps.input"); err != nil {
					return nil, err
				}
			}
		}
		if f, ok := FindField(f.ItemFields, "params"); ok {
			if err := requireSupported(f.ItemFields, map[string]bool{"name": true, "type": true, "nullable": true, "default": true}, "cap.services.params"); err != nil {
				return nil, err
			}
		}
		if f, ok := FindField(f.ItemFields, "returns"); ok {
			if err := requireSupported(f.ItemFields, map[string]bool{"name": true, "type": true, "value": true, "nullable": true}, "cap.services.returns"); err != nil {
				return nil, err
			}
		}
	}
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderService: cannot derive id from targets.id")
	}
	doc := serviceFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		DoNot:         []string{"TODO: 实例级负向约束"},
		OwnerModule:   module,
		Schemas: map[string]serviceSchemaScaffold{
			"GetInput": {
				Type: "_types.object",
				Properties: []servicePropertyScaffold{{
					Name:        "id",
					Schema:      serviceSchemaScaffold{Type: "_types.integer"},
					Required:    true,
					Description: "TODO: 字段说明",
				}},
			},
		},
		Services: []serviceItemScaffold{{
			Name:       "get",
			Summary:    "TODO: 服务说明",
			Visibility: "public",
			Params:     []methodArgScaffold{{Name: "ctx", Type: "context.Context"}, {Name: "input", Type: fileID + ".schemas.GetInput"}},
			Returns:    []methodArgScaffold{{Name: "value", Type: "_types.any", Value: "step.load.value"}, {Name: "err", Type: "error"}},
			TxMode:     "read_only",
			Steps: []stepScaffold{
				{
					ID:      "load",
					Kind:    "logic",
					Summary: "TODO: 实现步骤",
					Input:   []inputScaffold{{Name: "input", Value: "params.input", Description: "TODO: 入参用途"}},
					Output:  []outputScaffold{{Name: "value", Type: "_types.any", Description: "TODO: 产出说明"}},
					Errors:  []string{"platform.errors.COMMON_INTERNAL"},
					Rules:   []ruleRefScaffold{},
					Goto:    "$end",
				},
			},
		}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(fmt.Sprintf("# %s\n\n", summary)), b...), nil
}

// RenderRule 生成 feature.rules 的最小 policy 骨架。
func RenderRule(s *Schema, name, module, summary string) ([]byte, error) {
	if s == nil || s.Type != "feature.rules" {
		return nil, fmt.Errorf("RenderRule: want feature.rules schema")
	}
	if !validName(name) || !validName(module) {
		return nil, fmt.Errorf("RenderRule: invalid name/module")
	}
	if err := requireSupported(s.Fields, map[string]bool{"id": true, "summary": true, "owner_module": true, "schema_version": true, "stability": true, "do_not": true, "rules": true}, "feature.rules"); err != nil {
		return nil, err
	}
	if f, ok := FindField(s.Fields, "rules"); ok {
		if err := requireSupported(f.ItemFields, map[string]bool{"id": true, "kind": true, "summary": true, "on_violation": true, "implemented_by": true}, "feature.rules.rules"); err != nil {
			return nil, err
		}
	}
	fileID := s.Targets.FileID(name + ".yaml")
	doc := ruleFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		DoNot:         []string{"TODO: 实例级负向约束"},
		OwnerModule:   module,
		Rules: []ruleScaffold{{
			ID:            fileID + ".example",
			Kind:          "policy",
			Summary:       "TODO: 规则说明",
			OnViolation:   "platform.errors.COMMON_INTERNAL",
			ImplementedBy: []string{},
		}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(fmt.Sprintf("# %s\n\n", summary)), b...), nil
}

// RenderLifecycle 生成 cap.lifecycles 的最小生命周期骨架。
func RenderLifecycle(s *Schema, name, module, summary string) ([]byte, error) {
	if s == nil || s.Type != "cap.lifecycles" {
		return nil, fmt.Errorf("RenderLifecycle: want cap.lifecycles schema")
	}
	if !validName(name) || !validName(module) {
		return nil, fmt.Errorf("RenderLifecycle: invalid name/module")
	}
	if err := requireSupported(s.Fields, map[string]bool{
		"id": true, "summary": true, "owner_module": true, "schema_version": true,
		"stability": true, "do_not": true, "subjects": true, "states": true,
		"create": true, "destroy": true, "transitions": true,
	}, "cap.lifecycles"); err != nil {
		return nil, err
	}
	if f, ok := FindField(s.Fields, "subjects"); ok {
		if err := requireSupported(f.ItemFields, map[string]bool{"entity": true, "field": true}, "cap.lifecycles.subjects"); err != nil {
			return nil, err
		}
	}
	if f, ok := FindField(s.Fields, "states"); ok {
		if err := requireSupported(f.Fields, map[string]bool{"values": true, "initial": true, "final": true}, "cap.lifecycles.states"); err != nil {
			return nil, err
		}
	}
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderLifecycle: cannot derive id from targets.id")
	}
	doc := lifecycleFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		DoNot:         []string{"TODO: 实例级负向约束"},
		OwnerModule:   module,
		Subjects:      []lifecycleSubjectScaffold{{Entity: "data.entities." + name, Field: "status"}},
		States: lifecycleStatesScaffold{
			Values:  []string{"active"},
			Initial: "active",
			Final:   []string{"active"},
		},
		Create: lifecycleActionScaffold{
			ImplementedBy: []string{"cap.services." + name + ".create.create"},
		},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(fmt.Sprintf("# %s\n\n", summary)), b...), nil
}

// RenderEvents 生成 cap.events 的最小骨架：一个 topic + 一个订阅它的 job。
func RenderEvents(s *Schema, name, module, summary string) ([]byte, error) {
	if s == nil || s.Type != "cap.events" {
		return nil, fmt.Errorf("RenderEvents: want cap.events schema")
	}
	if !validName(name) || !validName(module) {
		return nil, fmt.Errorf("RenderEvents: invalid name/module")
	}
	if err := requireSupported(s.Fields, map[string]bool{
		"id": true, "summary": true, "owner_module": true, "schema_version": true,
		"stability": true, "do_not": true, "topics": true, "crons": true, "dispatches": true,
	}, "cap.events"); err != nil {
		return nil, err
	}
	if f, ok := FindField(s.Fields, "topics"); ok {
		if err := requireSupported(f.ItemFields, map[string]bool{
			"id": true, "name": true, "version": true, "summary": true,
			"idempotent": true, "publish": true, "subscribe": true,
		}, "cap.events.topics"); err != nil {
			return nil, err
		}
	}
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderEvents: cannot derive id from targets.id")
	}
	topicID := fileID + ".created"
	doc := eventsFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		DoNot:         []string{"TODO: 实例级负向约束"},
		OwnerModule:   module,
		Topics: []eventTopicScaffold{{
			ID:         topicID,
			Name:       name + ".created",
			Version:    1,
			Summary:    "TODO: 主题说明",
			Idempotent: false,
			Publish: eventPublishScaffold{
				Payload: eventPayloadScaffold{
					Type: "_types.object",
					Properties: []servicePropertyScaffold{{
						Name:        "id",
						Schema:      serviceSchemaScaffold{Type: "_types.integer"},
						Required:    true,
						Description: "TODO: id 说明",
					}},
				},
				EmittedBy: []string{},
			},
		}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(fmt.Sprintf("# %s\n\n", summary)), b...), nil
}

// RenderHTTP 生成 cap.protocols.http 的最小单 operation 骨架。
func RenderHTTP(s *Schema, name, module, summary, service, path, httpMethod string) ([]byte, error) {
	if s == nil || s.Type != "cap.protocols.http" {
		return nil, fmt.Errorf("RenderHTTP: want cap.protocols.http schema")
	}
	if !validName(name) || !validName(module) {
		return nil, fmt.Errorf("RenderHTTP: invalid name/module")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderHTTP: summary must not be empty")
	}
	if err := requireSupported(s.Fields, map[string]bool{"id": true, "summary": true, "owner_module": true, "schema_version": true, "stability": true, "do_not": true, "mount": true, "paths": true, "shared": true}, "cap.protocols.http"); err != nil {
		return nil, err
	}
	if !strings.HasPrefix(service, "cap.services.") {
		return nil, fmt.Errorf("RenderHTTP: service must be cap.services.*")
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("RenderHTTP: path must start with /")
	}
	if httpMethod == "" {
		httpMethod = "get"
	}
	httpMethod = strings.ToLower(httpMethod)
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderHTTP: cannot derive id from targets.id")
	}
	op := httpOperationScaffold{
		ID:       fileID + "." + httpMethod,
		Name:     httpMethod + pascal(name),
		Service:  service,
		Summary:  summary,
		UsedBy:   []string{"feature.pages." + name},
		Security: []any{},
		Request: map[string]any{
			"headers": map[string]any{
				"X-Request-Id": "RequestId",
			},
			"mapping": map[string]any{},
		},
		Response: map[string]httpResponseScaffold{
			"204": {
				Description: "OK",
				Headers: map[string]any{
					"X-Request-Id": "XRequestId",
				},
			},
		},
	}
	doc := httpFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		DoNot:         []string{},
		OwnerModule:   module,
		Mount:         httpMountScaffold{APIVersion: "v1", Path: "/", Audience: []string{"admin"}},
		Paths:         map[string]any{path: map[string]any{httpMethod: op}},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(fmt.Sprintf("# %s\n\n", summary)), b...), nil
}

type pageFileScaffold struct {
	ID            string                 `yaml:"id"`
	Summary       string                 `yaml:"summary"`
	SchemaVersion int                    `yaml:"schema_version"`
	Stability     string                 `yaml:"stability"`
	DoNot         []string               `yaml:"do_not"`
	Page          string                 `yaml:"page"`
	Route         any                    `yaml:"route"`
	ViewStates    pageViewStatesScaffold `yaml:"view_states"`
}

type pageViewStatesScaffold struct {
	Loading   pageStateScaffold `yaml:"loading"`
	Empty     pageStateScaffold `yaml:"empty"`
	Error     pageStateScaffold `yaml:"error"`
	Forbidden pageStateScaffold `yaml:"forbidden"`
}

type pageStateScaffold struct {
	When string `yaml:"when"`
	Note string `yaml:"note,omitempty"`
}

// RenderPage 生成 feature.pages 的最小合法骨架：四态齐全，route 留空待绑定。
func RenderPage(s *Schema, name, summary string) ([]byte, error) {
	if s == nil || s.Type != "feature.pages" {
		return nil, fmt.Errorf("RenderPage: want feature.pages schema")
	}
	if !validName(name) {
		return nil, fmt.Errorf("RenderPage: invalid name")
	}
	if strings.TrimSpace(summary) == "" {
		return nil, fmt.Errorf("RenderPage: summary must not be empty")
	}
	if err := requireSupported(s.Fields, map[string]bool{
		"id": true, "summary": true, "schema_version": true, "stability": true,
		"do_not": true, "page": true, "route": true, "data": true,
		"view_states": true, "actions": true,
	}, "feature.pages"); err != nil {
		return nil, err
	}
	if f, ok := FindField(s.Fields, "view_states"); ok {
		if err := requireSupported(f.ItemFields, map[string]bool{
			"when": true, "ui": true, "show": true, "note": true, "action": true, "retry": true,
		}, "feature.pages.view_states"); err != nil {
			return nil, err
		}
	}
	fileID := s.Targets.FileID(name + ".yaml")
	if fileID == "" {
		return nil, fmt.Errorf("RenderPage: cannot derive id from targets.id")
	}
	doc := pageFileScaffold{
		SchemaVersion: intConst(s.Fields, "schema_version", 1),
		ID:            fileID,
		Summary:       summary,
		Stability:     scaffoldStability,
		DoNot:         []string{"TODO: 实例级负向约束"},
		Page:          name,
		Route:         nil,
		ViewStates: pageViewStatesScaffold{
			Loading:   pageStateScaffold{When: "loading"},
			Empty:     pageStateScaffold{When: "items.length == 0"},
			Error:     pageStateScaffold{When: "error != null"},
			Forbidden: pageStateScaffold{When: "forbidden"},
		},
	}
	b, err := marshal2(doc)
	if err != nil {
		return nil, err
	}
	return append([]byte(fmt.Sprintf("# %s\n\n", summary)), b...), nil
}

// RenderFieldValue 按 Field.type 渲染最小骨架，供 contract-new 的通用字段使用。
// list<map<object>> 的 item_fields 描述 map value 的字段。
func RenderFieldValue(f Field) (any, error) {
	return RenderFieldValueWithTypes(f, ValueTypes{})
}

func RenderFieldValueWithTypes(f Field, vt ValueTypes) (any, error) {
	return renderFieldValue(f, 0, vt)
}

func renderFieldValue(f Field, index int, vt ValueTypes) (any, error) {
	typ := strings.ReplaceAll(f.Type, "_types.", "")
	switch {
	case typ == "object":
		fields := f.Fields
		if len(fields) == 0 {
			fields = f.ItemFields
		}
		out := map[string]any{}
		for _, child := range fields {
			if f.UniqueKey != "" && child.Name == f.UniqueKey {
				out[child.Name] = uniqueKeyValue(child.Type, index, vt)
				continue
			}
			v, err := renderFieldValue(child, index, vt)
			if err != nil {
				return nil, err
			}
			out[child.Name] = v
		}
		return out, nil
	case (strings.HasPrefix(typ, "list<") || strings.HasPrefix(typ, "set<") || strings.HasPrefix(typ, "array<")) && strings.HasSuffix(typ, ">"):
		var inner string
		isSet := strings.HasPrefix(typ, "set<")
		switch {
		case strings.HasPrefix(typ, "list<"):
			inner = typ[len("list<") : len(typ)-1]
		case isSet:
			inner = typ[len("set<") : len(typ)-1]
		default:
			inner = typ[len("array<") : len(typ)-1]
		}
		child := Field{Type: inner, ItemFields: f.ItemFields, Fields: f.Fields, KeyPatterns: f.KeyPatterns, UniqueKey: f.UniqueKey}
		n := 1
		if strings.HasPrefix(typ, "array<") {
			if v, ok := fieldInt(f.Len); ok && v > 0 {
				n = v
			}
		} else {
			if v, ok := fieldInt(f.MinItems); ok {
				n = v
			} else if v, ok := fieldInt(f.MaxItems); ok && v == 0 {
				n = 0
			}
			if v, ok := fieldInt(f.MaxItems); ok && n > v {
				n = v
			}
			if n < 0 {
				n = 0
			}
		}
		out := make([]any, 0, n)
		for i := 0; i < n; i++ {
			idx := i + 1
			v, err := renderFieldValue(child, idx, vt)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case strings.HasPrefix(typ, "map<") && strings.HasSuffix(typ, ">"):
		inner := typ[len("map<") : len(typ)-1]
		if i := strings.Index(inner, ","); i >= 0 {
			inner = strings.TrimSpace(inner[i+1:])
		}
		child := Field{Type: inner, ItemFields: f.ItemFields, Fields: f.Fields, KeyPatterns: f.KeyPatterns}
		v, err := renderFieldValue(child, index, vt)
		if err != nil {
			return nil, err
		}
		return map[string]any{"key": v}, nil
	case typ == "string", typ == "timestamp", typ == "datetime", typ == "json", typ == "bytes":
		if index > 0 {
			return fmt.Sprintf("item_%d", index), nil
		}
		return "", nil
	case typ == "integer":
		return index, nil
	case typ == "float", typ == "decimal":
		return float64(index), nil
	case typ == "boolean":
		return false, nil
	case typ == "any":
		return nil, nil
	default:
		return nil, fmt.Errorf("RenderFieldValue: unsupported type %q", f.Type)
	}
}

func fieldInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

func uniqueKeyValue(typ string, index int, vt ValueTypes) any {
	original := strings.ReplaceAll(typ, "_types.", "")
	t := original
	if ct, ok := vt.CustomTypes[t]; ok {
		t = ct.Family
	}
	switch t {
	case "integer", "number", "float", "decimal":
		return index
	case "boolean":
		return index%2 == 1
	case "timestamp", "datetime":
		mode := "datetime"
		if ct, ok := vt.CustomTypes[original]; ok {
			if m, ok := ct.Params["mode"].(string); ok {
				mode = m
			}
		}
		switch mode {
		case "date":
			return fmt.Sprintf("2024-01-%02d", index)
		case "time":
			return fmt.Sprintf("%02d:00:00", index)
		default:
			return fmt.Sprintf("2024-01-01T00:00:%02dZ", index)
		}
	default:
		return fmt.Sprintf("item_%d", index)
	}
}

// Package contractspec 定义 _meta 字段规范的共享类型与加载入口。
package contractspec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Schema struct {
	ID         string                `yaml:"id"`
	Type       string                `yaml:"type"`
	Title      string                `yaml:"title"`
	Targets    Targets               `yaml:"targets"`
	ValueTypes ValueTypes            `yaml:"-"`
	NodeKinds  map[string]SchemaKind `yaml:"-"`
	// PropertyKeys 是 object.properties[] 条目的键规范；槽位键只允许出现在这里。
	PropertyKeys []Field `yaml:"-"`
	Fields       []Field `yaml:"fields"`
	Rules        []Rule  `yaml:"rules"`
}

// SharedTypes 是 _meta/_types.yaml 的严格解析结果：类型族是元层词汇，
// 由所有契约类型共享；项目级自定义类型在 contracts/data/types.yaml。
type SharedTypes struct {
	ID           string                     `yaml:"id"`
	Title        string                     `yaml:"title"`
	TypeFamilies map[string]ValueTypeFamily `yaml:"basic_types"`
	Structures   map[string]Structure       `yaml:"structures"`
	Schemas      map[string]SchemaKind      `yaml:"schemas"`
	ParamTargets map[string]string          `yaml:"param_targets"`
}

// Structure 是模板字段的数据结构声明：list/set/array/map/object。
type Structure struct {
	Description string           `yaml:"description"`
	Fields      []Field          `yaml:"fields"`
	Params      map[string]Field `yaml:"params"`
}

// CustomTypesDoc 是 contracts/data/types.yaml 的严格解析结果。
// 自定义类型是 data 层词汇，随项目开发增长，不放在 _meta。
type CustomTypesDoc struct {
	ID            string                     `yaml:"id"`
	Summary       string                     `yaml:"summary"`
	SchemaVersion int                        `yaml:"schema_version"`
	Stability     string                     `yaml:"stability"`
	DoNot         []string                   `yaml:"do_not"`
	CustomTypes   map[string]CustomValueType `yaml:"custom_types"`
}

const customTypesRel = "contracts/data/types.yaml"

// SchemaAlias 在字段规范的 types 列表中代表 schemas 下声明的全部节点种类。
// 使用它可避免在每处 schema 槽位重复枚举 object/array/scalar/ref。
const SchemaAlias = "_types.schema"

// SchemaNodeKind 返回 schema 节点的种类：ref 优先，其次由 type 的 struct/array/其他值决定。
func SchemaNodeKind(node map[string]any, kinds map[string]SchemaKind) (string, bool) {
	_, hasRef := node["ref"]
	typ, _ := node["type"].(string)
	if hasRef && typ != "" {
		return "", false
	}
	if hasRef {
		return "ref", kinds["ref"].Keys != nil
	}
	switch typ {
	case "":
		return "", false
	case "object":
		return "object", kinds["object"].Keys != nil
	case "array":
		return "array", kinds["array"].Keys != nil
	default:
		return "scalar", kinds["scalar"].Keys != nil
	}
}

// SchemaKind 描述一种 schema 节点的判别键与允许键；判别键隐式必填。
// Keys 复用通用 Field 规范；object 的属性条目由 properties.item_fields 承载。
type SchemaKind struct {
	Description string  `yaml:"description"`
	Keys        []Field `yaml:"keys"`
}

type Rule struct {
	ID            string   `yaml:"id"`
	Text          string   `yaml:"text"`
	ImplementedBy []string `yaml:"implemented_by"`
}

// Targets 描述契约实例的落盘位置、ID 模板与命名。
type Targets struct {
	Dir    string `yaml:"dir"`
	Glob   string `yaml:"glob"`
	ID     string `yaml:"id"`
	Naming string `yaml:"naming"`
}

// FileID 按 targets.id 模板生成实例 ID。
func (t Targets) FileID(file string) string {
	if t.ID == "" || !strings.Contains(t.ID, "<file>") {
		return ""
	}
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	return strings.ReplaceAll(t.ID, "<file>", stem)
}

type ValueTypes struct {
	TypeFamilies map[string]ValueTypeFamily `yaml:"basic_types"`
	CustomTypes  map[string]CustomValueType `yaml:"custom_types"`
	Structures   map[string]Structure       `yaml:"structures"`
	Schemas      map[string]SchemaKind      `yaml:"schemas"`
	ParamTargets map[string]string          `yaml:"param_targets"`
}

// ParamTargetKinds 是 param_targets 允许的消费方：openapi=生成到 OpenAPI，
// storage=仅存储层，runtime=业务运行时。
var ParamTargetKinds = map[string]bool{
	"openapi": true,
	"storage": true,
	"runtime": true,
}

// CustomValueType 是自定义值类型：一个类型族加上固定参数，供表列与 schema 共享。
type CustomValueType struct {
	Family      string         `yaml:"family"`
	Description string         `yaml:"description"`
	Params      map[string]any `yaml:"params"`
}

// ValueTypeFamily 描述一个类型族及其允许的扩展字段。
// Params 的 required 表示使用该族时必须声明该扩展字段。
type ValueTypeFamily struct {
	Description string           `yaml:"description"`
	Params      map[string]Field `yaml:"params"`
}

type Field struct {
	Name string `yaml:"name"`
	Type string `yaml:"type"`
	// Types 声明 schema 节点允许的种类；与 Type 互斥，用于代替抽象的 type: schema。
	Types    []string `yaml:"types"`
	Required bool     `yaml:"required"`
	Const    any      `yaml:"const"`
	Enum     []any    `yaml:"enum"`
	Allowed  []any    `yaml:"allowed"`
	// Soft 标记无结构校验的 soft zone；当前只允许 type: any。
	Soft bool `yaml:"soft"`
	Min  any  `yaml:"min"`
	Max  any  `yaml:"max"`
	// MinItems/MaxItems 约束集合元素数：list/set/map。
	MinItems any `yaml:"min_items"`
	MaxItems any `yaml:"max_items"`
	// MinLen/MaxLen 约束字符串长度。
	MinLen  any    `yaml:"min_len"`
	MaxLen  any    `yaml:"max_len"`
	Len     any    `yaml:"len"`
	Pattern string `yaml:"pattern"`
	// KeyPatterns 是 map 键的正则列表；string key 时任一匹配即可。
	KeyPatterns []string `yaml:"key_patterns"`
	Description string   `yaml:"description"`
	Examples    []any    `yaml:"examples"`
	Default     any      `yaml:"default"`
	Generated   bool     `yaml:"generated"`
	Write       string   `yaml:"write"`
	Unique      bool     `yaml:"unique"`
	// UniqueKey 声明 list<object> 按哪个字段判重，例如 properties 用 name。
	UniqueKey  string  `yaml:"unique_key"`
	Fields     []Field `yaml:"fields"`
	ItemFields []Field `yaml:"item_fields"`
	// Unordered 标记运行时注入的字段（如类型族参数），不参与模板顺序校验。
	Unordered bool `yaml:"-"`
}

// LoadSchemaByType 在 contracts/_meta/*.yaml 中按 type 查找 meta。
// 文件本身即 registry，不维护第二份类型映射；无 type 的辅助文件跳过。
func LoadSchemaByType(root, typ string) (*Schema, string, error) {
	dir := filepath.Join(root, "contracts/_meta")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, "", err
	}
	sort.Strings(files)
	var found *Schema
	var foundPath string
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, "", err
		}
		var head struct {
			Type string `yaml:"type"`
		}
		if err := yaml.Unmarshal(b, &head); err != nil {
			return nil, "", fmt.Errorf("%s: %w", file, err)
		}
		if head.Type != typ {
			continue
		}
		if found != nil {
			return nil, "", fmt.Errorf("duplicate meta type %q: %s and %s", typ, foundPath, file)
		}
		s, err := LoadSchema(file)
		if err != nil {
			return nil, "", err
		}
		found = s
		foundPath = file
	}
	if found == nil {
		return nil, "", fmt.Errorf("no meta schema for type %q under %s", typ, dir)
	}
	if needsSharedTypes(typ) && len(found.ValueTypes.TypeFamilies) == 0 {
		return nil, "", fmt.Errorf("%s: missing %s (type_families is empty)", foundPath, filepath.Join(dir, "_types.yaml"))
	}
	if needsSharedSchemas(typ) && len(found.NodeKinds) == 0 {
		return nil, "", fmt.Errorf("%s: missing schemas (kinds is empty) in %s", foundPath, filepath.Join(dir, "_types.yaml"))
	}
	if needsSharedTypes(typ) {
		doc, err := LoadCustomTypes(root, found.ValueTypes.TypeFamilies)
		if err != nil {
			return nil, "", err
		}
		found.ValueTypes.CustomTypes = doc.CustomTypes
	}
	return found, foundPath, nil
}

// needsSharedTypes 标记依赖 `_meta/_types.yaml` 类型族的契约类型。
func needsSharedTypes(typ string) bool {
	switch typ {
	case "data.tables", "data.entities", "data.queries", "cap.services", "cap.events":
		return true
	}
	return false
}

func needsSharedSchemas(typ string) bool {
	switch typ {
	case "cap.services", "cap.events":
		return true
	}
	return false
}

// LoadSharedTypes 严格解析 `_meta/_types.yaml`；文件缺失时返回 os.ErrNotExist。
func LoadSharedTypes(dir string) (*SharedTypes, error) {
	path := filepath.Join(dir, "_types.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var t SharedTypes
	if err := dec.Decode(&t); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if t.ID != "meta.types" {
		return nil, fmt.Errorf("%s: id must be meta.types, got %q", path, t.ID)
	}
	if len(t.TypeFamilies) == 0 {
		return nil, fmt.Errorf("%s: basic_types must not be empty", path)
	}
	if len(t.Structures) == 0 {
		return nil, fmt.Errorf("%s: structures must not be empty", path)
	}
	if err := validateTypeFamilies(path, t.TypeFamilies); err != nil {
		return nil, err
	}
	if err := validateSchemas(path, t.Schemas); err != nil {
		return nil, err
	}
	if err := validateStructures(path, t.Structures, t.TypeFamilies); err != nil {
		return nil, err
	}
	if err := validateParamTargets(path, t.TypeFamilies, t.ParamTargets); err != nil {
		return nil, err
	}
	return &t, nil
}

// validateParamTargets 校验 param_targets 的键必须是已声明参数，值必须是已知消费方。
// "每个参数必须有消费方"由 checker 报 schema.types，保持错误分类不变。
func validateParamTargets(path string, families map[string]ValueTypeFamily, targets map[string]string) error {
	// default 是 custom_types 的通用参数，不属于任何 family 的 params。
	known := map[string]bool{"default": true}
	for _, fam := range families {
		for name := range fam.Params {
			known[name] = true
		}
	}
	for name, target := range targets {
		if !known[name] {
			return fmt.Errorf("%s: param_targets.%s does not match any family param", path, name)
		}
		if !ParamTargetKinds[target] {
			return fmt.Errorf("%s: param_targets.%s has invalid target %q (openapi/storage/runtime)", path, name, target)
		}
	}
	return nil
}

// LoadCustomTypes 严格解析 contracts/data/types.yaml；文件缺失视为没有自定义类型。
func LoadCustomTypes(root string, families map[string]ValueTypeFamily) (*CustomTypesDoc, error) {
	path := filepath.Join(root, customTypesRel)
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CustomTypesDoc{ID: "data.types", CustomTypes: map[string]CustomValueType{}}, nil
		}
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var doc CustomTypesDoc
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if doc.ID != "data.types" {
		return nil, fmt.Errorf("%s: id must be data.types, got %q", path, doc.ID)
	}
	if doc.SchemaVersion != 1 {
		return nil, fmt.Errorf("%s: schema_version must be 1", path)
	}
	if strings.TrimSpace(doc.Summary) == "" {
		return nil, fmt.Errorf("%s: summary must not be empty", path)
	}
	if len(doc.DoNot) == 0 {
		return nil, fmt.Errorf("%s: do_not must not be empty", path)
	}
	switch doc.Stability {
	case "stable", "draft", "deprecated":
	default:
		return nil, fmt.Errorf("%s: stability must be stable/draft/deprecated, got %q", path, doc.Stability)
	}
	if err := validateCustomTypes(path, families, doc.CustomTypes); err != nil {
		return nil, err
	}
	return &doc, nil
}

// schemaNodeRequiredKeys 是 SchemaNodeKind 硬编码的四种节点及其结构键。
// 加新 kind 必须同时改 SchemaNodeKind，元规范不能单方面扩展种类。
var schemaNodeRequiredKeys = map[string][]string{
	"object": {"type", "properties"},
	"array":  {"type", "items"},
	"scalar": {"type"},
	"ref":    {"ref"},
}

// validateStructures 校验 _types.structures：key 必须是已知容器，
// 每个 param 的 type 必须命中 basic_types，required 与 default 互斥。
func validateStructures(path string, structures map[string]Structure, families map[string]ValueTypeFamily) error {
	if len(structures) == 0 {
		return fmt.Errorf("%s: structures must not be empty", path)
	}
	known := map[string]bool{"list": true, "set": true, "array": true, "map": true, "object": true}
	allowedParams := map[string]map[string]bool{
		"list":   {"min_items": true, "max_items": true},
		"set":    {"min_items": true, "max_items": true},
		"array":  {"len": true},
		"map":    {"min_items": true, "max_items": true, "key_patterns": true},
		"object": {},
	}
	for name, st := range structures {
		if !known[name] {
			return fmt.Errorf("%s: unknown structure %s (list/set/array/map/object)", path, name)
		}
		if strings.TrimSpace(st.Description) == "" {
			return fmt.Errorf("%s: structure %s missing description", path, name)
		}
		switch name {
		case "map":
			for _, required := range []string{"key", "value"} {
				f, ok := FindField(st.Fields, required)
				if !ok || f.Type != "*" || !f.Required {
					return fmt.Errorf("%s: structure map field %s must be type \"*\" and required", path, required)
				}
			}
			key, _ := FindField(st.Fields, "key")
			if len(key.Enum) != 2 || !enumHas(key.Enum, "string") || !enumHas(key.Enum, "integer") {
				return fmt.Errorf("%s: structure map key enum must be [string, integer]", path)
			}
		case "list", "set", "array":
			f, ok := FindField(st.Fields, "item")
			if !ok || f.Type != "*" || !f.Required {
				return fmt.Errorf("%s: structure %s field item must be type \"*\" and required", path, name)
			}
		}
		for pname, p := range st.Params {
			if strings.TrimSpace(pname) == "" {
				return fmt.Errorf("%s: structure %s param name must not be empty", path, name)
			}
			if !allowedParams[name][pname] {
				return fmt.Errorf("%s: structure %s does not allow param %s", path, name, pname)
			}
			if p.Name != "" && p.Name != pname {
				return fmt.Errorf("%s: structure %s param %s has mismatched name %q", path, name, pname, p.Name)
			}
			t := strings.TrimPrefix(p.Type, "_types.")
			if _, ok := families[t]; !ok {
				if _, ok := structures[t]; !ok && !strings.Contains(t, "<") {
					return fmt.Errorf("%s: structure %s param %s has unknown type %q", path, name, pname, p.Type)
				}
			}
			if p.Required && p.Default != nil {
				return fmt.Errorf("%s: structure %s param %s: required and default are mutually exclusive", path, name, pname)
			}
		}
	}
	return nil
}

// validateSchemas 校验 _types.schemas：必须定义 struct/array/scalar/ref 四种节点，
// 判别键必填，struct 的 properties 必须有 item_fields，其他节点不得定义 properties。
func validateSchemas(path string, schemas map[string]SchemaKind) error {
	if len(schemas) == 0 {
		return fmt.Errorf("%s: schemas must not be empty", path)
	}
	for kind, required := range schemaNodeRequiredKeys {
		spec, ok := schemas[kind]
		if !ok {
			return fmt.Errorf("%s: schemas must define %s", path, kind)
		}
		for _, name := range required {
			key, ok := findSchemaKey(spec.Keys, name)
			if !ok {
				return fmt.Errorf("%s: schema %s keys must define %s", path, kind, name)
			}
			if !key.Required {
				return fmt.Errorf("%s: schema %s key %s must be required", path, kind, name)
			}
		}
	}
	for name := range schemas {
		if _, ok := schemaNodeRequiredKeys[name]; !ok {
			return fmt.Errorf("%s: unknown schema %s (SchemaNodeKind supports struct/array/scalar/ref)", path, name)
		}
	}
	keyTypes := map[string]bool{"": true, "_types.string": true, "_types.boolean": true, "_types.integer": true, "_types.any": true, "_types.map": true, "_types.list<_types.object>": true, "_types.list<_types.string>": true}
	for name, kind := range schemas {
		if strings.TrimSpace(kind.Description) == "" {
			return fmt.Errorf("%s: schema %s missing description", path, name)
		}
		if err := validateSchemaKeys(path, "schema "+name, kind.Keys, keyTypes); err != nil {
			return err
		}
	}
	structKind := schemas["object"]
	propsKey, ok := findSchemaKey(structKind.Keys, "properties")
	if !ok || len(propsKey.ItemFields) == 0 {
		return fmt.Errorf("%s: schema struct properties must define item_fields", path)
	}
	if err := validateSchemaKeys(path, "schema struct properties item_fields", propsKey.ItemFields, keyTypes); err != nil {
		return err
	}
	for name, kind := range schemas {
		if name != "object" {
			if _, ok := findSchemaKey(kind.Keys, "properties"); ok {
				return fmt.Errorf("%s: schema %s must not define properties (only struct has properties)", path, name)
			}
		}
	}
	return nil
}

// sharedObjectPropertyKeys 返回 struct.properties 的条目键规范。
func sharedObjectPropertyKeys(schemas map[string]SchemaKind) []Field {
	if obj, ok := schemas["object"]; ok {
		if key, ok := findSchemaKey(obj.Keys, "properties"); ok {
			return key.ItemFields
		}
	}
	return nil
}

// findSchemaKey 按名返回 schema 键规范。
func findSchemaKey(keys []Field, name string) (Field, bool) {
	for _, key := range keys {
		if key.Name == name {
			return key, true
		}
	}
	return Field{}, false
}

// validateSchemaKeys 校验一组 schema 键规范：非空、唯一、类型合法、说明完整。
func validateSchemaKeys(path, where string, keys []Field, keyTypes map[string]bool) error {
	if len(keys) == 0 {
		return fmt.Errorf("%s: %s keys must not be empty", path, where)
	}
	seen := map[string]bool{}
	for _, key := range keys {
		if strings.TrimSpace(key.Name) == "" {
			return fmt.Errorf("%s: %s key name must not be empty", path, where)
		}
		if seen[key.Name] {
			return fmt.Errorf("%s: %s duplicate key %s", path, where, key.Name)
		}
		seen[key.Name] = true
		if key.Type != "" && !keyTypes[key.Type] {
			return fmt.Errorf("%s: %s key %s has invalid type %q", path, where, key.Name, key.Type)
		}
		if len(key.Types) > 0 {
			if key.Type != "" && key.Type != "map" {
				return fmt.Errorf("%s: %s key %s: type and types are mutually exclusive", path, where, key.Name)
			}
			if key.Const != nil || len(key.Enum) > 0 || len(key.Allowed) > 0 || key.Pattern != "" || key.Min != nil || key.Max != nil ||
				key.MinItems != nil || key.MaxItems != nil || key.MinLen != nil || key.MaxLen != nil || key.Len != nil ||
				key.Unique || key.UniqueKey != "" || key.Soft {
				return fmt.Errorf("%s: %s key %s: types only accepts required/description/default/key_patterns", path, where, key.Name)
			}
			kindSeen := map[string]bool{}
			for _, kind := range key.Types {
				if kind == SchemaAlias {
					if kindSeen[kind] {
						return fmt.Errorf("%s: %s key %s has duplicate schema kind %q", path, where, key.Name, kind)
					}
					kindSeen[kind] = true
					continue
				}
				if _, ok := schemaNodeRequiredKeys[kind]; !ok {
					return fmt.Errorf("%s: %s key %s has unknown schema kind %q", path, where, key.Name, kind)
				}
				if kindSeen[kind] {
					return fmt.Errorf("%s: %s key %s has duplicate schema kind %q", path, where, key.Name, kind)
				}
				kindSeen[kind] = true
			}
		} else if key.Types != nil {
			return fmt.Errorf("%s: %s key %s: types must not be empty", path, where, key.Name)
		} else if key.Type == "map" {
			return fmt.Errorf("%s: %s key %s: map requires types", path, where, key.Name)
		} else if key.Type == "" {
			return fmt.Errorf("%s: %s key %s must declare type or types", path, where, key.Name)
		}
		if key.Pattern != "" {
			if _, err := regexp.Compile(key.Pattern); err != nil {
				return fmt.Errorf("%s: %s key %s has invalid pattern %q: %w", path, where, key.Name, key.Pattern, err)
			}
		}
		if key.UniqueKey != "" && strings.ReplaceAll(key.Type, "_types.", "") != "list<object>" {
			return fmt.Errorf("%s: %s key %s: unique_key requires list<object>", path, where, key.Name)
		}
		if len(key.Types) > 1 && key.Default != nil {
			return fmt.Errorf("%s: %s key %s: types with multiple kinds must not declare default", path, where, key.Name)
		}
		if key.Default != nil && len(key.Enum) > 0 && !enumHas(key.Enum, fmt.Sprint(key.Default)) {
			return fmt.Errorf("%s: %s key %s: default %v not in enum %v", path, where, key.Name, key.Default, key.Enum)
		}
		if strings.TrimSpace(key.Description) == "" {
			return fmt.Errorf("%s: %s key %s missing description", path, where, key.Name)
		}
	}
	return nil
}

func LoadSchema(path string) (*Schema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	// 字段规范自身也必须严格：拼错的键（如 requred）若被静默忽略，
	// 会把硬约束降级成无效文本，且不会触发任何 contract 实例错误。
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var s Schema
	if err := dec.Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// 类型族是项目级共享源；单文件加载时若同目录存在 _types.yaml 就注入，
	// LoadSchemaByType 再对依赖类型族的契约类型强制要求它存在。
	if t, err := LoadSharedTypes(filepath.Dir(path)); err == nil {
		s.ValueTypes = ValueTypes{
			TypeFamilies: t.TypeFamilies,
			Structures:   t.Structures,
			Schemas:      t.Schemas,
			ParamTargets: t.ParamTargets,
		}
		s.NodeKinds = t.Schemas
		s.PropertyKeys = sharedObjectPropertyKeys(t.Schemas)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if s.Type == "" || len(s.Fields) == 0 {
		return nil, fmt.Errorf("%s: empty schema (type/fields)", path)
	}
	if s.ID != "meta."+s.Type {
		return nil, fmt.Errorf("%s: id must be meta.%s, got %q", path, s.Type, s.ID)
	}
	injectFamilyParams(&s)
	return &s, nil
}

// injectFamilyParams 把所有类型族的扩展字段名加入 columns.item_fields。
// 类型族参数各自只对特定族合法，具体类型/范围由 contractcheck 按族校验；
// 这里只负责让通用字段校验不把扩展字段当成未知字段。
func injectFamilyParams(s *Schema) {
	idx := -1
	for i := range s.Fields {
		if s.Fields[i].Name == "columns" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	seen := make(map[string]bool, len(s.Fields[idx].ItemFields))
	for _, f := range s.Fields[idx].ItemFields {
		seen[f.Name] = true
	}
	for _, fam := range s.ValueTypes.TypeFamilies {
		for name := range fam.Params {
			if seen[name] {
				continue
			}
			s.Fields[idx].ItemFields = append(s.Fields[idx].ItemFields, Field{Name: name, Type: "any", Required: false, Unordered: true})
			seen[name] = true
		}
	}
}

func validateTypeFamilies(path string, families map[string]ValueTypeFamily) error {
	for name, fam := range families {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(fam.Description) == "" {
			return fmt.Errorf("%s: family %s must have a name and description", path, name)
		}
	}
	return nil
}

func validateCustomTypes(path string, families map[string]ValueTypeFamily, customs map[string]CustomValueType) error {
	for name, custom := range customs {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(custom.Description) == "" {
			return fmt.Errorf("%s: custom type %s must have a name and description", path, name)
		}
		if _, ok := families[name]; ok {
			return fmt.Errorf("%s: custom type %s collides with a family", path, name)
		}
		fam, ok := families[custom.Family]
		if !ok {
			return fmt.Errorf("%s: custom type %s uses unknown family %s", path, name, custom.Family)
		}
		for pname := range custom.Params {
			if pname == "default" {
				continue
			}
			if _, ok := fam.Params[pname]; !ok {
				return fmt.Errorf("%s: custom type %s param %s not allowed by family %s", path, name, pname, custom.Family)
			}
		}
		for pname, def := range fam.Params {
			if def.Required {
				if _, ok := custom.Params[pname]; !ok {
					return fmt.Errorf("%s: custom type %s missing required param %s", path, name, pname)
				}
			}
		}
	}
	return nil
}

func FindField(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

func enumHas(values []any, want string) bool {
	for _, v := range values {
		if s, ok := v.(string); ok && s == want {
			return true
		}
	}
	return false
}

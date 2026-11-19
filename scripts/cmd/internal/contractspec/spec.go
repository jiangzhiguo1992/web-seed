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

// SharedTypes 是 _meta/_types.yaml 的严格解析结果：类型族由所有契约类型共享，
// 不再挂靠在 data.tables 的字段规范里。
type SharedTypes struct {
	ID           string                     `yaml:"id"`
	Title        string                     `yaml:"title"`
	TypeFamilies map[string]ValueTypeFamily `yaml:"type_families"`
	CustomTypes  map[string]CustomValueType `yaml:"custom_types"`
}

// SharedSchemas 是 _meta/_schemas.yaml 的严格解析结果：services.schemas 与
// events.payload 共用的 schema 节点语法。
// 两层语法：Kinds 描述值域节点，Property 描述 object.properties[] 的槽位条目。
// 节点种类就是 Kinds 的键集，不维护第二份 one_of 清单。
type SharedSchemas struct {
	ID    string                `yaml:"id"`
	Title string                `yaml:"title"`
	Kinds map[string]SchemaKind `yaml:"kinds"`
}

// SchemaNodeKind 返回 schema 节点的种类：ref 优先，其次由 type 的 object/array/其他值决定。
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
	File   string `yaml:"file"`
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
	TypeFamilies map[string]ValueTypeFamily `yaml:"type_families"`
	CustomTypes  map[string]CustomValueType `yaml:"custom_types"`
}

// SchemaParamTargets 声明 family param 的消费方，避免新增参数后静默丢失：
// openapi=生成到 OpenAPI，storage=仅存储层，runtime=业务运行时。
var SchemaParamTargets = map[string]string{
	"min_len":            "openapi",
	"max_len":            "openapi",
	"pattern":            "openapi",
	"min":                "openapi",
	"max":                "openapi",
	"default":            "openapi",
	"fixed":              "storage",
	"normalize":          "runtime",
	"bits":               "storage",
	"unsigned":           "storage",
	"precision":          "storage",
	"scale":              "storage",
	"mode":               "storage",
	"timezone":           "storage",
	"fractional_seconds": "storage",
	"binary":             "storage",
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
	Types       []string `yaml:"types"`
	Required    bool     `yaml:"required"`
	Const       any      `yaml:"const"`
	Enum        []any    `yaml:"enum"`
	Allowed     []any    `yaml:"allowed"`
	Min         any      `yaml:"min"`
	Max         any      `yaml:"max"`
	MinItems    any      `yaml:"min_items"`
	Pattern     string   `yaml:"pattern"`
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
		return nil, "", fmt.Errorf("%s: missing %s (kinds is empty)", foundPath, filepath.Join(dir, "_schemas.yaml"))
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
	vt := ValueTypes{TypeFamilies: t.TypeFamilies, CustomTypes: t.CustomTypes}
	if t.ID != "meta.types" {
		return nil, fmt.Errorf("%s: id must be meta.types, got %q", path, t.ID)
	}
	if len(vt.TypeFamilies) == 0 {
		return nil, fmt.Errorf("%s: type_families must not be empty", path)
	}
	if err := validateValueTypes(path, vt); err != nil {
		return nil, err
	}
	return &t, nil
}

// schemaNodeRequiredKeys 是 SchemaNodeKind 硬编码的四种节点及其结构键。
// 加新 kind 必须同时改 SchemaNodeKind，元规范不能单方面扩展种类。
var schemaNodeRequiredKeys = map[string][]string{
	"object": {"type", "properties"},
	"array":  {"type", "items"},
	"scalar": {"type"},
	"ref":    {"ref"},
}

// LoadSharedSchemas 严格解析 `_meta/_schemas.yaml`；文件缺失时返回 os.ErrNotExist。
func LoadSharedSchemas(dir string) (*SharedSchemas, error) {
	path := filepath.Join(dir, "_schemas.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var sh SharedSchemas
	if err := dec.Decode(&sh); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if sh.ID != "meta.schemas" {
		return nil, fmt.Errorf("%s: id must be meta.schemas, got %q", path, sh.ID)
	}
	if len(sh.Kinds) == 0 {
		return nil, fmt.Errorf("%s: kinds must not be empty", path)
	}
	// SchemaNodeKind 硬编码了四种节点；元规范不能单方面增减 kind。
	for kind, required := range schemaNodeRequiredKeys {
		spec, ok := sh.Kinds[kind]
		if !ok {
			return nil, fmt.Errorf("%s: kinds must define %s", path, kind)
		}
		for _, name := range required {
			key, ok := findSchemaKey(spec.Keys, name)
			if !ok {
				return nil, fmt.Errorf("%s: kind %s keys must define %s", path, kind, name)
			}
			if !key.Required {
				return nil, fmt.Errorf("%s: kind %s key %s must be required", path, kind, name)
			}
		}
	}
	for name := range sh.Kinds {
		if _, ok := schemaNodeRequiredKeys[name]; !ok {
			return nil, fmt.Errorf("%s: unknown kind %s (SchemaNodeKind supports object/array/scalar/ref)", path, name)
		}
	}
	keyTypes := map[string]bool{"": true, "string": true, "boolean": true, "integer": true, "any": true, "map": true, "list<object>": true, "list<string>": true}
	for name, kind := range sh.Kinds {
		if strings.TrimSpace(kind.Description) == "" {
			return nil, fmt.Errorf("%s: kind %s missing description", path, name)
		}
		if err := validateSchemaKeys(path, "kind "+name, kind.Keys, keyTypes); err != nil {
			return nil, err
		}
	}
	object := sh.Kinds["object"]
	propsKey, ok := findSchemaKey(object.Keys, "properties")
	if !ok || len(propsKey.ItemFields) == 0 {
		return nil, fmt.Errorf("%s: kind object properties must define item_fields", path)
	}
	if err := validateSchemaKeys(path, "kind object properties item_fields", propsKey.ItemFields, keyTypes); err != nil {
		return nil, err
	}
	for name, kind := range sh.Kinds {
		if name != "object" {
			if _, ok := findSchemaKey(kind.Keys, "properties"); ok {
				return nil, fmt.Errorf("%s: kind %s must not define properties (only object has properties)", path, name)
			}
		}
	}
	return &sh, nil
}

// sharedObjectPropertyKeys 返回 object.properties 的条目键规范。
func sharedObjectPropertyKeys(sh *SharedSchemas) []Field {
	if obj, ok := sh.Kinds["object"]; ok {
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
			if key.Const != nil || len(key.Enum) > 0 || len(key.Allowed) > 0 || key.Pattern != "" || key.Min != nil || key.Max != nil || key.MinItems != nil || key.Unique {
				return fmt.Errorf("%s: %s key %s: types only accepts required/description/default", path, where, key.Name)
			}
			kindSeen := map[string]bool{}
			for _, kind := range key.Types {
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
		if key.UniqueKey != "" && key.Type != "list<object>" {
			return fmt.Errorf("%s: %s key %s: unique_key requires list<object>", path, where, key.Name)
		}
		if len(key.Types) > 1 && key.Default != nil {
			return fmt.Errorf("%s: %s key %s: types with multiple kinds must not declare default", path, where, key.Name)
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
		s.ValueTypes = ValueTypes{TypeFamilies: t.TypeFamilies, CustomTypes: t.CustomTypes}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if sh, err := LoadSharedSchemas(filepath.Dir(path)); err == nil {
		s.NodeKinds = sh.Kinds
		s.PropertyKeys = sharedObjectPropertyKeys(sh)
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

func validateValueTypes(path string, vt ValueTypes) error {
	for name, fam := range vt.TypeFamilies {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(fam.Description) == "" {
			return fmt.Errorf("%s: family %s must have a name and description", path, name)
		}
	}
	for name, custom := range vt.CustomTypes {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(custom.Description) == "" {
			return fmt.Errorf("%s: custom type %s must have a name and description", path, name)
		}
		if _, ok := vt.TypeFamilies[name]; ok {
			return fmt.Errorf("%s: custom type %s collides with a family", path, name)
		}
		fam, ok := vt.TypeFamilies[custom.Family]
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

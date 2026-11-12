// Package contractspec 定义 _meta 字段规范的共享类型与加载入口。
package contractspec

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Schema struct {
	ID          string      `yaml:"id"`
	Type        string      `yaml:"type"`
	Title       string      `yaml:"title"`
	Targets     Targets     `yaml:"targets"`
	ColumnTypes ColumnTypes `yaml:"column_types"`
	ColumnRefs  ColumnRefs  `yaml:"column_refs"`
	Fields      []Field     `yaml:"fields"`
	Rules       []Rule      `yaml:"rules"`
}

type Rule struct {
	ID            string   `yaml:"id"`
	Text          string   `yaml:"text"`
	ImplementedBy []string `yaml:"implemented_by"`
}

// Targets 描述契约实例的落盘位置、ID 模板与例外；checker/contract-new 共用。
type Targets struct {
	Dir       string            `yaml:"dir"`
	Glob      string            `yaml:"glob"`
	File      string            `yaml:"file"`
	ID        string            `yaml:"id"`
	Naming    string            `yaml:"naming"`
	Exception []TargetException `yaml:"exception"`
}

// TargetException 显式覆盖某个文件的 ID；Kind: base 标记基类。
type TargetException struct {
	File string `yaml:"file"`
	ID   string `yaml:"id"`
	Kind string `yaml:"kind"`
}

// ExceptionFor 返回指定文件名对应的显式例外。
func (t Targets) ExceptionFor(file string) (TargetException, bool) {
	for _, ex := range t.Exception {
		if ex.File == file {
			return ex, true
		}
	}
	return TargetException{}, false
}

// FileID 按 targets.id 模板生成实例 ID；例外优先。
func (t Targets) FileID(file string) string {
	if ex, ok := t.ExceptionFor(file); ok {
		return ex.ID
	}
	if t.ID == "" || !strings.Contains(t.ID, "<file>") {
		return ""
	}
	stem := strings.TrimSuffix(file, filepath.Ext(file))
	return strings.ReplaceAll(t.ID, "<file>", stem)
}

// Base 返回 kind: base 的例外；没有则返回 false。
func (t Targets) Base() (TargetException, bool) {
	for _, ex := range t.Exception {
		if ex.Kind == "base" {
			return ex, true
		}
	}
	return TargetException{}, false
}

type ColumnTypes struct {
	ValueTypeFamilies map[string]ValueTypeFamily `yaml:"families"`
}

// ColumnRefs 描述 columns[].type 允许引用的命名空间，与类型族是不同维度。
type ColumnRefs struct {
	Prefixes   []string `yaml:"prefixes"`
	RequireCol []string `yaml:"require_col"`
}

// ValueTypeFamily 描述一个类型族及其允许的扩展字段。
// Params 的 required 表示使用该族时必须声明该扩展字段。
type ValueTypeFamily struct {
	Description string           `yaml:"description"`
	Params      map[string]Field `yaml:"params"`
}

type Field struct {
	Name        string  `yaml:"name"`
	Type        string  `yaml:"type"`
	Required    bool    `yaml:"required"`
	Const       any     `yaml:"const"`
	Enum        []any   `yaml:"enum"`
	Allowed     []any   `yaml:"allowed"`
	Min         any     `yaml:"min"`
	Max         any     `yaml:"max"`
	Pattern     string  `yaml:"pattern"`
	Description string  `yaml:"description"`
	Generates   string  `yaml:"generates"`
	Caution     string  `yaml:"caution"`
	Examples    []any   `yaml:"examples"`
	Default     any     `yaml:"default"`
	Generated   bool    `yaml:"generated"`
	Write       string  `yaml:"write"`
	Unique      bool    `yaml:"unique"`
	Fields      []Field `yaml:"fields"`
	ItemFields  []Field `yaml:"item_fields"`
}

// LoadSchemaByType 在 contracts/_meta/*.yaml 中按 type 查找 meta。
// 文件本身即 registry，不维护第二份类型映射；_types.yaml 等无 type 的文件跳过。
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
	return found, foundPath, nil
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
	if s.Type == "" || len(s.Fields) == 0 {
		return nil, fmt.Errorf("%s: empty schema (type/fields)", path)
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
	for _, fam := range s.ColumnTypes.ValueTypeFamilies {
		for name := range fam.Params {
			if seen[name] {
				continue
			}
			s.Fields[idx].ItemFields = append(s.Fields[idx].ItemFields, Field{Name: name, Type: "any", Required: false})
			seen[name] = true
		}
	}
}

func FindField(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

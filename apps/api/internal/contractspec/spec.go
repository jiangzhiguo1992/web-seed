// Package contractspec 定义 _meta 字段规范的共享类型与加载入口。
package contractspec

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Schema struct {
	ID          string      `yaml:"id"`
	Type        string      `yaml:"type"`
	Title       string      `yaml:"title"`
	ColumnTypes ColumnTypes `yaml:"column_types"`
	Fields      []Field     `yaml:"fields"`
}

type ColumnTypes struct {
	Builtin []string `yaml:"builtin"`
	Refs    []string `yaml:"refs"`
}

type Field struct {
	Name        string  `yaml:"name"`
	Type        string  `yaml:"type"`
	Required    bool    `yaml:"required"`
	Const       any     `yaml:"const"`
	Enum        []any   `yaml:"enum"`
	Allowed     []any   `yaml:"allowed"`
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

func LoadSchema(path string) (*Schema, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Schema
	if err := yaml.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Type == "" || len(s.Fields) == 0 {
		return nil, fmt.Errorf("%s: empty schema (type/fields)", path)
	}
	return &s, nil
}

func FindField(fields []Field, name string) (Field, bool) {
	for _, f := range fields {
		if f.Name == name {
			return f, true
		}
	}
	return Field{}, false
}

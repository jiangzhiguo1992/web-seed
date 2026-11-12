// Package check 实现契约校验：结构、ID、基类、列/约束/索引、引用、used_by、index 与 stability。
//
// 当前覆盖：
//   - data.tables/*.yaml 的结构、ID/文件、基类继承、列/约束/索引、used_by、do_not 具体性
//   - data/enums.yaml 的结构、item ID、值唯一性
//   - 表列引用的 data.enums.* / data.tables.*.col.* 是否存在
//   - 枚举默认值、enum 约束与列类型一致
//   - owner_module 写归属（capabilities.persistence）
//   - entity/query/capability 的表引用是否存在
//
// 不覆盖（后续扩展）：
//   - 全类型 schema
//   - graph.json / 越层引用
//   - generated diff
package check

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

type Schema = contractspec.Schema
type ColumnTypes = contractspec.ColumnTypes
type ColumnRefs = contractspec.ColumnRefs
type ValueTypeFamily = contractspec.ValueTypeFamily
type Field = contractspec.Field

var baseColumns = []string{"id", "status", "created_at", "updated_at", "deleted_at"}

type Checker struct {
	root        string
	errs        []string
	tableOwners map[string]string
}

// Run 执行全部契约校验并返回错误列表。
func Run(root string) []string {
	c := &Checker{root: root}
	c.run()
	return c.errs
}

func (c *Checker) errf(format string, args ...any) {
	c.errs = append(c.errs, fmt.Sprintf(format, args...))
}

func (c *Checker) run() {
	tableSchema, err := loadSchema(filepath.Join(c.root, "contracts/_meta/data.tables.yaml"))
	if err != nil {
		c.errf("load tables schema: %v", err)
		return
	}
	enumSchema, err := loadSchema(filepath.Join(c.root, "contracts/_meta/data.enums.yaml"))
	if err != nil {
		c.errf("load enums schema: %v", err)
		return
	}

	tableIDs, cols := c.checkTables(tableSchema)
	enumValues := c.checkEnums(enumSchema)
	c.checkEnumRefs(tableIDs, cols, enumValues)
	c.checkCrossFileTableRefs(tableIDs, cols)
	c.checkUsedBy(tableIDs)
	c.checkIndex(tableIDs)
}

func loadSchema(path string) (*Schema, error) {
	s, err := contractspec.LoadSchema(path)
	if err != nil {
		return nil, err
	}
	if err := validatePatterns(s.Fields); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if err := validateFieldTypes(s.Fields); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return s, nil
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func filterFields(fields []Field, names map[string]bool) []Field {
	out := make([]Field, 0, len(fields))
	for _, f := range fields {
		if names[f.Name] {
			out = append(out, f)
		}
	}
	return out
}

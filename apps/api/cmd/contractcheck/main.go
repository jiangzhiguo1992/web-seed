// contractcheck 是契约试点校验器。
//
// 当前覆盖：
//   - data.tables/*.yaml 的结构、ID/文件、基类继承、列/约束/索引、used_by
//   - data/enums.yaml 的结构、item ID、值唯一性
//   - 表列引用的 data.enums.* 是否存在
//   - entity/query/capability 的表引用是否存在
//
// 不覆盖（后续扩展）：
//   - 全类型 schema
//   - graph.json / 越层引用
//   - generated diff
//
// 用法（在 apps/api 下）：
//
//	go run ./cmd/contractcheck -root ../..
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourorg/web-seed/apps/api/internal/contractspec"
	"gopkg.in/yaml.v3"
)

type Schema = contractspec.Schema
type ColumnTypes = contractspec.ColumnTypes
type ValueTypeFamily = contractspec.ValueTypeFamily
type Field = contractspec.Field

var baseColumns = []string{"id", "status", "created_at", "updated_at", "deleted_at"}

type Checker struct {
	root string
	errs []string
}

func main() {
	root := flag.String("root", "../..", "repo root")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "contract-check:", err)
		os.Exit(1)
	}
	c := &Checker{root: abs}
	c.run()
	if len(c.errs) > 0 {
		fmt.Fprintf(os.Stderr, "contract-check: FAILED (%d error(s))\n", len(c.errs))
		for _, e := range c.errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}
	fmt.Println("contract-check: OK")
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
	enumIDs := c.checkEnums(enumSchema)
	c.checkEnumRefs(tableIDs, cols, enumIDs)
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

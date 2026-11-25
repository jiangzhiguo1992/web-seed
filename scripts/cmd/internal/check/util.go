// util.go YAML、集合与路径辅助函数。
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func match(pattern, s string) bool {
	ok, err := regexp.MatchString(pattern, s)
	return err == nil && ok
}

func inList(list []any, s string) bool {
	for _, x := range list {
		if fmt.Sprint(x) == s {
			return true
		}
	}
	return false
}

// normalizeContractType 把契约写法 _types.structures.list<T> / _types.basics.<name>
// 归一为 checker 内部的 List<T> / <name>；指针前缀保留。用于 Go/SQL 类型槽位，
// schema 节点与列类型不走这里。
func normalizeContractType(s string) string {
	s = strings.TrimSpace(s)
	ptr := ""
	if strings.HasPrefix(s, "*") {
		ptr, s = "*", strings.TrimSpace(strings.TrimPrefix(s, "*"))
	}
	if strings.HasPrefix(s, "_types.structures.list<") && strings.HasSuffix(s, ">") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "_types.structures.list<"), ">"))
		return ptr + "List<" + normalizeContractType(inner) + ">"
	}
	return ptr + contractspec.BareTypeName(s)
}

func asMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := map[string]any{}
		for k, v := range m {
			out[fmt.Sprint(k)] = v
		}
		return out, true
	default:
		return nil, false
	}
}

func asList(v any) []any {
	l, _ := asList2(v)
	return l
}

func asList2(v any) ([]any, bool) {
	l, ok := v.([]any)
	return l, ok
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
	}
	return 0, false
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// tableColumnTypeRe 是表列类型引用的硬约束：只允许 data.tables.<t>.col.<c>。
var tableColumnTypeRe = regexp.MustCompile(`^data\.tables\.[a-z][a-z0-9_]*\.col\.[a-z][a-z0-9_]*$`)

func isColumnType(t string, ct ValueTypes) bool {
	if isTableColumnType(t) {
		return true
	}
	family, _, ok := contractspec.SplitBasicsRef(t)
	if !ok {
		return false
	}
	if _, ok := ct.TypeFamilies[family]; ok {
		return true
	}
	if _, ok := ct.CustomTypes[family]; ok {
		return true
	}
	return false
}

func isTableColumnType(t string) bool {
	return tableColumnTypeRe.MatchString(t)
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// snakeCase 保留包内调用名；实现统一在 contractspec.SnakeCase。
func snakeCase(s string) string {
	return contractspec.SnakeCase(s)
}

func stringSet(list []any) map[string]bool {
	out := map[string]bool{}
	for _, x := range list {
		if s, ok := x.(string); ok {
			out[s] = true
		}
	}
	return out
}

func sameSet(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func glob(root, pattern string) []string {
	files, _ := filepath.Glob(filepath.Join(root, pattern))
	sort.Strings(files)
	return files
}

func walkYAML(root, dir string) []string {
	var files []string
	_ = filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func relPath(root, path string) string {
	r, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return r
}

// collectRefs 递归收集结构化文档中的 $ref。
func collectRefs(v any, fn func(string)) {
	if m, ok := asMap(v); ok {
		for k, child := range m {
			if k == "$ref" {
				if s, ok := child.(string); ok {
					fn(s)
				}
			}
			collectRefs(child, fn)
		}
		return
	}
	for _, item := range asList(v) {
		collectRefs(item, fn)
	}
}

// columnExists 判断列是否存在于已加载的物理表（含继承列）。
func (c *Checker) columnExists(table, col string) bool {
	_, ok := c.tableColumns[table][col]
	return ok
}

// normalizeYAMLKeys 把 YAML 数字键等非字符串键统一转成字符串，供 map[string]any 消费。
func normalizeYAMLKeys(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = normalizeYAMLKeys(child)
		}
		return value
	case map[any]any:
		out := map[string]any{}
		for key, child := range value {
			out[fmt.Sprint(key)] = normalizeYAMLKeys(child)
		}
		return out
	case []any:
		for i, child := range value {
			value[i] = normalizeYAMLKeys(child)
		}
		return value
	}
	return v
}

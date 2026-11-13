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

func hasNonEmptyString(m map[string]any, key string) bool {
	s, ok := m[key].(string)
	return ok && strings.TrimSpace(s) != ""
}

// tableColumnTypeRe 是表列类型引用的硬约束：只允许 data.tables.<t>.col.<c>。
var tableColumnTypeRe = regexp.MustCompile(`^data\.tables\.[a-z][a-z0-9_]*\.col\.[a-z][a-z0-9_]*$`)

func isColumnType(t string, ct ColumnTypes) bool {
	if _, ok := ct.ValueTypeFamilies[t]; ok {
		return true
	}
	return isTableColumnType(t)
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

// queryTableRefs 读取查询的 tables；查询契约不写 SQL/joins。
func queryTableRefs(m map[string]any) []string {
	var out []string
	for _, raw := range asList(m["tables"]) {
		if s, ok := raw.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// entityTableRefs 读取实体 model.tables；model 外的 tables 不再合法。
func entityTableRefs(m map[string]any) []string {
	model, _ := asMap(m["model"])
	if model == nil {
		return nil
	}
	return stringList(model["tables"])
}

// snakeCase 保留包内调用名；实现统一在 contractspec.SnakeCase。
func snakeCase(s string) string {
	return contractspec.SnakeCase(s)
}

func tableRefs(m map[string]any) []string {
	var out []string
	if t, ok := m["table"].(string); ok && t != "" {
		out = append(out, t)
	}
	for _, x := range asList(m["tables"]) {
		if s, ok := x.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
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

// util.go YAML、集合与路径辅助函数。
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
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

func isColumnType(t string, ct ColumnTypes, refPrefixes []string) bool {
	if _, ok := ct.ValueTypeFamilies[t]; ok {
		return true
	}
	for _, p := range refPrefixes {
		if !strings.HasPrefix(t, p) || t == p {
			continue
		}
		if p == "data.tables." {
			return strings.Contains(t, ".col.") && !strings.HasSuffix(t, ".col.")
		}
		return true
	}
	return false
}

func tableRefs(m map[string]any) []string {
	return append(writeTableRefs(m), readOnlyTableRefs(m)...)
}

// writeTableRefs 返回 capability.persistence 中声明为可写的表。
func writeTableRefs(m map[string]any) []string {
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

// readOnlyTableRefs 返回 capability.persistence 中声明为只读的表。
func readOnlyTableRefs(m map[string]any) []string {
	var out []string
	for _, x := range asList(m["read_only_tables"]) {
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

func stringSetOf(list []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range list {
		out[s] = true
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

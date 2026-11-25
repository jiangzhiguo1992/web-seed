package check

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestContractTypeFieldsAreDataTypes 是独立文本扫描：
// 只允许 type 承载数据类型；schema 结构判别必须是 _types.schemas.*，不能改用 kind。
func TestContractTypeFieldsAreDataTypes(t *testing.T) {
	root := repoRoot(t)
	err := filepath.Walk(filepath.Join(root, "contracts"), func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if typeScanExempt(rel) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var doc any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return err
		}
		var walk func(any, string)
		walk = func(v any, where string) {
			switch x := v.(type) {
			case map[string]any:
				for key, child := range x {
					childWhere := where + "." + key
					if s, ok := child.(string); ok {
						if key == "type" && !isDataTypeRef(s) {
							t.Errorf("%s: type %q is not a data type", childWhere, s)
						}
						if key == "json_type" && rel != "contracts/platform/http_conventions.yaml" {
							t.Errorf("%s: json_type is only allowed in contracts/platform/http_conventions.yaml", childWhere)
						}
					}
					walk(child, childWhere)
				}
			case []any:
				for i, child := range x {
					walk(child, where+"[]")
					_ = i
				}
			}
		}
		walk(doc, rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func typeScanExempt(rel string) bool {
	switch {
	case strings.HasPrefix(rel, "contracts/generated/"):
		return true
	}
	return false
}

func isDataTypeRef(s string) bool {
	s = strings.TrimPrefix(strings.TrimSpace(s), "*")
	if s == "" {
		return true
	}
	if strings.HasPrefix(s, "_types.schemas.") {
		return false
	}
	for _, prefix := range []string{"_types.", "data.", "cap.services.", "context.Context", "error", "time."} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

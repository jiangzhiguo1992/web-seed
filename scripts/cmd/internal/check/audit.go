// audit.go 暴露契约 type 词汇审计，供 make audit-types 与 CI 输出报告。
package check

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// TypeAudit 是一次类型词汇审计的结果。
type TypeAudit struct {
	Total   int
	Exempt  int
	Values  map[string]int
	Invalid []string
}

// AuditTypeVocabulary 扫描 contracts/ 下所有非生成 YAML 的 type 键。
// 它只做词汇校验，不检查 enum/entity/schema/column 是否存在，后者由 contract-check 负责。
func AuditTypeVocabulary(root string) (*TypeAudit, error) {
	shared, err := contractspec.LoadSharedTypes(filepath.Join(root, "contracts/_meta"))
	if err != nil {
		return nil, err
	}
	custom, err := contractspec.LoadCustomTypes(root, shared.TypeFamilies, shared.CommonParams)
	if err != nil {
		return nil, err
	}
	c := &Checker{
		root: root,
		valueTypes: ValueTypes{
			CommonParams: shared.CommonParams,
			TypeFamilies: shared.TypeFamilies,
			Structures:   shared.Structures,
			Schemas:      shared.Schemas,
			CustomTypes:  custom.CustomTypes,
		},
		nodeKinds: shared.Schemas,
	}
	report := &TypeAudit{Values: map[string]int{}}
	contractsRoot := filepath.Join(root, "contracts")
	err = filepath.WalkDir(contractsRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		rel := relPath(root, path)
		if typeVocabularyExempt(rel) {
			report.Exempt++
			return nil
		}
		doc, err := loadYAML(path)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		walkTypeRefs(doc, rel, func(where, value string) {
			report.Total++
			report.Values[value]++
			c.errs = nil
			c.checkTypeRef(value, where)
			report.Invalid = append(report.Invalid, c.errs...)
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(report.Invalid)
	return report, nil
}

// walkTypeRefs 递归收集任意映射中的 type 标量值。
func walkTypeRefs(v any, path string, fn func(where, value string)) {
	switch x := v.(type) {
	case map[string]any:
		_, hasKinds := x["types"]
		for key, child := range x {
			childPath := path + "." + key
			if key == "type" && !hasKinds {
				if s, ok := child.(string); ok {
					fn(childPath, s)
				}
			}
			walkTypeRefs(child, childPath, fn)
		}
	case []any:
		for i, child := range x {
			walkTypeRefs(child, fmt.Sprintf("%s[%d]", path, i), fn)
		}
	}
}

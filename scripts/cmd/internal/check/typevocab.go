// typevocab.go 通用类型词汇校验。
//
// 契约里所有 type 值只允许两种来源：
//   - _types.<basics|structures|schemas>.<name>（类型族/自定义类型、容器、schema 节点判别键）
//   - 已声明的外部引用：data.enums.* / data.entities.* / cap.services.*.schemas.* /
//     data.tables.*.col.* / context.Context / error / time.*
//
// _types.schemas.object|array|map|scalar|ref 是 schema 节点判别键，由 schema 节点校验负责；
// cap.protocols.http 是完整 OpenAPI 3.1，使用外部词汇，不参与本规则。
package check

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// typeVocabularyf 追加通用词汇错误；同一路径或同一缺失类型已有领域错误时只保留领域错误。
func (c *Checker) typeVocabularyf(path, value, format string, args ...any) {
	if path != "" && c.hasTypeErrorAt(path, value) {
		return
	}
	c.rulef("schema.type-vocabulary", format, args...)
}

// hasTypeErrorAt 判断同一契约位置或同一缺失类型是否已有更精确的领域错误。
func (c *Checker) hasTypeErrorAt(path, value string) bool {
	loc := path
	file := ""
	if i := strings.Index(loc, ".yaml"); i >= 0 {
		file = loc[:i+len(".yaml")]
		loc = strings.TrimLeft(loc[i+len(".yaml"):], ".: ")
	}
	for _, err := range c.errs {
		if strings.Contains(err, "schema.type-vocabulary") {
			continue
		}
		rest := err
		if i := strings.Index(rest, ".yaml"); i >= 0 {
			rest = strings.TrimLeft(rest[i+len(".yaml"):], ".: ")
		}
		if loc != "" && (strings.HasPrefix(rest, loc) || (len(loc) > 12 && strings.Contains(rest, loc))) {
			return true
		}
		// 领域规则常用 service/query 名而不是完整路径；同一文件内同一缺失类型只报一次。
		if file != "" && (strings.Contains(value, ".") || len(value) > 12) &&
			strings.Contains(err, file) && containsTypeToken(err, value) {
			return true
		}
	}
	return false
}

// containsTypeToken 判断 token 是否作为完整类型词出现在 s 中，避免 account 命中 account_status。
func containsTypeToken(s, token string) bool {
	if token == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(s[i:], token)
		if j < 0 {
			return false
		}
		end := i + j + len(token)
		if end >= len(s) || !isTypeTokenChar(s[end]) {
			return true
		}
		i = end
	}
}

func isTypeTokenChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' ||
		c == '_' || c == '.' || c == '<' || c == '>' || c == '*'
}

// checkTypeRef 校验一个类型槽位的值；非字符串与空值交给必填规则。
func (c *Checker) checkTypeRef(v any, path string) {
	s, ok := v.(string)
	if !ok {
		return
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if strings.HasPrefix(s, "*") {
		s = strings.TrimSpace(strings.TrimPrefix(s, "*"))
		if s == "" {
			return
		}
	}
	if strings.HasPrefix(s, "_types.structures.list<") && strings.HasSuffix(s, ">") {
		inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(s, "_types.structures.list<"), ">"))
		c.checkTypeRef(inner, path+".item")
		return
	}
	if strings.HasPrefix(s, "_types.") {
		if class, name, ok := contractspec.SplitTypeRef(s); ok && class == contractspec.TypeClassBasics {
			family, _, hasVariant := splitBasicsName(name)
			if !hasVariant {
				if _, custom := c.valueTypes.CustomTypes[family]; !custom {
					if fam, ok := c.valueTypes.TypeFamilies[family]; ok && len(fam.Types) > 0 {
						// 单变体族的 variant 名就是族名，裸写无歧义；其余族必须写具体 variant。
						if len(fam.Types) > 1 {
							c.rulef("schema.type-concrete", "%s: %s must name a concrete variant (family %s declares %v)", path, s, family, sortedConcreteKeys(fam.Types))
							return
						}
						if _, ok := fam.Types[family]; !ok {
							c.rulef("schema.type-concrete", "%s: %s must name a concrete variant (family %s declares %v)", path, s, family, sortedConcreteKeys(fam.Types))
							return
						}
					}
				}
			}
		}
		if _, err := parseTypeExpr(s, c.valueTypes); err != nil {
			c.typeVocabularyf(path, s, "%s: %v", path, err)
		}
		return
	}
	switch {
	case strings.HasPrefix(s, "data.enums."):
		if !c.enumDeclared(s) {
			c.typeVocabularyf(path, s, "%s: unknown enum %s", path, s)
		}
		return
	case strings.HasPrefix(s, "data.entities."):
		if c.entities != nil && c.entities[s] == nil {
			c.typeVocabularyf(path, s, "%s: unknown entity %s", path, s)
		}
		return
	case strings.HasPrefix(s, "cap.services.") && strings.Contains(s, ".schemas."):
		if c.methodTypeDefs != nil && c.methodTypeDefs[s] == nil {
			c.typeVocabularyf(path, s, "%s: unknown local schema %s", path, s)
		}
		return
	case strings.HasPrefix(s, "data.tables.") && strings.Contains(s, ".col."):
		table, col, ok := splitColumnRef(s)
		if !ok {
			c.typeVocabularyf(path, s, "%s: malformed column ref %s", path, s)
			return
		}
		if c.tableColumns != nil {
			if _, ok := c.tableColumns[table][col]; !ok {
				c.typeVocabularyf(path, s, "%s: unknown column %s", path, s)
			}
		}
		return
	case s == "context.Context", s == "error", strings.HasPrefix(s, "time."):
		return
	}
	c.typeVocabularyf(path, s, "%s: %q is not a _types reference or a known external type", path, s)
}

// scanTypeRefs 递归扫描一个契约文档里的所有 type 键。
func (c *Checker) scanTypeRefs(v any, path string) {
	switch x := v.(type) {
	case map[string]any:
		_, hasKinds := x["types"]
		for k, child := range x {
			if k == "type" && !hasKinds {
				c.checkTypeRef(child, path+".type")
			}
			c.scanTypeRefs(child, path+"."+k)
		}
	case []any:
		for i, item := range x {
			c.scanTypeRefs(item, fmt.Sprintf("%s[%d]", path, i))
		}
	}
}

func sortedConcreteKeys(m map[string]contractspec.ConcreteType) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		a, aerr := strconv.Atoi(out[i])
		b, berr := strconv.Atoi(out[j])
		if aerr == nil && berr == nil {
			return a < b
		}
		return out[i] < out[j]
	})
	return out
}

// knownSchemaKind 是 SchemaNodeKind 支持的节点种类加通配；与 _meta 的 schemas 键一致。
func knownSchemaKind(name string) bool {
	switch name {
	case "object", "array", "map", "scalar", "ref", "*":
		return true
	}
	return false
}

// checkTypeVocabulary 扫描 contracts/ 下所有实例 YAML 的 type 键；只豁免 contracts/generated。
func (c *Checker) checkTypeVocabulary() {
	shared, err := contractspec.LoadSharedTypes(filepath.Join(c.root, "contracts", "_meta"))
	if err != nil {
		c.rulef("schema.type-vocabulary", "load _types.yaml: %v", err)
		return
	}
	custom, err := contractspec.LoadCustomTypes(c.root, shared.TypeFamilies, shared.CommonParams)
	if err != nil {
		c.rulef("schema.type-vocabulary", "load data/types.yaml: %v", err)
		return
	}
	c.valueTypes = ValueTypes{
		CommonParams: shared.CommonParams,
		TypeFamilies: shared.TypeFamilies,
		Structures:   shared.Structures,
		Schemas:      shared.Schemas,
		CustomTypes:  custom.CustomTypes,
	}
	c.nodeKinds = shared.Schemas
	root := filepath.Join(c.root, "contracts")
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		rel := relPath(c.root, path)
		if typeVocabularyExempt(rel) {
			return nil
		}
		doc, err := loadYAML(path)
		if err != nil {
			return nil
		}
		c.scanTypeRefs(doc, rel)
		return nil
	})
}

// typeVocabularyExempt 判断契约路径是否命中显式例外清单。
func typeVocabularyExempt(rel string) bool {
	rel = filepath.ToSlash(rel)
	return strings.HasPrefix(rel, "contracts/generated/")
}

// enum_codes.go 校验表 CHECK 中引用枚举列时的整数码是否存在于 data.enums。
//
// 映射来源：entity 字段 type: data.enums.* + source.column；表 CHECK 只允许用整数码。
package check

import (
	"regexp"
	"strconv"
	"strings"
)

var enumCheckCompareRe = regexp.MustCompile(`(?i)\b([a-z_][a-z0-9_]*)\s*(=|<>|!=|in)\s*(\([^()]*\)|\d+)`)
var enumCheckIntRe = regexp.MustCompile(`\d+`)

// checkEnumCodeLiterals 检查每张表 CHECK 中枚举列的整数码。
func (c *Checker) checkEnumCodeLiterals() {
	if len(c.enumCodes) == 0 {
		return
	}
	enumCols := c.enumColumnMap()
	for tableID, doc := range c.tableDocs {
		cols := enumCols[tableID]
		if len(cols) == 0 {
			continue
		}
		for _, raw := range asList(doc["constraints"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			expr, _ := m["expr"].(string)
			if expr == "" {
				continue
			}
			for _, match := range enumCheckCompareRe.FindAllStringSubmatch(expr, -1) {
				col := strings.ToLower(match[1])
				enumID, ok := cols[col]
				if !ok {
					continue
				}
				codes, ok := c.enumCodes[enumID]
				if !ok {
					continue
				}
				for _, lit := range enumCheckIntRe.FindAllString(match[3], -1) {
					n, err := strconv.Atoi(lit)
					if err != nil {
						continue
					}
					if !codes[n] {
						c.rulef("enum-code-literal", "%s: constraint %s: %s value %d is not a code of %s", tableID, name, col, n, enumID)
					}
				}
			}
		}
	}
}

// enumColumnMap 从 entity 字段建立 table.column -> data.enums.* 映射。
func (c *Checker) enumColumnMap() map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, e := range c.entities {
		for _, f := range e.Fields {
			if f.SourceColumnRef == "" || f.ColumnTable == "" || f.Column == "" {
				continue
			}
			base, _ := splitPointer(f.Type)
			if !strings.HasPrefix(base, "data.enums.") {
				continue
			}
			if out[f.ColumnTable] == nil {
				out[f.ColumnTable] = map[string]string{}
			}
			out[f.ColumnTable][f.Column] = base
		}
	}
	return out
}

// validate.go 通用 schema 校验与正则自检。
package check

import (
	"fmt"
	"regexp"
)

func validatePatterns(fields []Field) error {
	for _, f := range fields {
		if f.Pattern != "" {
			if _, err := regexp.Compile(f.Pattern); err != nil {
				return fmt.Errorf("field %s: invalid pattern %q: %w", f.Name, f.Pattern, err)
			}
		}
		if err := validatePatterns(f.Fields); err != nil {
			return err
		}
		if err := validatePatterns(f.ItemFields); err != nil {
			return err
		}
	}
	return nil
}

var schemaFieldTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "float": true, "decimal": true,
	"boolean": true, "any": true, "object": true,
	"list<string>": true, "list<object>": true, "list<list<string>>": true, "list<any>": true,
}

// validateFieldTypes 校验 _meta 自身的 type 词汇；避免把列类型族的
// float/decimal 误写进 schema 的 Field.type 后，只在某列使用时才暴露。
func validateFieldTypes(fields []Field) error {
	for _, f := range fields {
		if !schemaFieldTypes[f.Type] {
			return fmt.Errorf("field %s: unknown schema type %q", f.Name, f.Type)
		}
		if err := validateFieldTypes(f.Fields); err != nil {
			return err
		}
		if err := validateFieldTypes(f.ItemFields); err != nil {
			return err
		}
	}
	return nil
}

func (c *Checker) validateDoc(doc map[string]any, fields []Field, path string) {
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Name] = true
	}
	for k := range doc {
		if !known[k] {
			c.rulef("schema.unknown-field", "%s: unknown field %q", path, k)
		}
	}
	for _, f := range fields {
		v, ok := doc[f.Name]
		if !ok || v == nil {
			if f.Required {
				c.rulef("schema.required", "%s: missing required field %q", path, f.Name)
			}
			continue
		}
		c.validateValue(v, f, path+"."+f.Name)
	}
}

func (c *Checker) validateValue(v any, f Field, path string) {
	switch f.Type {
	case "string":
		s, ok := v.(string)
		if !ok {
			c.rulef("schema.type", "%s: must be string", path)
			return
		}
		c.checkScalar(s, f, path)
	case "integer":
		n, ok := toInt(v)
		if !ok {
			c.rulef("schema.type", "%s: must be integer", path)
			return
		}
		c.checkConst(v, f, path)
		c.checkNumericEnum(fmt.Sprint(n), f, path)
		c.checkNumericAllowed(fmt.Sprint(n), f, path)
		c.checkIntRange(n, f, path)
	case "number", "float", "decimal":
		n, ok := toFloat(v)
		if !ok {
			c.rulef("schema.type", "%s: must be number", path)
			return
		}
		c.checkConst(v, f, path)
		c.checkNumericEnum(fmt.Sprint(n), f, path)
		c.checkNumericAllowed(fmt.Sprint(n), f, path)
		c.checkFloatRange(n, f, path)
	case "boolean":
		if _, ok := v.(bool); !ok {
			c.rulef("schema.type", "%s: must be boolean", path)
			return
		}
		c.checkConst(v, f, path)
	case "any":
		c.checkConst(v, f, path)
	case "object":
		m, ok := asMap(v)
		if !ok {
			c.rulef("schema.type", "%s: must be object", path)
			return
		}
		c.validateDoc(m, f.Fields, path)
	case "list<string>":
		l, ok := asList2(v)
		if !ok {
			c.rulef("schema.type", "%s: must be list", path)
			return
		}
		c.checkMinItems(l, f, path)
		seen := map[string]bool{}
		for i, item := range l {
			s, ok := item.(string)
			if !ok {
				c.rulef("schema.type", "%s[%d]: must be string", path, i)
				continue
			}
			if f.Unique && seen[s] {
				c.rulef("schema.unique", "%s[%d]: duplicate %q", path, i, s)
			}
			seen[s] = true
			if len(f.Allowed) > 0 && !inList(f.Allowed, s) {
				c.rulef("schema.enum", "%s[%d]: %q not allowed (allowed: %v)", path, i, s, f.Allowed)
			}
			if f.Pattern != "" && !match(f.Pattern, s) {
				c.rulef("schema.pattern", "%s[%d]: %q does not match %s", path, i, s, f.Pattern)
			}
		}
	case "list<object>":
		l, ok := asList2(v)
		if !ok {
			c.rulef("schema.type", "%s: must be list", path)
			return
		}
		c.checkMinItems(l, f, path)
		for i, item := range l {
			m, ok := asMap(item)
			if !ok {
				c.rulef("schema.type", "%s[%d]: must be object", path, i)
				continue
			}
			c.validateDoc(m, f.ItemFields, fmt.Sprintf("%s[%d]", path, i))
		}
	case "list<any>":
		l, ok := asList2(v)
		if !ok {
			c.rulef("schema.type", "%s: must be list", path)
			return
		}
		c.checkMinItems(l, f, path)
	case "list<list<string>>":
		l, ok := asList2(v)
		if !ok {
			c.rulef("schema.type", "%s: must be list", path)
			return
		}
		c.checkMinItems(l, f, path)
		for i, item := range l {
			inner, ok := asList2(item)
			if !ok {
				c.rulef("schema.type", "%s[%d]: must be list", path, i)
				continue
			}
			if len(inner) == 0 {
				c.rulef("schema.non-empty", "%s[%d]: must not be empty", path, i)
			}
			for j, raw := range inner {
				if _, ok := raw.(string); !ok {
					c.rulef("schema.type", "%s[%d][%d]: must be string", path, i, j)
				}
			}
		}
	default:
		c.rulef("schema.type", "%s: unknown schema type %q", path, f.Type)
	}
}

// checkMinItems 校验 list 的最小长度；min_items 只约束列表，不约束字符串。
func (c *Checker) checkMinItems(l []any, f Field, path string) {
	if f.MinItems == nil {
		return
	}
	if n, ok := toInt(f.MinItems); ok && len(l) < n {
		c.rulef("schema.range", "%s: %d items < min_items %d", path, len(l), n)
	}
}

func (c *Checker) checkScalar(s string, f Field, path string) {
	if len(f.Enum) > 0 && !inList(f.Enum, s) {
		c.rulef("schema.enum", "%s: %q not in enum %v", path, s, f.Enum)
	}
	if len(f.Allowed) > 0 && !inList(f.Allowed, s) {
		c.rulef("schema.enum", "%s: %q not allowed (allowed: %v)", path, s, f.Allowed)
	}
	if f.Pattern != "" && !match(f.Pattern, s) {
		c.rulef("schema.pattern", "%s: %q does not match %s", path, s, f.Pattern)
	}
	c.checkConst(s, f, path)
}

func (c *Checker) checkConst(v any, f Field, path string) {
	if f.Const == nil {
		return
	}
	if fmt.Sprint(v) != fmt.Sprint(f.Const) {
		c.rulef("schema.const", "%s: must be %v, got %v", path, f.Const, v)
	}
}

func (c *Checker) checkNumericEnum(s string, f Field, path string) {
	if len(f.Enum) > 0 && !inList(f.Enum, s) {
		c.rulef("schema.enum", "%s: %s not in enum %v", path, s, f.Enum)
	}
}

func (c *Checker) checkNumericAllowed(s string, f Field, path string) {
	if len(f.Allowed) > 0 && !inList(f.Allowed, s) {
		c.rulef("schema.enum", "%s: %s not allowed (allowed: %v)", path, s, f.Allowed)
	}
}

func (c *Checker) checkIntRange(n int, f Field, path string) {
	if f.Min != nil {
		if m, ok := toInt(f.Min); ok && n < m {
			c.rulef("schema.range", "%s: %d < min %d", path, n, m)
		}
	}
	if f.Max != nil {
		if m, ok := toInt(f.Max); ok && n > m {
			c.rulef("schema.range", "%s: %d > max %d", path, n, m)
		}
	}
}

func (c *Checker) checkFloatRange(n float64, f Field, path string) {
	if f.Min != nil {
		if m, ok := toFloat(f.Min); ok && n < m {
			c.rulef("schema.range", "%s: %v < min %v", path, n, m)
		}
	}
	if f.Max != nil {
		if m, ok := toFloat(f.Max); ok && n > m {
			c.rulef("schema.range", "%s: %v > max %v", path, n, m)
		}
	}
}

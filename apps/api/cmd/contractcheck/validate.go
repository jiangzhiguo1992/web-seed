// validate.go 通用 schema 校验与正则自检。
package main

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

func (c *Checker) validateDoc(doc map[string]any, fields []Field, path string) {
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Name] = true
	}
	for k := range doc {
		if !known[k] {
			c.errf("%s: unknown field %q", path, k)
		}
	}
	for _, f := range fields {
		v, ok := doc[f.Name]
		if !ok || v == nil {
			if f.Required {
				c.errf("%s: missing required field %q", path, f.Name)
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
			c.errf("%s: must be string", path)
			return
		}
		c.checkScalar(s, f, path)
	case "integer":
		if _, ok := toInt(v); !ok {
			c.errf("%s: must be integer", path)
			return
		}
		c.checkConst(v, f, path)
	case "number":
		if _, ok := toFloat(v); !ok {
			c.errf("%s: must be number", path)
			return
		}
		c.checkConst(v, f, path)
	case "boolean":
		if _, ok := v.(bool); !ok {
			c.errf("%s: must be boolean", path)
		}
	case "any":
		c.checkConst(v, f, path)
	case "object":
		m, ok := asMap(v)
		if !ok {
			c.errf("%s: must be object", path)
			return
		}
		c.validateDoc(m, f.Fields, path)
	case "list<string>":
		l, ok := asList2(v)
		if !ok {
			c.errf("%s: must be list", path)
			return
		}
		seen := map[string]bool{}
		for i, item := range l {
			s, ok := item.(string)
			if !ok {
				c.errf("%s[%d]: must be string", path, i)
				continue
			}
			if f.Unique && seen[s] {
				c.errf("%s[%d]: duplicate %q", path, i, s)
			}
			seen[s] = true
			if len(f.Allowed) > 0 && !inList(f.Allowed, s) {
				c.errf("%s[%d]: %q not allowed (allowed: %v)", path, i, s, f.Allowed)
			}
			if f.Pattern != "" && !match(f.Pattern, s) {
				c.errf("%s[%d]: %q does not match %s", path, i, s, f.Pattern)
			}
		}
	case "list<object>":
		l, ok := asList2(v)
		if !ok {
			c.errf("%s: must be list", path)
			return
		}
		for i, item := range l {
			m, ok := asMap(item)
			if !ok {
				c.errf("%s[%d]: must be object", path, i)
				continue
			}
			c.validateDoc(m, f.ItemFields, fmt.Sprintf("%s[%d]", path, i))
		}
	default:
		c.errf("%s: unknown schema type %q", path, f.Type)
	}
}

func (c *Checker) checkScalar(s string, f Field, path string) {
	if len(f.Enum) > 0 && !inList(f.Enum, s) {
		c.errf("%s: %q not in enum %v", path, s, f.Enum)
	}
	if f.Pattern != "" && !match(f.Pattern, s) {
		c.errf("%s: %q does not match %s", path, s, f.Pattern)
	}
	c.checkConst(s, f, path)
}

func (c *Checker) checkConst(v any, f Field, path string) {
	if f.Const == nil {
		return
	}
	if fmt.Sprint(v) != fmt.Sprint(f.Const) {
		c.errf("%s: must be %v, got %v", path, f.Const, v)
	}
}

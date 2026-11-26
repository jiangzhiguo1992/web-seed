// validate.go 通用 schema 校验。
package check

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

var schemaFieldTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "float": true, "decimal": true,
	"boolean": true, "any": true, "object": true,
	"list<string>": true, "list<integer>": true, "list<object>": true, "list<list<string>>": true, "list<any>": true,
	"map<string>": true, "map<object>": true, "map<list<object>>": true, "map": true,
}

// validateFieldTypes 校验 _meta 自身的 type 词汇；避免把列类型族的
// float/decimal 误写进 schema 的 Field.type 后，只在某列使用时才暴露。
// types 是 schema 节点种类列表，与单值 type 互斥。
// typeExpr 是解析后的类型表达式：Base 是类型名，Args 是泛型参数。
type typeExpr struct {
	Base string
	Args []typeExpr
}

func containsObject(expr typeExpr) bool {
	if expr.Base == "object" {
		return true
	}
	for _, arg := range expr.Args {
		if containsObject(arg) {
			return true
		}
	}
	return false
}

// lengthBoundBase 报告 min_len/max_len 对哪些标量有效：只约束字符串长度。
func lengthBoundBase(base string) bool {
	return base == "string"
}

// itemBoundBase 报告 min_items/max_items 对哪些容器有效：集合元素数。
func itemBoundBase(base string) bool {
	switch base {
	case "list", "set", "map":
		return true
	}
	return false
}

// numericLeaf 报告约束最终落在数值叶子上：list/set/array 看元素，map 看 value。
func numericLeaf(expr typeExpr) bool {
	switch expr.Base {
	case "integer", "number", "float", "decimal":
		return true
	case "list", "set", "array", "map":
		if len(expr.Args) > 0 {
			return numericLeaf(expr.Args[len(expr.Args)-1])
		}
	}
	return false
}

// stringLeaf 报告约束最终落在字符串叶子上：list/set/array 看元素，map 看 value。
func stringLeaf(expr typeExpr) bool {
	switch expr.Base {
	case "string":
		return true
	case "list", "set", "array", "map":
		if len(expr.Args) > 0 {
			return stringLeaf(expr.Args[len(expr.Args)-1])
		}
	}
	return false
}

// parseTypeExpr 解析 Field.type：必须是 _types.<basics|structures>.<name>，
// list<T>/map<T> 递归解析；struct 不接受泛型参数。
const maxTypeDepth = 8

func defaultValueTypes() ValueTypes {
	fams := map[string]contractspec.ValueTypeFamily{}
	for _, name := range []string{"string", "boolean", "integer", "float", "decimal", "number", "datetime", "timestamp", "json", "bytes", "any"} {
		fams[name] = contractspec.ValueTypeFamily{}
	}
	// 多变体族的 variant 是固定词汇：没有 _types.yaml 时也要能解析具体引用。
	fams["integer"] = contractspec.ValueTypeFamily{Types: map[string]contractspec.ConcreteType{"8": {}, "16": {}, "32": {}, "64": {}}}
	fams["float"] = contractspec.ValueTypeFamily{Types: map[string]contractspec.ConcreteType{"32": {}, "64": {}}}
	fams["timestamp"] = contractspec.ValueTypeFamily{Types: map[string]contractspec.ConcreteType{"time": {}, "long": {}}}
	fams["datetime"] = contractspec.ValueTypeFamily{Types: map[string]contractspec.ConcreteType{"timezone": {}, "time": {}}}
	structs := map[string]contractspec.Structure{}
	for _, name := range []string{"list", "set", "array", "map", "object"} {
		structs[name] = contractspec.Structure{}
	}
	return ValueTypes{TypeFamilies: fams, Structures: structs}
}

func parseTypeExpr(raw string, vt ValueTypes) (typeExpr, error) {
	def := defaultValueTypes()
	merged := ValueTypes{
		TypeFamilies: map[string]contractspec.ValueTypeFamily{},
		Structures:   map[string]contractspec.Structure{},
		CustomTypes:  map[string]contractspec.CustomValueType{},
	}
	for k, v := range def.TypeFamilies {
		merged.TypeFamilies[k] = v
	}
	for k, v := range vt.TypeFamilies {
		merged.TypeFamilies[k] = v
	}
	for k, v := range def.Structures {
		merged.Structures[k] = v
	}
	for k, v := range vt.Structures {
		merged.Structures[k] = v
	}
	for k, v := range vt.CustomTypes {
		merged.CustomTypes[k] = v
	}
	return parseTypeExprDepth(raw, merged, 0)
}

func parseTypeExprDepth(raw string, vt ValueTypes, depth int) (typeExpr, error) {
	if depth > maxTypeDepth {
		return typeExpr{}, fmt.Errorf("type nesting exceeds %d levels", maxTypeDepth)
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return typeExpr{}, fmt.Errorf("empty type")
	}
	class, name, ok := contractspec.SplitTypeRef(s)
	if !ok {
		return typeExpr{}, fmt.Errorf("type %q must be _types.<basics|structures|schemas>.<name>", raw)
	}
	if i := strings.Index(name, "<"); i >= 0 {
		if class != contractspec.TypeClassStructures {
			return typeExpr{}, fmt.Errorf("type %q: only _types.structures.* accepts type arguments", raw)
		}
		if !strings.HasSuffix(name, ">") {
			return typeExpr{}, fmt.Errorf("malformed type %q", raw)
		}
		base := name[:i]
		if _, ok := vt.Structures[base]; !ok {
			return typeExpr{}, fmt.Errorf("unknown structure %q", base)
		}
		if base == "object" {
			return typeExpr{}, fmt.Errorf("object must not take type arguments")
		}
		args, err := splitTypeArgs(name[i+1:len(name)-1], vt, depth+1)
		if err != nil {
			return typeExpr{}, err
		}
		if base == "map" {
			if len(args) == 1 {
				args = append([]typeExpr{{Base: "string"}}, args...)
			}
			if len(args) != 2 {
				return typeExpr{}, fmt.Errorf("map requires one value type or key,value types")
			}
			if args[0].Base != "string" && args[0].Base != "integer" {
				return typeExpr{}, fmt.Errorf("map key must be string or integer, got %q", args[0].Base)
			}
			return typeExpr{Base: base, Args: args}, nil
		}
		if len(args) != 1 {
			return typeExpr{}, fmt.Errorf("%s requires exactly one type argument", base)
		}
		return typeExpr{Base: base, Args: args}, nil
	}
	switch class {
	case contractspec.TypeClassBasics:
		family, variant, hasVariant := splitBasicsName(name)
		if fam, ok := vt.TypeFamilies[family]; ok {
			if len(fam.Types) == 0 && !hasVariant {
				return typeExpr{Base: family}, nil
			}
			if hasVariant {
				if _, ok := fam.Types[variant]; !ok {
					return typeExpr{}, fmt.Errorf("unknown basics variant %q for family %s", variant, family)
				}
			}
			return typeExpr{Base: family}, nil
		}
		if !hasVariant {
			if _, ok := vt.CustomTypes[name]; ok {
				return typeExpr{Base: name}, nil
			}
		}
		return typeExpr{}, fmt.Errorf("unknown basics type %q", raw)
	case contractspec.TypeClassStructures:
		if _, ok := vt.Structures[name]; ok {
			if name == "list" || name == "map" {
				return typeExpr{}, fmt.Errorf("%s requires a type argument", name)
			}
			return typeExpr{Base: name}, nil
		}
		return typeExpr{}, fmt.Errorf("unknown structure %q", raw)
	default:
		return typeExpr{}, fmt.Errorf("type %q is a schema node discriminator, not a field type", raw)
	}
}

// splitBasicsName 把 basics 名称拆成 family 与 variant；自定义类型没有 variant。
func splitBasicsName(name string) (family, variant string, has bool) {
	if i := strings.IndexByte(name, '.'); i >= 0 {
		return name[:i], name[i+1:], true
	}
	return name, "", false
}

func splitTypeArgs(inner string, vt ValueTypes, depth int) ([]typeExpr, error) {
	var out []typeExpr
	bracket, start := 0, 0
	for i, r := range inner {
		switch r {
		case '<':
			bracket++
		case '>':
			bracket--
		case ',':
			if bracket == 0 {
				part := strings.TrimSpace(inner[start:i])
				if part == "" {
					return nil, fmt.Errorf("empty type argument")
				}
				arg, err := parseTypeExprDepth(part, vt, depth)
				if err != nil {
					return nil, err
				}
				out = append(out, arg)
				start = i + 1
			}
		}
	}
	part := strings.TrimSpace(inner[start:])
	if part == "" {
		return nil, fmt.Errorf("empty type argument")
	}
	arg, err := parseTypeExprDepth(part, vt, depth)
	if err != nil {
		return nil, err
	}
	return append(out, arg), nil
}

// normalizeTypeExpr 去掉全部 _types 命名空间前缀，返回裸类型表达式；
// 供 validateValue 的既有分支按 "list<object>" 这类裸名匹配。
func normalizeTypeExpr(s string) string {
	return contractspec.BareTypeName(s)
}

func validateFieldTypes(fields []Field, vt ValueTypes, bootstrap bool) error {
	for _, f := range fields {
		typ := normalizeTypeExpr(f.Type)
		if len(f.Types) > 0 {
			if f.Type != "" && typ != "map" {
				return fmt.Errorf("field %s: type and types are mutually exclusive", f.Name)
			}
			if f.Const != nil || len(f.Enum) > 0 || f.Pattern != "" || f.Min != nil || f.Max != nil ||
				f.MinItems != nil || f.MaxItems != nil || f.MinLen != nil || f.MaxLen != nil || f.Len != nil ||
				f.Unique || f.UniqueKey != "" || f.Soft {
				return fmt.Errorf("field %s: types only accepts required/description/default/key_patterns", f.Name)
			}
			seen := map[string]bool{}
			for _, kind := range f.Types {
				if !schemaKindAllowed(kind, vt.Schemas) {
					return fmt.Errorf("field %s: unknown schema kind %q", f.Name, kind)
				}
				if seen[kind] {
					return fmt.Errorf("field %s: duplicate schema kind %q", f.Name, kind)
				}
				seen[kind] = true
			}
		} else if f.Types != nil {
			return fmt.Errorf("field %s: types must not be empty", f.Name)
		} else if typ == "map" {
			return fmt.Errorf("field %s: map requires types", f.Name)
		} else if f.Type != "" {
			expr, err := parseTypeExpr(f.Type, vt)
			if err != nil {
				if !bootstrap || !schemaFieldTypes[typ] {
					return fmt.Errorf("field %s: %w", f.Name, err)
				}
			} else {
				switch expr.Base {
				case "array":
					if f.Len == nil {
						return fmt.Errorf("field %s: array requires len", f.Name)
					}
					if n, ok := toInt(f.Len); !ok || n < 1 {
						return fmt.Errorf("field %s: len must be a positive integer", f.Name)
					}
					if f.MinLen != nil || f.MaxLen != nil || f.MinItems != nil || f.MaxItems != nil {
						return fmt.Errorf("field %s: array uses len, not min_len/max_len/min_items/max_items", f.Name)
					}
				case "set":
					if strings.HasPrefix(normalizeTypeExpr(f.Type), "set<object>") && f.UniqueKey == "" {
						return fmt.Errorf("field %s: set<object> requires unique_key", f.Name)
					}
					if strings.HasPrefix(normalizeTypeExpr(f.Type), "set<object>") && f.UniqueKey != "" {
						for _, child := range f.ItemFields {
							if child.Name == f.UniqueKey && normalizeTypeExpr(child.Type) == "boolean" {
								return fmt.Errorf("field %s: boolean unique_key is not allowed", f.Name)
							}
						}
					}
				}
				if f.Len != nil && expr.Base != "array" {
					return fmt.Errorf("field %s: len is only allowed for array", f.Name)
				}
				if (f.MinLen != nil || f.MaxLen != nil) && !lengthBoundBase(expr.Base) {
					return fmt.Errorf("field %s: min_len/max_len are only allowed for string", f.Name)
				}
				if (f.MinItems != nil || f.MaxItems != nil) && !itemBoundBase(expr.Base) {
					return fmt.Errorf("field %s: min_items/max_items are only allowed for list/set/map", f.Name)
				}
				if n, ok := toInt(f.MinItems); f.MinItems != nil && (!ok || n < 0) {
					return fmt.Errorf("field %s: min_items must be a non-negative integer", f.Name)
				}
				if n, ok := toInt(f.MaxItems); f.MaxItems != nil && (!ok || n < 0) {
					return fmt.Errorf("field %s: max_items must be a non-negative integer", f.Name)
				}
				if f.MinItems != nil && f.MaxItems != nil {
					minN, ok1 := toInt(f.MinItems)
					maxN, ok2 := toInt(f.MaxItems)
					if ok1 && ok2 && minN > maxN {
						return fmt.Errorf("field %s: min_items must not exceed max_items", f.Name)
					}
				}
				if (f.Min != nil || f.Max != nil) && !numericLeaf(expr) {
					return fmt.Errorf("field %s: min/max are only allowed for numeric types", f.Name)
				}
				if f.Pattern != "" && !stringLeaf(expr) {
					return fmt.Errorf("field %s: pattern is only allowed for string types", f.Name)
				}
				if f.Min != nil {
					if _, ok := toFloat(f.Min); !ok {
						return fmt.Errorf("field %s: min must be a number", f.Name)
					}
				}
				if f.Max != nil {
					if _, ok := toFloat(f.Max); !ok {
						return fmt.Errorf("field %s: max must be a number", f.Name)
					}
				}
				if f.Min != nil && f.Max != nil {
					minN, ok1 := toFloat(f.Min)
					maxN, ok2 := toFloat(f.Max)
					if ok1 && ok2 && minN > maxN {
						return fmt.Errorf("field %s: min must not exceed max", f.Name)
					}
				}
				if len(expr.Args) == 1 && (expr.Base == "list" || expr.Base == "set" || expr.Base == "array") {
					if containsObject(expr.Args[0]) && len(f.ItemFields) == 0 {
						return fmt.Errorf("field %s: %s requires item_fields for object elements", f.Name, expr.Base)
					}
					if !containsObject(expr.Args[0]) && len(f.ItemFields) > 0 {
						return fmt.Errorf("field %s: item_fields only allowed when element is object", f.Name)
					}
				}
				if expr.Base == "map" && len(expr.Args) == 2 && expr.Args[0].Base == "integer" && len(f.KeyPatterns) > 0 {
					return fmt.Errorf("field %s: key_patterns only allowed for string map keys", f.Name)
				}
				if f.MinLen != nil {
					if n, ok := toInt(f.MinLen); !ok || n < 0 {
						return fmt.Errorf("field %s: min_len must be a non-negative integer", f.Name)
					}
				}
				if f.MaxLen != nil {
					if n, ok := toInt(f.MaxLen); !ok || n < 1 {
						return fmt.Errorf("field %s: max_len must be a positive integer", f.Name)
					}
				}
				if f.MinLen != nil && f.MaxLen != nil {
					minN, ok1 := toInt(f.MinLen)
					maxN, ok2 := toInt(f.MaxLen)
					if ok1 && ok2 && minN > maxN {
						return fmt.Errorf("field %s: min_len must not exceed max_len", f.Name)
					}
				}
				if err := validateFieldConstraints(f, expr, vt); err != nil {
					return err
				}
			}
		}
		if f.Default != nil && len(f.Enum) > 0 && !inList(f.Enum, fmt.Sprint(f.Default)) {
			return fmt.Errorf("field %s: default %v not in enum %v", f.Name, f.Default, f.Enum)
		}
		if f.UniqueKey != "" {
			if typ != "list<object>" && typ != "set<object>" {
				return fmt.Errorf("field %s: unique_key requires list<object> or set<object>", f.Name)
			}
			found := false
			for _, child := range f.ItemFields {
				if child.Name == f.UniqueKey {
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("field %s: unique_key %s not found in item_fields", f.Name, f.UniqueKey)
			}
		}
		if (typ == "map<object>" || typ == "map<list<object>>" || typ == "list<map<object>>") && len(f.ItemFields) == 0 {
			return fmt.Errorf("field %s: %s requires item_fields", f.Name, f.Type)
		}
		if len(f.KeyPatterns) > 0 && !strings.Contains(typ, "map") {
			return fmt.Errorf("field %s: key_patterns requires map", f.Name)
		}
		if f.Soft && typ != "any" && typ != "string" {
			return fmt.Errorf("field %s: soft requires type: any or string", f.Name)
		}
		if len(f.Types) > 1 && f.Default != nil {
			return fmt.Errorf("field %s: types with multiple kinds must not declare default", f.Name)
		}
		if err := validateFieldTypes(f.Fields, vt, bootstrap); err != nil {
			return err
		}
		if err := validateFieldTypes(f.ItemFields, vt, bootstrap); err != nil {
			return err
		}
	}
	return nil
}

// validateFieldConstraints 校验字段规范里可判定的值约束，避免 enum/const/default/examples
// 只在实例上生效、却在模板里写入与类型不兼容的值。
func validateFieldConstraints(f Field, expr typeExpr, vt ValueTypes) error {
	leaf := leafTypeExpr(expr)
	if f.Unique {
		switch expr.Base {
		case "list", "set", "array":
		default:
			return fmt.Errorf("field %s: unique is only allowed for list/set/array", f.Name)
		}
		if containsObject(expr) && f.UniqueKey == "" {
			return fmt.Errorf("field %s: unique list/set of objects requires unique_key", f.Name)
		}
	}
	if f.UniqueKey != "" {
		if child, ok := contractspec.FindField(f.ItemFields, f.UniqueKey); ok {
			childExpr, err := parseTypeExpr(child.Type, vt)
			if err != nil {
				return fmt.Errorf("field %s: unique_key %s: %w", f.Name, f.UniqueKey, err)
			}
			if childExpr.Base != "string" {
				return fmt.Errorf("field %s: unique_key %s must be a string field", f.Name, f.UniqueKey)
			}
		}
	}
	if len(f.Fields) > 0 && expr.Base != "object" {
		return fmt.Errorf("field %s: fields is only allowed for object", f.Name)
	}
	enumSeen := map[string]bool{}
	for i, value := range f.Enum {
		if !valueCompatible(leaf, value, vt) {
			return fmt.Errorf("field %s: enum[%d] %v is not compatible with %s", f.Name, i, value, f.Type)
		}
		key := fmt.Sprint(value)
		if enumSeen[key] {
			return fmt.Errorf("field %s: enum[%d] %v is duplicated", f.Name, i, value)
		}
		enumSeen[key] = true
	}
	for i, value := range f.Examples {
		if !valueCompatible(leaf, value, vt) {
			return fmt.Errorf("field %s: examples[%d] %v is not compatible with %s", f.Name, i, value, f.Type)
		}
	}
	if f.Const != nil {
		if !valueCompatible(leaf, f.Const, vt) {
			return fmt.Errorf("field %s: const %v is not compatible with %s", f.Name, f.Const, f.Type)
		}
		if len(f.Enum) > 0 && !inList(f.Enum, fmt.Sprint(f.Const)) {
			return fmt.Errorf("field %s: const %v not in enum %v", f.Name, f.Const, f.Enum)
		}
	}
	if f.Default != nil {
		if !valueCompatible(leaf, f.Default, vt) {
			return fmt.Errorf("field %s: default %v is not compatible with %s", f.Name, f.Default, f.Type)
		}
		if f.Const != nil && fmt.Sprint(f.Const) != fmt.Sprint(f.Default) {
			return fmt.Errorf("field %s: const %v and default %v conflict", f.Name, f.Const, f.Default)
		}
	}
	if leaf.Base == "integer" {
		if f.Min != nil {
			if _, ok := toInt(f.Min); !ok {
				return fmt.Errorf("field %s: min must be an integer for integer type", f.Name)
			}
		}
		if f.Max != nil {
			if _, ok := toInt(f.Max); !ok {
				return fmt.Errorf("field %s: max must be an integer for integer type", f.Name)
			}
		}
	}
	checkValue := func(value any) error {
		switch leaf.Base {
		case "string":
			s, ok := value.(string)
			if !ok {
				return nil
			}
			if f.Pattern != "" && !match(f.Pattern, s) {
				return fmt.Errorf("field %s: value %q does not match pattern %s", f.Name, s, f.Pattern)
			}
			n := utf8.RuneCountInString(s)
			if min, ok := toInt(f.MinLen); ok && n < min {
				return fmt.Errorf("field %s: value %q length %d < min_len %d", f.Name, s, n, min)
			}
			if max, ok := toInt(f.MaxLen); ok && n > max {
				return fmt.Errorf("field %s: value %q length %d > max_len %d", f.Name, s, n, max)
			}
		case "integer", "number", "float", "decimal":
			n, ok := toFloat(value)
			if !ok {
				return nil
			}
			if min, ok := toFloat(f.Min); ok && n < min {
				return fmt.Errorf("field %s: value %v < min %v", f.Name, value, min)
			}
			if max, ok := toFloat(f.Max); ok && n > max {
				return fmt.Errorf("field %s: value %v > max %v", f.Name, value, max)
			}
		}
		return nil
	}
	for _, value := range f.Enum {
		if err := checkValue(value); err != nil {
			return err
		}
	}
	for _, value := range f.Examples {
		if err := checkValue(value); err != nil {
			return err
		}
	}
	if f.Const != nil {
		if err := checkValue(f.Const); err != nil {
			return err
		}
	}
	if f.Default != nil {
		if err := checkValue(f.Default); err != nil {
			return err
		}
	}
	return nil
}

// leafTypeExpr 返回集合约束实际作用的值类型：list/set/array 取元素，map 取 value。
func leafTypeExpr(expr typeExpr) typeExpr {
	switch expr.Base {
	case "list", "set", "array":
		if len(expr.Args) == 1 {
			return expr.Args[0]
		}
	case "map":
		if len(expr.Args) == 2 {
			return expr.Args[1]
		}
	}
	return expr
}

// valueCompatible 只校验能确定落型的标量；时间/JSON/字节等序列化形态由生成器负责，
// 这里不引入第二套类型规则。
func valueCompatible(expr typeExpr, v any, vt ValueTypes) bool {
	switch expr.Base {
	case "string":
		_, ok := v.(string)
		return ok
	case "integer":
		_, ok := toInt(v)
		return ok
	case "number", "float", "decimal":
		_, ok := toFloat(v)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "any":
		switch v.(type) {
		case string, bool, int, int64, uint64, float64:
			return true
		}
		return false
	}
	if custom, ok := vt.CustomTypes[expr.Base]; ok {
		if family, _, ok := custom.Basics(); ok {
			return valueCompatible(typeExpr{Base: family}, v, vt)
		}
	}
	return true
}

// 生成字段必须标记为不可手写，write 只允许 forbidden。
// validateMetaFields 校验字段规范自身：同层 name 唯一且非空，description 非空。
// Unordered 是运行时注入的类型族参数，不在 _meta 中手写。
func validateMetaFields(fields []Field, where string) error {
	seen := map[string]bool{}
	for _, f := range fields {
		if f.Unordered {
			continue
		}
		if strings.TrimSpace(f.Name) == "" {
			return fmt.Errorf("%s: field name must not be empty", where)
		}
		if seen[f.Name] {
			return fmt.Errorf("%s: duplicate field %q", where, f.Name)
		}
		seen[f.Name] = true
		if strings.TrimSpace(f.Description) == "" {
			return fmt.Errorf("%s.%s: description must not be empty", where, f.Name)
		}
		if err := validateMetaFields(f.Fields, where+"."+f.Name+".fields"); err != nil {
			return err
		}
		if err := validateMetaFields(f.ItemFields, where+"."+f.Name+".item_fields"); err != nil {
			return err
		}
	}
	return nil
}

func validateFieldMarkers(fields []Field) error {
	for _, f := range fields {
		if f.Write != "" && f.Write != "forbidden" {
			return fmt.Errorf("field %s: write must be forbidden, got %q", f.Name, f.Write)
		}
		if f.Generated && f.Write != "forbidden" {
			return fmt.Errorf("field %s: generated fields must set write: forbidden", f.Name)
		}
		if f.Write == "forbidden" && !f.Generated {
			return fmt.Errorf("field %s: write: forbidden requires generated: true", f.Name)
		}
		if err := validateFieldMarkers(f.Fields); err != nil {
			return err
		}
		if err := validateFieldMarkers(f.ItemFields); err != nil {
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
	if len(f.Types) > 0 {
		if normalizeTypeExpr(f.Type) == "map" {
			m, ok := asMap(v)
			if !ok {
				c.rulef("schema.type", "%s: must be map", path)
				return
			}
			for k, item := range m {
				if k == "" {
					c.rulef("schema.non-empty", "%s: map key must not be empty", path)
					continue
				}
				if !matchKeyPatterns(k, f.KeyPatterns) {
					c.rulef("schema.pattern", "%s.%s: map key %q does not match %s", path, k, k, strings.Join(f.KeyPatterns, " | "))
				}
				c.validateSchemaNodeWith(item, path+"."+k, f.Types)
			}
			return
		}
		c.validateSchemaNodeWith(v, path, f.Types)
		return
	}
	expr, err := parseTypeExpr(f.Type, c.valueTypes)
	if err != nil {
		c.rulef("schema.type", "%s: %v", path, err)
		return
	}
	c.validateExpr(v, f, expr, path)
}

func matchKeyPatterns(k string, patterns []string) bool {
	for _, pat := range patterns {
		if !match(pat, k) {
			return false
		}
	}
	return true
}

func (c *Checker) validateExpr(v any, f Field, expr typeExpr, path string) {
	switch expr.Base {
	case "string":
		s, ok := v.(string)
		if !ok {
			c.rulef("schema.type", "%s: must be string", path)
			return
		}
		if f.Required && strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: must not be empty", path)
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
		c.checkIntRange(n, f, path)
	case "number", "float", "decimal":
		n, ok := toFloat(v)
		if !ok {
			c.rulef("schema.type", "%s: must be number", path)
			return
		}
		c.checkConst(v, f, path)
		c.checkNumericEnum(fmt.Sprint(n), f, path)
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
		c.validateObject(v, f, path)
	case "list", "set", "array":
		c.validateSequence(v, f, expr, path)
	case "map":
		c.validateMap(v, f, expr, path)
	default:
		c.rulef("schema.type", "%s: unsupported type %q", path, expr.Base)
	}
}

func (c *Checker) validateObject(v any, f Field, path string) {
	m, ok := asMap(v)
	if !ok {
		c.rulef("schema.type", "%s: must be object", path)
		return
	}
	fields := f.Fields
	if len(fields) == 0 {
		fields = f.ItemFields
	}
	c.validateDoc(m, fields, path)
}

func (c *Checker) validateSequence(v any, f Field, expr typeExpr, path string) {
	l, ok := asList2(v)
	if !ok {
		c.rulef("schema.type", "%s: must be list", path)
		return
	}
	c.checkSeqBounds(l, f, path, expr.Base == "set", expr.Base == "array")
	if len(expr.Args) != 1 {
		c.rulef("schema.type", "%s: %s requires one element type", path, expr.Base)
		return
	}
	seen := map[string]bool{}
	for i, item := range l {
		elemPath := fmt.Sprintf("%s[%d]", path, i)
		if expr.Base == "set" || f.Unique || f.UniqueKey != "" {
			if key, ok := sequenceKey(item, f); ok {
				if seen[key] {
					c.rulef("schema.unique", "%s[%d]: duplicate %q", path, i, key)
				}
				seen[key] = true
			}
		}
		c.validateExpr(item, f, expr.Args[0], elemPath)
	}
}

func sequenceKey(item any, f Field) (string, bool) {
	if m, ok := asMap(item); ok {
		if f.UniqueKey == "" {
			return "", false
		}
		v, _ := m[f.UniqueKey].(string)
		return v, v != ""
	}
	return fmt.Sprint(item), true
}

func (c *Checker) validateMap(v any, f Field, expr typeExpr, path string) {
	m, ok := asMap(v)
	if !ok {
		c.rulef("schema.type", "%s: must be map", path)
		return
	}
	c.checkMapBounds(m, f, path)
	if len(expr.Args) != 2 {
		c.rulef("schema.type", "%s: map requires key and value types", path)
		return
	}
	keyExpr, valExpr := expr.Args[0], expr.Args[1]
	for k, item := range m {
		if k == "" {
			c.rulef("schema.non-empty", "%s: map key must not be empty", path)
			continue
		}
		switch keyExpr.Base {
		case "string":
			if !matchKeyPatterns(k, f.KeyPatterns) {
				c.rulef("schema.pattern", "%s.%s: map key %q does not match %s", path, k, k, strings.Join(f.KeyPatterns, " | "))
			}
		case "integer":
			n, ok := toInt(k)
			if !ok {
				c.rulef("schema.type", "%s.%s: map key must be integer", path, k)
				continue
			}
			c.checkIntRange(n, f, path+"."+k)
		default:
			c.rulef("schema.type", "%s: unsupported map key type %q", path, keyExpr.Base)
		}
		c.validateExpr(item, f, valExpr, path+"."+k)
	}
}

func (c *Checker) checkMapBounds(m map[string]any, f Field, path string) {
	c.checkItemBounds(len(m), f, path)
}

func (c *Checker) checkSeqBounds(l []any, f Field, path string, isSet, isArray bool) {
	if isArray {
		if f.Len == nil {
			c.rulef("schema.len", "%s: array requires len", path)
			return
		}
		if n, ok := toInt(f.Len); ok && len(l) != n {
			c.rulef("schema.len", "%s: length %d must equal %d", path, len(l), n)
		}
		return
	}
	c.checkItemBounds(len(l), f, path)
}

// checkItemBounds 校验集合元素数：min_items/max_items 只约束 list/set/map。
// generated 字段用独立 rule-id，让 gen-graph 在写回前跳过这类已知漂移。
func (c *Checker) checkItemBounds(n int, f Field, path string) {
	if f.MinItems != nil {
		if min, ok := toInt(f.MinItems); ok && n < min {
			if f.Generated {
				c.rulef("schema.generated-range", "%s: %d items < min_items %d", path, n, min)
			} else {
				c.rulef("schema.range", "%s: %d items < min_items %d", path, n, min)
			}
		}
	}
	if f.MaxItems != nil {
		if max, ok := toInt(f.MaxItems); ok && n > max {
			c.rulef("schema.range", "%s: %d items > max_items %d", path, n, max)
		}
	}
}

func (c *Checker) checkScalar(s string, f Field, path string) {
	if len(f.Enum) > 0 && !inList(f.Enum, s) {
		c.rulef("schema.enum", "%s: %q not in enum %v", path, s, f.Enum)
	}
	if f.MinLen != nil || f.MaxLen != nil {
		n := utf8.RuneCountInString(s)
		if min, ok := toInt(f.MinLen); ok && n < min {
			c.rulef("schema.len", "%s: length %d must be >= %d", path, n, min)
		}
		if max, ok := toInt(f.MaxLen); ok && n > max {
			c.rulef("schema.len", "%s: length %d must be <= %d", path, n, max)
		}
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

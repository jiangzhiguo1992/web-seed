// http_types.go 把 HTTP 契约里的 _types.basics 引用解析成 OpenAPI 标量 schema。
package check

import (
	"fmt"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

// httpTypeResolver 供 NormalizeHTTPDocument 解析 _types.basics.<name>。
func (c *Checker) httpTypeResolver(name string) (map[string]any, error) {
	family, _, ok := contractspec.SplitBasicsRef(name)
	if !ok {
		return nil, fmt.Errorf("unknown type %s", name)
	}
	params := map[string]any{}
	if custom, ok := c.valueTypes.CustomTypes[family]; ok {
		family, _, _ = custom.Basics()
		params = custom.Params
	} else if _, ok := c.valueTypes.TypeFamilies[family]; !ok {
		return nil, fmt.Errorf("unknown type %s", name)
	}
	out := map[string]any{}
	switch family {
	case "string":
		out["type"] = "string"
	case "integer":
		out["type"] = "integer"
	case "boolean":
		out["type"] = "boolean"
	case "timestamp", "datetime":
		out["type"] = "string"
		out["format"] = "date-time"
	case "float", "decimal":
		out["type"] = "number"
	case "json":
		out["type"] = "object"
	case "bytes":
		out["type"] = "string"
		out["format"] = "byte"
	case "any":
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported type family %s for %s", family, name)
	}
	for key, value := range params {
		switch key {
		case "min_len":
			out["minLength"] = value
		case "max_len":
			out["maxLength"] = value
		case "min":
			out["minimum"] = value
		case "max":
			out["maximum"] = value
		case "pattern":
			out["pattern"] = value
		case "default":
			out["default"] = value
		}
	}
	return out, nil
}

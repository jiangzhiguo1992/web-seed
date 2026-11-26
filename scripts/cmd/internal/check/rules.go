// rules.go 校验 feature.flows 文档与规则项。
package check

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/rulesdsl"
)

func (c *Checker) checkRuleDoc(doc map[string]any, rel string, schema *Schema, serviceIDs map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkStability(doc, rel)
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("flow-refs", "%s: id %q must be %q", rel, fileID, want)
	}
	seen := map[string]bool{}
	for _, raw := range asList(doc["rules"]) {
		item, _ := asMap(raw)
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		if seen[id] {
			c.rulef("flow-refs", "%s: duplicate rule id %s", rel, id)
		}
		seen[id] = true
		if !strings.HasPrefix(id, fileID+".") {
			c.rulef("flow-refs", "%s: rule id %s must start with %s.", rel, id, fileID)
		}
		kind, _ := item["kind"].(string)
		var fieldInfo *entityFieldInfo
		reject := func(fields ...string) {
			for _, field := range fields {
				if item[field] != nil {
					c.rulef("flow-rule-structure", "%s: %s %s must not declare %s", rel, kind, id, field)
				}
			}
		}
		switch kind {
		case "constraint":
			entityID, _ := item["entity"].(string)
			field, _ := item["field"].(string)
			if entityID == "" {
				c.rulef("flow-rule-structure", "%s: constraint %s requires entity", rel, id)
			} else if ent := c.entities[entityID]; ent == nil {
				c.rulef("flow-rule-structure", "%s: constraint %s unknown entity %s", rel, id, entityID)
			} else if field != "" {
				f := findEntityFieldByJSONName(ent, field)
				if f == nil {
					f = findEntityField(ent, field)
				}
				if f == nil && !isBaseColumn(field) {
					c.rulef("flow-rule-structure", "%s: constraint %s field %s not found on %s", rel, id, field, entityID)
				} else if f != nil && f.IsRelation {
					c.rulef("flow-rule-structure", "%s: constraint %s field %s must be scalar", rel, id, field)
				}
				fieldInfo = f
				if fieldInfo == nil {
					// 基类列不来自具体实体字段，仍需给出类型族以校验 op/value。
					if family := baseColumnFamily(field); family != "" {
						fieldInfo = &entityFieldInfo{Type: family}
					}
				}
			}
			if field == "" || item["op"] == nil || item["value"] == nil {
				c.rulef("flow-rule-structure", "%s: constraint %s requires field/op/value", rel, id)
			} else {
				c.checkRuleValue(rel, id, fieldInfo, item["op"], item["value"])
			}
			reject("assert", "decision", "given", "when", "then")
		case "invariant":
			if s, _ := item["assert"].(string); strings.TrimSpace(s) == "" {
				c.rulef("flow-rule-structure", "%s: invariant %s requires assert", rel, id)
			} else if err := rulesdsl.Validate(s); err != nil {
				c.rulef("flow-rule-dsl", "%s: invariant %s assert is not executable: %v", rel, id, err)
			}
			reject("entity", "field", "op", "value", "decision", "given", "when", "then")
		case "policy":
			reject("entity", "field", "op", "value", "assert", "given", "when", "then")
			decisions := asList(item["decision"])
			if len(decisions) == 0 {
				c.rulef("policy-decision", "%s: policy %s requires non-empty decision", rel, id)
			}
			for i, rawDecision := range decisions {
				decision, _ := asMap(rawDecision)
				where := fmt.Sprintf("%s.decision[%d]", id, i)
				when, _ := decision["when"].(string)
				if strings.TrimSpace(when) == "" {
					c.rulef("policy-decision", "%s: %s requires when", rel, where)
				} else if err := rulesdsl.Validate(when); err != nil {
					c.rulef("flow-rule-dsl", "%s: %s when is not executable: %v", rel, where, err)
				}
				effect, _ := decision["effect"].(string)
				errorCode, _ := decision["error"].(string)
				target, _ := decision["target"].(string)
				value := decision["value"]
				switch effect {
				case "deny":
					if errorCode == "" {
						c.rulef("policy-decision", "%s: %s deny requires error", rel, where)
					} else if !ruleDeclaresError(item, errorCode) {
						c.rulef("policy-decision", "%s: %s error %s is not declared in rule.errors", rel, where, errorCode)
					}
				case "call":
					if target == "" || !serviceIDs[target] {
						c.rulef("policy-decision", "%s: %s call target %q is not a known cap.services.*", rel, where, target)
					}
				case "set", "normalize", "record", "create", "revoke":
					if target == "" {
						c.rulef("policy-decision", "%s: %s %s requires target", rel, where, effect)
					}
				case "forbid":
					if target == "" {
						c.rulef("policy-decision", "%s: %s forbid requires target", rel, where)
					}
					op, _ := value.(string)
					if op != "update" && op != "delete" {
						c.rulef("policy-decision", "%s: %s forbid value must be update or delete", rel, where)
					}
				case "allow":
					if errorCode != "" || target != "" || value != nil {
						c.rulef("policy-decision", "%s: %s allow must not declare error/target/value", rel, where)
					}
				default:
					c.rulef("policy-decision", "%s: %s effect must be deny/allow/call/set/normalize/record/create/revoke/forbid", rel, where)
				}
				if onFailure, _ := decision["on_failure"].(string); onFailure != "" && onFailure != "rollback" && onFailure != "ignore" {
					c.rulef("policy-decision", "%s: %s on_failure must be rollback or ignore", rel, where)
				}
			}
		default:
			c.rulef("flow-rule-structure", "%s: rule %s kind must be constraint/invariant/policy", rel, id)
		}
		for _, rawErr := range asList(item["errors"]) {
			code, _ := rawErr.(string)
			if code == "" {
				continue
			}
			if !c.errorCodeDeclared(code) {
				c.rulef("flow-rule-structure", "%s: rule %s unknown error %s", rel, id, code)
			}
		}
		implemented := asList(item["implemented_by"])
		for _, rawSvc := range implemented {
			svc, _ := rawSvc.(string)
			if !serviceIDs[svc] {
				c.rulef("flow-rule-structure", "%s: rule %s unknown service %s", rel, id, svc)
			}
		}
	}
}

// checkRuleValue 校验 constraint 的 op/value 组合是否可判定。
func (c *Checker) checkRuleValue(rel, id string, f *entityFieldInfo, opValue, value any) {
	op, _ := opValue.(string)
	fail := func(format string, args ...any) {
		c.rulef("flow-rule-value", "%s: rule %s "+format, append([]any{rel, id}, args...)...)
	}
	family := c.entityFieldFamily(f)
	switch op {
	case "pattern":
		pattern, ok := value.(string)
		if !ok || strings.TrimSpace(pattern) == "" {
			fail("pattern value must be a non-empty string")
			return
		}
		if _, err := regexp.Compile(pattern); err != nil {
			fail("pattern value %q is not a valid regex: %v", pattern, err)
		}
		if family != "" && family != "string" {
			fail("pattern requires a string field")
		}
	case "in", "not_in":
		items, ok := asList2(value)
		if !ok || len(items) == 0 {
			fail("%s value must be a non-empty list", op)
			return
		}
		for _, item := range items {
			if !isScalarValue(item) {
				fail("%s value items must be scalars", op)
				return
			}
		}
	case "min_len", "max_len":
		n, ok := toInt(value)
		if !ok || n < 0 {
			fail("%s value must be a non-negative integer", op)
		}
		if family != "" && family != "string" {
			fail("%s requires a string field", op)
		}
	case "required", "not_null":
		if _, ok := value.(bool); !ok {
			fail("%s value must be boolean", op)
		}
	case "eq", "ne":
		if !isScalarValue(value) {
			fail("%s value must be a scalar", op)
		}
	case "gt", "ge", "lt", "le":
		if _, ok := toFloat(value); !ok {
			fail("%s value must be a number", op)
		}
		switch family {
		case "", "integer", "float", "decimal", "timestamp", "datetime":
		default:
			fail("%s requires a numeric or time field", op)
		}
	}
}

// baseColumnFamily 返回基类列的类型族；用于 constraint 未解析到具体实体字段时的 op/value 校验。
func baseColumnFamily(name string) string {
	switch name {
	case "id", "status":
		return "integer"
	case "created_at", "updated_at", "deleted_at":
		return "timestamp"
	}
	return ""
}

func (c *Checker) entityFieldFamily(f *entityFieldInfo) string {
	if f == nil || f.Type == "" {
		return ""
	}
	typ := strings.TrimPrefix(strings.TrimSpace(f.Type), "*")
	if strings.HasPrefix(typ, "data.enums.") {
		return "integer"
	}
	if family := c.valueType(typ); family != "" {
		return family
	}
	if family, ok := paramFamily(typ); ok {
		return family
	}
	return ""
}

func isScalarValue(v any) bool {
	switch v.(type) {
	case string, bool, int, int64, uint64, float64:
		return true
	}
	return false
}

func ruleDeclaresError(rule map[string]any, code string) bool {
	for _, raw := range asList(rule["errors"]) {
		if declared, _ := raw.(string); declared == code {
			return true
		}
	}
	return false
}

// rules.go 校验 feature.rules 文档与规则项。
package check

import (
	"path/filepath"
	"strings"
)

func (c *Checker) checkRuleDoc(doc map[string]any, rel string, schema *Schema, serviceIDs map[string]bool) {
	c.validateDoc(doc, schema.Fields, rel)
	c.checkFieldOrder(rel, schema.Fields, orderRules{})
	c.checkStability(doc, rel)
	fileID, _ := doc["id"].(string)
	want := schema.Targets.FileID(filepath.Base(rel))
	if fileID != want {
		c.rulef("rule-id-unique", "%s: id %q must be %q", rel, fileID, want)
	}
	seen := map[string]bool{}
	for _, raw := range asList(doc["rules"]) {
		item, _ := asMap(raw)
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		if seen[id] {
			c.rulef("rule-id-unique", "%s: duplicate rule id %s", rel, id)
		}
		seen[id] = true
		if !strings.HasPrefix(id, fileID+".") {
			c.rulef("rule-id-unique", "%s: rule id %s must start with %s.", rel, id, fileID)
		}
		kind, _ := item["kind"].(string)
		reject := func(fields ...string) {
			for _, field := range fields {
				if item[field] != nil {
					c.rulef("rule-structure", "%s: %s %s must not declare %s", rel, kind, id, field)
				}
			}
		}
		switch kind {
		case "constraint":
			entityID, _ := item["entity"].(string)
			field, _ := item["field"].(string)
			if entityID == "" {
				c.rulef("rule-structure", "%s: constraint %s requires entity", rel, id)
			} else if ent := c.entities[entityID]; ent == nil {
				c.rulef("rule-structure", "%s: constraint %s unknown entity %s", rel, id, entityID)
			} else if field != "" {
				f := findEntityFieldByJSONName(ent, field)
				if f == nil {
					f = findEntityField(ent, field)
				}
				if f == nil && !isBaseColumn(field) {
					c.rulef("rule-structure", "%s: constraint %s field %s not found on %s", rel, id, field, entityID)
				} else if f != nil && f.IsRelation {
					c.rulef("rule-structure", "%s: constraint %s field %s must be scalar", rel, id, field)
				}
			}
			if field == "" || item["op"] == nil || item["value"] == nil {
				c.rulef("rule-structure", "%s: constraint %s requires field/op/value", rel, id)
			}
			reject("assert", "given", "when", "then")
		case "invariant":
			if s, _ := item["assert"].(string); strings.TrimSpace(s) == "" {
				c.rulef("rule-structure", "%s: invariant %s requires assert", rel, id)
			}
			reject("entity", "field", "op", "value", "given", "when", "then")
		case "policy":
			reject("entity", "field", "op", "value", "assert")
		default:
			c.rulef("rule-structure", "%s: rule %s kind must be constraint/invariant/policy", rel, id)
		}
		if code, _ := item["on_violation"].(string); code != "" && !c.errorCodes()[strings.TrimPrefix(code, "platform.errors.")] {
			c.rulef("rule-structure", "%s: rule %s unknown on_violation %s", rel, id, code)
		}
		for _, rawSvc := range asList(item["implemented_by"]) {
			svc, _ := rawSvc.(string)
			if !serviceIDs[svc] {
				c.rulef("rule-structure", "%s: rule %s unknown service %s", rel, id, svc)
			}
		}
	}
}

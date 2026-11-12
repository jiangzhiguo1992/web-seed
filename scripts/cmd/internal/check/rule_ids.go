// rule_ids.go 校验 rules.implemented_by 与生成文件 rule_ids_gen.go 的一致性。
//
// knownRuleIDs 由 gen_rule_ids 从 check 源码的 rulef("...") 调用生成，
// 因此独立二进制也带完整映射，不依赖运行时读取源码。
package check

import (
	"strings"
)

// checkImplementedBy 校验规则声明的实现：
//   - implemented_by 非空；
//   - 必须包含规则自身 ID，防止把实现偷懒映射到别的错误码；
//   - 每个 ID 都是 checker 实际输出的 [rule-id]；
//   - 同一规则内不重复。
func (c *Checker) checkImplementedBy(ruleID, where string, implementedBy []string) {
	if len(implementedBy) == 0 {
		c.rulef("schema.rules", "%s: must declare implemented_by", where)
		return
	}
	seen := map[string]bool{}
	for _, id := range implementedBy {
		if id == "" || !knownRuleIDs[id] {
			c.rulef("schema.rules", "%s: implemented_by %q is not emitted by checker", where, id)
			continue
		}
		if seen[id] {
			c.rulef("schema.rules", "%s: duplicate implemented_by %q", where, id)
		}
		seen[id] = true
		c.declareRuleID(id)
	}
	if ruleID != "" && !seen[ruleID] {
		c.rulef("schema.rules", "%s: implemented_by must include rule id %q", where, ruleID)
	}
}

func (c *Checker) declareRuleID(id string) {
	if c.declaredRuleIDs == nil {
		c.declaredRuleIDs = map[string]bool{}
	}
	c.declaredRuleIDs[id] = true
}

// checkUndeclaredRuleIDs 反向校验：checker 实际输出的非 schema.* 错误码
// 必须出现在某个 rules/schema_rules 的 implemented_by 中。
func (c *Checker) checkUndeclaredRuleIDs() {
	for id := range knownRuleIDs {
		if strings.HasPrefix(id, "schema.") {
			continue
		}
		if !c.declaredRuleIDs[id] {
			c.rulef("schema.rules", "rule id %q is emitted by checker but not declared in any rules list", id)
		}
	}
}

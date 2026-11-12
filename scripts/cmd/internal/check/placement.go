// placement.go 固化 rules / do_not 的位置：rules 只在 _meta（领域规则集除外），
// do_not 只在具体契约；_meta 下不得出现 do_not。
package check

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// 领域规则集本身以 rules 为内容，不属于“类型级规则清单”位置约束。
var rulesAllowedPrefixes = []string{
	"contracts/business/rules/",
}

var rulesAllowedFiles = map[string]bool{
	"contracts/platform/architecture/go_arch.yaml":     true,
	"contracts/platform/observability/log_fields.yaml": true,
}

func (c *Checker) checkFieldPlacement() {
	for _, file := range walkYAML(c.root, "contracts") {
		rel := relPath(c.root, file)
		if strings.HasPrefix(rel, "contracts/generated/") {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		var doc yaml.Node
		if err := yaml.Unmarshal(b, &doc); err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		if topLevelFieldsFlow(&doc) {
			c.rulef("schema.fields-style", "%s: top-level fields must be block style (one key per line)", rel)
		}
		if strings.HasPrefix(rel, "contracts/_meta/") {
			if nodeHasKey(&doc, "do_not") {
				c.rulef("schema.meta-placement", "%s: _meta must not declare do_not; put instance negatives in concrete contracts", rel)
			}
			continue
		}
		if topLevelHasKey(&doc, "rules") && !rulesAllowed(rel) {
			c.rulef("schema.meta-placement", "%s: concrete contract must not declare top-level rules; type rules belong in _meta", rel)
		}
	}
}

// checkDoNotDuplicates 防止把组级指导重新抄回具体契约。
func (c *Checker) checkDoNotDuplicates() {
	seen := map[string]string{}
	for _, file := range walkYAML(c.root, "contracts") {
		rel := relPath(c.root, file)
		if strings.HasPrefix(rel, "contracts/_meta/") || strings.HasPrefix(rel, "contracts/generated/") {
			continue
		}
		if rel == baseEntityPath || rel == "contracts/data/tables/_base.yaml" {
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		group := ""
		if parts := strings.Split(rel, "/"); len(parts) > 1 {
			group = parts[1]
		}
		guidance := c.guidanceLines(group)
		for _, raw := range asList(doc["do_not"]) {
			s, _ := raw.(string)
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			if guidance[s] {
				c.rulef("schema.do-not-duplicate", "%s: do_not duplicates group guidance: %s", rel, s)
				continue
			}
			if prev, ok := seen[s]; ok && prev != rel {
				c.rulef("schema.do-not-duplicate", "%s: do_not duplicates %s: %s", rel, prev, s)
				continue
			}
			seen[s] = rel
		}
	}
}

func (c *Checker) guidanceLines(group string) map[string]bool {
	out := map[string]bool{}
	files := append(glob(c.root, "contracts/README.md"), glob(c.root, "contracts/"+group+"/README.md")...)
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(b), "\n") {
			s := strings.TrimSpace(line)
			if strings.HasPrefix(s, "- ") {
				item := strings.TrimSpace(s[2:])
				item = strings.Trim(item, `"`)
				out[item] = true
			}
		}
	}
	if group == "data" {
		for _, rel := range []string{"contracts/data/tables/_base.yaml", baseEntityPath} {
			doc, err := loadYAML(filepath.Join(c.root, rel))
			if err != nil {
				continue
			}
			for _, raw := range asList(doc["do_not"]) {
				if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
					out[strings.TrimSpace(s)] = true
				}
			}
		}
	}
	return out
}

func rulesAllowed(rel string) bool {
	if rulesAllowedFiles[rel] {
		return true
	}
	for _, p := range rulesAllowedPrefixes {
		if strings.HasPrefix(rel, p) {
			return true
		}
	}
	return false
}

// topLevelFieldsFlow 只检查顶层 fields：字段定义必须竖排；嵌套 item_fields 不限制。
func topLevelFieldsFlow(n *yaml.Node) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return false
		}
		return topLevelFieldsFlow(n.Content[0])
	}
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value != "fields" {
			continue
		}
		v := n.Content[i+1]
		if v.Kind == yaml.SequenceNode && len(v.Content) == 0 {
			return false
		}
		if v.Style == yaml.FlowStyle {
			return true
		}
		if v.Kind != yaml.SequenceNode {
			return false
		}
		for _, item := range v.Content {
			if item.Style == yaml.FlowStyle {
				return true
			}
		}
		return false
	}
	return false
}

func topLevelHasKey(n *yaml.Node, key string) bool {
	if n == nil {
		return false
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return false
		}
		return topLevelHasKey(n.Content[0], key)
	}
	if n.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return true
		}
	}
	return false
}

func nodeHasKey(n *yaml.Node, key string) bool {
	if n == nil {
		return false
	}
	switch n.Kind {
	case yaml.DocumentNode, yaml.SequenceNode:
		for _, child := range n.Content {
			if nodeHasKey(child, key) {
				return true
			}
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				return true
			}
			if nodeHasKey(n.Content[i+1], key) {
				return true
			}
		}
	}
	return false
}

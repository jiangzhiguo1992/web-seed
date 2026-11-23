// platform.go 校验 platform 单例契约的字段规范与字段顺序。
// 这些契约没有实例增长，targets 使用固定文件而不是 <file> 模板；
// 语义检查仍由 modules/access/errors 等领域文件负责。
package check

import (
	"fmt"
	"path/filepath"
	"strings"
)

var platformSingletonTypes = []string{
	"platform.environments",
	"platform.modules",
	"platform.quality",
	"platform.versioning",
	"platform.observability.log_fields",
	"platform.observability.metrics",
	"platform.architecture.go_arch",
	"platform.analytics.events",
	"platform.security.access",
	"platform.security.permissions",
	"platform.security.roles",
	"platform.security.auth_policy",
}

func (c *Checker) checkPlatformContracts() {
	for _, typ := range platformSingletonTypes {
		schema, err := c.loadSchemaByType(typ)
		if err != nil {
			c.rulef("schema.load", "load %s schema: %v", typ, err)
			continue
		}
		c.checkRules(schema)
		if err := checkSingletonTargets(schema); err != nil {
			c.rulef("schema.targets", "%s: %v", schema.ID, err)
			continue
		}
		c.valueTypes = schema.ValueTypes
		c.nodeKinds = schema.NodeKinds
		c.propertyKeys = schema.PropertyKeys
		rel := filepath.Join(schema.Targets.Dir, schema.Targets.Glob)
		doc, err := loadYAML(filepath.Join(c.root, rel))
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkStability(doc, rel)
		if id, _ := doc["id"].(string); id != schema.Targets.ID {
			c.rulef("schema.id-from-file", "%s: id must be %s, got %q", rel, schema.Targets.ID, id)
		}
		if typ == "platform.environments" {
			c.checkEnvironmentShape(rel, doc)
		}
	}
}

// checkSingletonTargets 校验单文件契约的 targets：固定文件，不允许 <file>。
func checkSingletonTargets(schema *Schema) error {
	t := schema.Targets
	if t.Dir == "" || t.Glob == "" || t.ID == "" {
		return fmt.Errorf("targets.dir/glob/id must not be empty")
	}
	if strings.Contains(t.ID, "<file>") || strings.Contains(t.Glob, "*") {
		return fmt.Errorf("singleton targets must use a fixed file, got id=%q glob=%q", t.ID, t.Glob)
	}
	return nil
}

// checkEnvironmentShape 校验环境名与差异键的语义。
func (c *Checker) checkEnvironmentShape(rel string, doc map[string]any) {
	seen := map[string]bool{}
	for i, raw := range asList(doc["environments"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if seen[name] {
			c.rulef("environment-shape", "%s: environments[%d] duplicate name %s", rel, i, name)
		}
		seen[name] = true
	}
	for i, raw := range asList(doc["differences"]) {
		item, _ := asMap(raw)
		if key, _ := item["key"].(string); key == "" {
			c.rulef("environment-shape", "%s: differences[%d].key must not be empty", rel, i)
		}
	}
}

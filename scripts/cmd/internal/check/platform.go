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
	"platform.http_conventions",
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

// environmentValueSets 展开每个环境对象的叶子值：key → 取值集合；缺失值也计入集合。
func environmentValueSets(environments []any) map[string]map[string]bool {
	flat := make([]map[string]string, 0, len(environments))
	for _, raw := range environments {
		env, _ := asMap(raw)
		if env == nil {
			continue
		}
		values := map[string]string{}
		flattenEnvironment(env, "", values)
		flat = append(flat, values)
	}
	out := map[string]map[string]bool{}
	for _, values := range flat {
		for key := range values {
			if out[key] == nil {
				out[key] = map[string]bool{}
			}
		}
	}
	for _, values := range flat {
		for key := range out {
			value, ok := values[key]
			if !ok {
				value = "\x00missing"
			}
			out[key][value] = true
		}
	}
	return out
}

func flattenEnvironment(env map[string]any, prefix string, out map[string]string) {
	for key, raw := range env {
		if prefix == "" && (key == "name" || key == "description") {
			continue
		}
		full := key
		if prefix != "" {
			full = prefix + "." + key
		}
		if child, ok := asMap(raw); ok {
			flattenEnvironment(child, full, out)
			continue
		}
		out[full] = fmt.Sprint(raw)
	}
}

// checkEnvironmentShape 校验差异项与密钥来源只引用已声明环境，且密钥来源覆盖全部环境。
func (c *Checker) checkEnvironmentShape(rel string, doc map[string]any) {
	envs := map[string]bool{}
	for _, raw := range asList(doc["environments"]) {
		item, _ := asMap(raw)
		if name, _ := item["name"].(string); name != "" {
			if envs[name] {
				c.rulef("environment-shape", "%s: duplicate environment %s", rel, name)
			}
			envs[name] = true
		}
		security, _ := asMap(item["security"])
		if require, _ := security["require_https"].(bool); require {
			for _, key := range []string{"api_base_url", "web_base_url"} {
				if value, _ := item[key].(string); value != "" && !strings.HasPrefix(value, "https://") {
					c.rulef("environment-shape", "%s: %s must use https when security.require_https is true", rel, key)
				}
			}
		}
	}
	differences := map[string]bool{}
	for i, raw := range asList(doc["differences"]) {
		item, _ := asMap(raw)
		if key, _ := item["key"].(string); key != "" {
			differences[key] = true
		}
		for _, rawEnv := range asList(item["environments"]) {
			env, _ := rawEnv.(string)
			if env != "" && !envs[env] {
				c.rulef("environment-shape", "%s: differences[%d] references unknown environment %s", rel, i, env)
			}
		}
	}
	values := environmentValueSets(asList(doc["environments"]))
	for key, set := range values {
		if len(set) > 1 && !differences[key] {
			c.rulef("environment-shape", "%s: environment difference %s is not registered in differences", rel, key)
		}
	}
	for key := range differences {
		if len(values[key]) <= 1 {
			c.rulef("environment-shape", "%s: differences key %s is not actually different across environments", rel, key)
		}
	}
	secrets, hasSecrets := asMap(doc["secrets_source"])
	if hasSecrets {
		for name := range secrets {
			if !envs[name] {
				c.rulef("environment-shape", "%s: secrets_source.%s is not a declared environment", rel, name)
			}
		}
		for name := range envs {
			if _, ok := secrets[name]; !ok {
				c.rulef("environment-shape", "%s: secrets_source is missing environment %s", rel, name)
			}
		}
	}
}

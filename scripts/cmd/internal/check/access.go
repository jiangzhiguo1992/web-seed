// access.go 校验 platform.security.access：入口权限、审计与限流绑定。
package check

import "path/filepath"

type accessPolicy struct {
	permissions []string
	audit       bool
	rateLimited bool
}

func (c *Checker) permissionExists(id string) bool {
	if c.permissionSet == nil {
		c.permissionSet = map[string]bool{}
		doc, err := loadYAML(filepath.Join(c.root, "contracts/platform/security/permissions.yaml"))
		if err == nil {
			for _, raw := range asList(doc["permissions"]) {
				if m, ok := asMap(raw); ok {
					if code, _ := m["code"].(string); code != "" {
						c.permissionSet["platform.security.permissions."+code] = true
					}
				}
			}
		}
	}
	return c.permissionSet[id]
}

func (c *Checker) checkAccess(serviceIDs map[string]bool) {
	if schema, err := c.loadSchemaByType("platform.security.access"); err == nil {
		c.checkRules(schema)
	}
	path := filepath.Join(c.root, "contracts/platform/security/access.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		c.rulef("schema.load", "%s: %v", relPath(c.root, path), err)
		return
	}
	rel := relPath(c.root, path)
	if s, _ := doc["id"].(string); s != "platform.security.access" {
		c.rulef("schema.required", "%s: id must be platform.security.access", rel)
	}
	if s, _ := doc["summary"].(string); s == "" {
		c.rulef("schema.required", "%s: summary must not be empty", rel)
	}
	if _, ok := doc["schema_version"]; !ok {
		c.rulef("schema.required", "%s: missing required field schema_version", rel)
	}
	if _, ok := doc["stability"]; !ok {
		c.rulef("schema.required", "%s: missing required field stability", rel)
	}
	if _, ok := doc["do_not"]; !ok {
		c.rulef("schema.required", "%s: missing required field do_not", rel)
	}
	c.accessBindings = map[string]accessPolicy{}
	for i, raw := range asList(doc["bindings"]) {
		item, _ := asMap(raw)
		target, _ := item["target"].(string)
		if !serviceIDs[target] {
			c.rulef("schema.ref", "%s: bindings[%d] target %q is not a service item", rel, i, target)
			continue
		}
		if _, exists := c.accessBindings[target]; exists {
			c.rulef("schema.unique", "%s: duplicate binding for %s", rel, target)
			continue
		}
		policy := accessPolicy{}
		for key := range item {
			switch key {
			case "target", "permissions", "audit", "rate_limit":
			default:
				c.rulef("schema.unknown-field", "%s: %s binding has unknown field %s", rel, target, key)
			}
		}
		if rawPerms, ok := item["permissions"]; ok {
			if _, ok := rawPerms.([]any); !ok {
				c.rulef("schema.type", "%s: %s permissions must be a list", rel, target)
			}
		}
		for _, rawPerm := range asList(item["permissions"]) {
			perm, _ := rawPerm.(string)
			if !c.permissionExists(perm) {
				c.rulef("schema.ref", "%s: %s references unknown permission %s", rel, target, perm)
				continue
			}
			policy.permissions = append(policy.permissions, perm)
		}
		if rawAudit, ok := item["audit"]; ok {
			audit, ok := rawAudit.(bool)
			if !ok {
				c.rulef("schema.type", "%s: %s audit must be a boolean", rel, target)
			}
			policy.audit = audit
		}
		if rawRL, ok := item["rate_limit"]; ok {
			rl, ok := asMap(rawRL)
			if !ok {
				c.rulef("schema.type", "%s: %s rate_limit must be an object", rel, target)
			} else {
				for key := range rl {
					if key != "requests" && key != "window_seconds" {
						c.rulef("schema.unknown-field", "%s: %s rate_limit.%s is not allowed", rel, target, key)
					}
				}
				if n, ok := toInt(rl["requests"]); !ok || n < 1 {
					c.rulef("schema.required", "%s: %s rate_limit.requests must be >= 1", rel, target)
				}
				if n, ok := toInt(rl["window_seconds"]); !ok || n < 1 {
					c.rulef("schema.required", "%s: %s rate_limit.window_seconds must be >= 1", rel, target)
				}
				policy.rateLimited = true
			}
		}
		if len(policy.permissions) == 0 && !policy.audit && !policy.rateLimited {
			c.rulef("schema.required", "%s: %s must declare permissions, audit, or rate_limit", rel, target)
		}
		c.accessBindings[target] = policy
	}
}

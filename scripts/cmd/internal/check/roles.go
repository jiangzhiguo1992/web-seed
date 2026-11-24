// roles.go 校验角色继承、权限与约束。
package check

import (
	"path/filepath"
)

func (c *Checker) checkRoles() {
	c.checkPermissions()
	path := filepath.Join(c.root, "contracts/platform/security/roles.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	roles := map[string]map[string]any{}
	order := []string{}
	for _, raw := range asList(doc["roles"]) {
		item, _ := asMap(raw)
		code, _ := item["code"].(string)
		if code == "" {
			continue
		}
		roles["platform.security.roles."+code] = item
		order = append(order, "platform.security.roles."+code)
	}
	constraints, _ := asMap(doc["constraints"])
	maxDepth, _ := toInt(constraints["max_inheritance_depth"])
	cycleForbidden, _ := constraints["cycle_forbidden"].(bool)
	unknownForbidden, _ := constraints["unknown_permission_forbidden"].(bool)
	depth := map[string]int{}
	var resolve func(id string, stack map[string]bool) int
	resolve = func(id string, stack map[string]bool) int {
		if d, ok := depth[id]; ok {
			return d
		}
		item := roles[id]
		if item == nil {
			return 0
		}
		if stack[id] {
			if cycleForbidden {
				c.rulef("role-inheritance", "%s: inheritance cycle at %s", rel, id)
			}
			return 0
		}
		stack[id] = true
		max := 0
		for _, raw := range asList(item["inherits"]) {
			parent, _ := raw.(string)
			if parent == id {
				c.rulef("role-inheritance", "%s: role %s inherits itself", rel, id)
				continue
			}
			if roles[parent] == nil {
				c.rulef("role-inheritance", "%s: role %s inherits unknown %s", rel, id, parent)
				continue
			}
			if d := resolve(parent, stack); d+1 > max {
				max = d + 1
			}
		}
		delete(stack, id)
		depth[id] = max
		return max
	}
	for _, id := range order {
		if d := resolve(id, map[string]bool{}); maxDepth > 0 && d > maxDepth {
			c.rulef("role-inheritance", "%s: role %s inheritance depth %d exceeds %d", rel, id, d, maxDepth)
		}
	}
	allowedWildcard := c.wildcardRoles(roles)
	for _, id := range order {
		item := roles[id]
		for _, raw := range asList(item["permissions"]) {
			perm, _ := raw.(string)
			if perm == "*" {
				if !allowedWildcard[id] {
					c.rulef("role-permissions", "%s: role %s uses wildcard but is not in permissions.boundaries.wildcard.allowed_for", rel, id)
				}
				continue
			}
			if unknownForbidden && !c.permissionExists(perm) {
				c.rulef("role-permissions", "%s: role %s references unknown permission %s", rel, id, perm)
			}
		}
	}
	if defaultRole, _ := doc["default_role"].(string); defaultRole != "" && roles[defaultRole] == nil {
		c.rulef("role-permissions", "%s: default_role %s is not a declared role", rel, defaultRole)
	}
}

// checkPermissions 校验权限码由 <resource>_<action> 组成，action 来自 naming.actions。
func (c *Checker) checkPermissions() {
	path := filepath.Join(c.root, "contracts/platform/security/permissions.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	naming, _ := asMap(doc["naming"])
	if pattern, _ := naming["pattern"].(string); pattern != "<resource>_<action>" {
		c.rulef("permission-code-shape", "%s: naming.pattern must be <resource>_<action>, got %q", rel, pattern)
	}
	actions := map[string]bool{}
	for _, raw := range asList(naming["actions"]) {
		action, _ := raw.(string)
		if action != "" {
			actions[action] = true
		}
	}
	for i, raw := range asList(doc["permissions"]) {
		item, _ := asMap(raw)
		code, _ := item["code"].(string)
		resource, _ := item["resource"].(string)
		action, _ := item["action"].(string)
		if code == "" || resource == "" || action == "" {
			continue // schema.required 负责
		}
		if code != resource+"_"+action {
			c.rulef("permission-code-shape", "%s: permissions[%d] code %s must be %s_%s", rel, i, code, resource, action)
		}
		if !actions[action] {
			c.rulef("permission-code-shape", "%s: permissions[%d] (%s) action %s is not in naming.actions", rel, i, code, action)
		}
	}
}

// wildcardRoles 读取 permissions.boundaries.wildcard.allowed_for，并校验其角色引用。
func (c *Checker) wildcardRoles(roles map[string]map[string]any) map[string]bool {
	out := map[string]bool{}
	path := filepath.Join(c.root, "contracts/platform/security/permissions.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return out
	}
	boundaries, _ := asMap(doc["boundaries"])
	wildcard, _ := asMap(boundaries["wildcard"])
	for _, raw := range asList(wildcard["allowed_for"]) {
		id, _ := raw.(string)
		if id == "" {
			continue
		}
		out[id] = true
		if roles[id] == nil {
			c.rulef("role-permissions", "%s: wildcard allowed_for references unknown role %s", relPath(c.root, path), id)
		}
	}
	return out
}

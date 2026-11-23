// versioning.go 校验版本、兼容命令与弃用头引用。
package check

import (
	"path/filepath"
	"strings"
)

func (c *Checker) checkVersioning() {
	path := filepath.Join(c.root, "contracts/platform/versioning.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	versions := map[string]bool{}
	if project, err := loadYAML(filepath.Join(c.root, "contracts/project.yaml")); err == nil {
		apps, _ := asMap(project["apps"])
		for _, raw := range apps {
			app, _ := asMap(raw)
			if version, _ := app["api_version"].(string); version != "" {
				versions[version] = true
			}
		}
	}
	api, _ := asMap(doc["api"])
	if current, _ := api["current"].(string); current != "" && !versions[current] {
		c.rulef("versioning-refs", "%s: api.current %s is not a declared app api_version", rel, current)
	}
	detection, _ := asMap(doc["detection"])
	command, _ := detection["command"].(string)
	if strings.HasPrefix(command, "make ") {
		targets := makeTargets(filepath.Join(c.root, "Makefile"))
		target := strings.Fields(command)[1]
		if len(targets) > 0 && target != "" && !targets[target] {
			c.rulef("versioning-refs", "%s: detection.command target %s is not in Makefile", rel, target)
		}
	}
	headers := map[string]bool{}
	if conventions, err := loadYAML(filepath.Join(c.root, "contracts/platform/http_conventions.yaml")); err == nil {
		headerMap, _ := asMap(conventions["headers"])
		for _, raw := range asList(headerMap["response"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); name != "" {
				headers[name] = true
			}
		}
	}
	compat, _ := asMap(doc["compatibility"])
	seen := map[string]bool{}
	for i, raw := range asList(compat["accepted_breaking_changes"]) {
		item, _ := asMap(raw)
		id, _ := item["id"].(string)
		if id == "" {
			c.rulef("versioning-refs", "%s: accepted_breaking_changes[%d] requires id", rel, i)
			continue
		}
		if seen[id] {
			c.rulef("versioning-refs", "%s: duplicate accepted breaking change %s", rel, id)
		}
		seen[id] = true
		for _, rawScope := range asList(item["scope"]) {
			scope, _ := rawScope.(string)
			if scope != "" && !versions[scope] {
				c.rulef("versioning-refs", "%s: accepted breaking change %s scope %s is not an app api_version", rel, id, scope)
			}
		}
	}
	deprecation, _ := asMap(doc["deprecation"])
	for _, raw := range asList(deprecation["headers"]) {
		item, _ := asMap(raw)
		if name, _ := item["name"].(string); name != "" && !headers[name] {
			c.rulef("versioning-refs", "%s: deprecation header %s is not registered in http_conventions.headers.response", rel, name)
		}
	}
}

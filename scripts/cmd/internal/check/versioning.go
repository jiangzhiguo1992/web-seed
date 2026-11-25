// versioning.go 校验版本、兼容命令与弃用头引用。
package check

import (
	"net/http"
	"path/filepath"
	"strconv"
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
	for _, file := range glob(c.root, "contracts/capabilities/protocols/http/*.yaml") {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		mount, _ := asMap(doc["mount"])
		if version, _ := mount["api_version"].(string); version != "" {
			versions[version] = true
		}
	}
	api, _ := asMap(doc["api"])
	if current, _ := api["current"].(string); current != "" && !versions[current] {
		c.rulef("versioning-refs", "%s: api.current %s is not declared by any HTTP mount.api_version", rel, current)
	}
	detection, _ := asMap(doc["detection"])
	targets := makeTargets(filepath.Join(c.root, "Makefile"))
	for _, key := range []string{"command", "baseline_command"} {
		command, _ := detection[key].(string)
		target := makeCommandTarget(command)
		if len(targets) > 0 && target != "" && !targets[target] {
			c.rulef("versioning-refs", "%s: detection.%s target %s is not in Makefile", rel, key, target)
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
				c.rulef("versioning-refs", "%s: accepted breaking change %s scope %s is not an HTTP mount.api_version", rel, id, scope)
			}
		}
	}
	deprecation, _ := asMap(doc["deprecation"])
	for _, raw := range asList(deprecation["headers"]) {
		item, _ := asMap(raw)
		if name, _ := item["name"].(string); name != "" && !headers[name] {
			c.rulef("versioning-refs", "%s: deprecation header %s is not registered in http_conventions.headers.response", rel, name)
		}
		format, _ := item["format"].(string)
		example, _ := item["example"].(string)
		switch format {
		case "RFC9745":
			value, ok := strings.CutPrefix(example, "@")
			if _, err := strconv.ParseInt(value, 10, 64); !ok || err != nil {
				c.rulef("versioning-refs", "%s: deprecation header %s example must be @<unix-seconds>", rel, item["name"])
			}
		case "HTTP-date":
			if _, err := http.ParseTime(example); err != nil {
				c.rulef("versioning-refs", "%s: deprecation header %s example must be HTTP-date", rel, item["name"])
			}
		default:
			c.rulef("versioning-refs", "%s: deprecation header %s has unknown format %q", rel, item["name"], format)
		}
	}
}

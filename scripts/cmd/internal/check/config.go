// config.go 校验 platform/config/env.json 的 JSON Schema 与环境枚举。
package check

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func (c *Checker) checkConfigEnv() {
	path := filepath.Join(c.root, "contracts/platform/config/env.json")
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		c.rulef("schema.ref", "%s: %v", rel, err)
		return
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("file://"+path, doc); err != nil {
		c.rulef("schema.ref", "%s: %v", rel, err)
		return
	}
	if _, err := compiler.Compile("file://" + path); err != nil {
		c.rulef("schema.ref", "%s: invalid JSON Schema: %v", rel, err)
	}
	docMap, _ := asMap(doc)
	props, _ := docMap["properties"].(map[string]any)
	if props == nil {
		return
	}
	envs := map[string]bool{}
	if env, err := loadYAML(filepath.Join(c.root, "contracts/platform/environments.yaml")); err == nil {
		for _, raw := range asList(env["environments"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); name != "" {
				envs[name] = true
			}
		}
	}
	required := map[string]bool{}
	for _, raw := range asList(docMap["required"]) {
		if name, _ := raw.(string); name != "" {
			required[name] = true
		}
	}
	for _, name := range []string{"APP_ENV", "HTTP_ADDR"} {
		if !required[name] {
			c.rulef("schema.ref", "%s: required must include %s", rel, name)
		}
	}
	if additional, ok := docMap["additionalProperties"].(bool); !ok || additional {
		c.rulef("schema.ref", "%s: additionalProperties must be false", rel)
	}
	app, _ := asMap(props["APP_ENV"])
	if app == nil {
		c.rulef("schema.ref", "%s: properties.APP_ENV is missing", rel)
	} else if len(envs) > 0 {
		got := map[string]bool{}
		for _, raw := range asList(app["enum"]) {
			if name, _ := raw.(string); name != "" {
				got[name] = true
				if !envs[name] {
					c.rulef("schema.ref", "%s: APP_ENV enum %s is not declared in platform.environments", rel, name)
				}
			}
		}
		for name := range envs {
			if !got[name] {
				c.rulef("schema.ref", "%s: APP_ENV enum is missing environment %s", rel, name)
			}
		}
		if def, _ := app["default"].(string); def != "" && !got[def] {
			c.rulef("schema.ref", "%s: APP_ENV default %s is not in enum", rel, def)
		}
	}
	levels := map[string]bool{}
	if lf, err := loadYAML(filepath.Join(c.root, "contracts/platform/observability/log_fields.yaml")); err == nil {
		for _, raw := range asList(lf["required"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); name == "level" {
				for _, rawLevel := range asList(item["enum"]) {
					if level, _ := rawLevel.(string); level != "" {
						levels[level] = true
					}
				}
			}
		}
	}
	logLevel, _ := asMap(props["LOG_LEVEL"])
	if logLevel == nil {
		c.rulef("schema.ref", "%s: properties.LOG_LEVEL is missing", rel)
	} else if len(levels) > 0 {
		got := map[string]bool{}
		for _, raw := range asList(logLevel["enum"]) {
			if level, _ := raw.(string); level != "" {
				got[level] = true
				if !levels[level] {
					c.rulef("schema.ref", "%s: LOG_LEVEL enum %s is not declared in log_fields level", rel, level)
				}
			}
		}
		for level := range levels {
			if !got[level] {
				c.rulef("schema.ref", "%s: LOG_LEVEL enum is missing level %s", rel, level)
			}
		}
		if def, _ := logLevel["default"].(string); def != "" && !got[def] {
			c.rulef("schema.ref", "%s: LOG_LEVEL default %s is not in enum", rel, def)
		}
	}
	for name, raw := range props {
		prop, _ := asMap(raw)
		if secret, _ := prop["secret"].(bool); secret {
			if typ, _ := prop["type"].(string); typ != "string" {
				c.rulef("schema.ref", "%s: secret property %s must be type string", rel, name)
			}
		}
	}
}

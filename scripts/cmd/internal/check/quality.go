// quality.go 校验测试门槛引用的场景与 Make 目标。
package check

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func (c *Checker) checkQuality() {
	path := filepath.Join(c.root, "contracts/platform/quality.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	scenarios := c.scenarioIDs()
	if e2e, _ := asMap(doc["e2e"]); e2e != nil {
		for _, raw := range asList(e2e["required_scenarios"]) {
			id, _ := raw.(string)
			if id != "" && !scenarios[id] {
				c.rulef("quality-refs", "%s: e2e.required_scenarios references unknown %s", rel, id)
			}
		}
	}
	targets := makeTargets(filepath.Join(c.root, "Makefile"))
	for i, raw := range asList(doc["gates"]) {
		if len(targets) == 0 {
			break
		}
		gate, _ := asMap(raw)
		command, _ := gate["command"].(string)
		if !strings.HasPrefix(command, "make ") {
			continue
		}
		target := strings.Fields(command)[1]
		if target != "" && !targets[target] {
			c.rulef("quality-refs", "%s: gates[%d] command target %s is not in Makefile", rel, i, target)
		}
	}
	seen := map[string]bool{}
	for i, raw := range asList(doc["must_test"]) {
		item, _ := asMap(raw)
		id, _ := item["id"].(string)
		if id == "" {
			continue
		}
		if seen[id] {
			c.rulef("quality-refs", "%s: must_test[%d] duplicate id %s", rel, i, id)
		}
		seen[id] = true
	}
}

func (c *Checker) scenarioIDs() map[string]bool {
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(c.root, "contracts/scenarios/http/*.yaml"))
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["scenarios"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); fileID != "" && name != "" {
				out[fileID+"."+name] = true
			}
		}
	}
	return out
}

var makeTargetRe = regexp.MustCompile(`(?m)^([A-Za-z0-9_-]+):`)

func makeTargets(path string) map[string]bool {
	out := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, m := range makeTargetRe.FindAllStringSubmatch(string(b), -1) {
		out[m[1]] = true
	}
	return out
}

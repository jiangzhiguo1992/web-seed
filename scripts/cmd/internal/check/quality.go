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
	hasApps := c.appTreeExists()
	if hasApps {
		c.checkCoveragePackages(rel, asMapValue(doc["coverage"]))
	}
	scenarios := c.scenarioIDs()
	if e2e, _ := asMap(doc["e2e"]); e2e != nil {
		for _, raw := range asList(e2e["required_scenarios"]) {
			id, _ := raw.(string)
			if id != "" && !scenarios[id] {
				c.rulef("quality-refs", "%s: e2e.required_scenarios references unknown %s", rel, id)
			}
		}
		if source, _ := e2e["base_url_source"].(string); source != "" {
			if ids := c.contractIDs(); len(ids) > 0 && !ids[source] {
				c.rulef("quality-refs", "%s: e2e.base_url_source references unknown contract %s", rel, source)
			}
		}
	}
	targets := makeTargets(filepath.Join(c.root, "Makefile"))
	hasBlocking := false
	for i, raw := range asList(doc["gates"]) {
		gate, _ := asMap(raw)
		if blocking, _ := gate["blocking"].(bool); blocking {
			hasBlocking = true
		}
		if len(targets) == 0 {
			continue
		}
		command, _ := gate["command"].(string)
		target := makeCommandTarget(command)
		if target != "" && !targets[target] {
			c.rulef("quality-refs", "%s: gates[%d] command target %s is not in Makefile", rel, i, target)
		}
	}
	if len(asList(doc["gates"])) > 0 && !hasBlocking {
		c.rulef("quality-refs", "%s: gates must contain at least one blocking gate", rel)
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
		status, _ := item["status"].(string)
		if hasApps && (status == "" || status == "required") {
			where, _ := item["where"].(string)
			matches, _ := filepath.Glob(filepath.Join(c.root, where))
			if len(matches) == 0 {
				c.rulef("quality-refs", "%s: must_test[%d] where %s does not match any file", rel, i, where)
			}
		}
	}
}

// appTreeExists 报告仓库是否包含 apps/（contracts-only 骨架测试不检查应用路径）。
func (c *Checker) appTreeExists() bool {
	info, err := os.Stat(filepath.Join(c.root, "apps"))
	return err == nil && info.IsDir()
}

// checkCoveragePackages 校验覆盖率统计包在对应应用中存在。
func (c *Checker) checkCoveragePackages(rel string, coverage map[string]any) {
	for _, kind := range []string{"api", "web"} {
		part, _ := asMap(coverage[kind])
		for _, raw := range asList(part["packages"]) {
			pkg, _ := raw.(string)
			if pkg == "" {
				continue
			}
			if !c.coveragePackageExists(kind, pkg) {
				c.rulef("quality-refs", "%s: coverage.%s.packages %s does not exist", rel, kind, pkg)
			}
		}
	}
}

func (c *Checker) coveragePackageExists(kind, pkg string) bool {
	if kind == "api" {
		info, err := os.Stat(filepath.Join(c.root, "apps/api", pkg))
		return err == nil && info.IsDir()
	}
	matches, _ := filepath.Glob(filepath.Join(c.root, "apps/web/*", pkg))
	for _, match := range matches {
		if info, err := os.Stat(match); err == nil && info.IsDir() {
			return true
		}
	}
	return false
}

// contractIDs 读取 contracts/index.yaml 的契约 ID 集合。
func (c *Checker) contractIDs() map[string]bool {
	out := map[string]bool{}
	doc, err := loadYAML(filepath.Join(c.root, "contracts/index.yaml"))
	if err != nil {
		return out
	}
	for _, raw := range asList(doc["entries"]) {
		item, _ := asMap(raw)
		if id, _ := item["id"].(string); id != "" {
			out[id] = true
		}
	}
	return out
}

// makeCommandTarget 返回 make 命令的首个目标；不是 make 命令或缺少目标时返回空。
func makeCommandTarget(command string) string {
	if !strings.HasPrefix(command, "make ") {
		return ""
	}
	fields := strings.Fields(command)
	if len(fields) < 2 {
		return ""
	}
	return fields[1]
}

func (c *Checker) scenarioIDs() map[string]bool {
	out := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(c.root, "contracts/features/scenarios/http/*.yaml"))
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

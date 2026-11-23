// observability.go 校验日志字段与指标标签。
package check

import (
	"path"
	"path/filepath"
	"regexp"
	"strconv"
)

func (c *Checker) checkLogFields() {
	path := filepath.Join(c.root, "contracts/platform/observability/log_fields.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	seen := map[string]bool{}
	allowed := map[string]bool{"http": true, "job": true, "worker": true, "cli": true, "test": true}
	neverSeen := map[string]bool{}
	neverPatternRe := regexp.MustCompile(`^(\*_[a-z][a-z0-9_]*|[a-z][a-z0-9_]*)$`)
	for i, raw := range asList(doc["never_log"]) {
		name, _ := raw.(string)
		if name == "" {
			continue
		}
		if !neverPatternRe.MatchString(name) {
			c.rulef("log-field-refs", "%s: never_log[%d] %s must be snake_case or *suffix", rel, i, name)
		}
		if neverSeen[name] {
			c.rulef("log-field-refs", "%s: never_log[%d] duplicate %s", rel, i, name)
		}
		neverSeen[name] = true
	}
	neverLog := neverLogPatterns(doc)
	if len(neverLog) > 0 {
		files, _ := filepath.Glob(filepath.Join(c.root, "contracts/data/tables/*.yaml"))
		for _, file := range files {
			if filepath.Base(file) == "_base.yaml" {
				continue
			}
			table, err := loadYAML(file)
			if err != nil {
				continue
			}
			tableID, _ := table["id"].(string)
			for _, rawCol := range asList(table["columns"]) {
				col, _ := asMap(rawCol)
				sensitivity, _ := col["sensitivity"].(string)
				if sensitivity != "pii" && sensitivity != "secret" {
					continue
				}
				name, _ := col["name"].(string)
				if name != "" && !matchesNeverLog(name, neverLog) {
					c.rulef("log-field-refs", "%s: %s column %s (%s) is not covered by never_log", rel, tableID, name, sensitivity)
				}
			}
		}
	}
	for i, raw := range asList(doc["fields"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		if seen[name] {
			c.rulef("log-field-refs", "%s: fields[%d] duplicate %s", rel, i, name)
		}
		seen[name] = true
		for _, rawScope := range asList(item["required_for"]) {
			scope, _ := rawScope.(string)
			if scope != "" && !allowed[scope] {
				c.rulef("log-field-refs", "%s: fields[%d] unknown required_for %s", rel, i, scope)
			}
		}
	}
}

func (c *Checker) checkMetrics() {
	path := filepath.Join(c.root, "contracts/platform/observability/metrics.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	labelRules, _ := asMap(doc["label_rules"])
	forbidden := map[string]bool{}
	for _, raw := range asList(labelRules["forbidden_labels"]) {
		if name, _ := raw.(string); name != "" {
			forbidden[name] = true
		}
	}
	seen := map[string]bool{}
	for i, raw := range asList(doc["metrics"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		where := rel + ".metrics[" + strconv.Itoa(i) + "]"
		if seen[name] {
			c.rulef("metric-shape", "%s: duplicate metric %s", where, name)
		}
		seen[name] = true
		if kind, _ := item["kind"].(string); kind != "" && kind != "counter" && kind != "gauge" && kind != "histogram" {
			c.rulef("metric-shape", "%s: unknown kind %s", where, kind)
		}
		for _, rawLabel := range asList(item["labels"]) {
			label, _ := rawLabel.(string)
			if forbidden[label] {
				c.rulef("metric-shape", "%s: forbidden label %s", where, label)
			}
		}
	}
}

func neverLogPatterns(doc map[string]any) []string {
	out := make([]string, 0, len(asList(doc["never_log"])))
	for _, raw := range asList(doc["never_log"]) {
		if name, _ := raw.(string); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func matchesNeverLog(name string, patterns []string) bool {
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, name); ok {
			return true
		}
	}
	return false
}

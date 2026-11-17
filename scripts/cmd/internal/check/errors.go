// errors.go 校验 contracts/platform/errors 下的错误码文件与基础设施基线。
package check

import (
	"path/filepath"
	"strings"
)

func (c *Checker) checkErrorFiles() {
	files := glob(c.root, "contracts/platform/errors/*.yaml")
	if len(files) == 0 {
		c.rulef("schema.load", "contracts/platform/errors: no files")
		return
	}
	baseDoc, err := loadYAML(filepath.Join(c.root, "contracts/platform/errors/_base.yaml"))
	if err != nil {
		c.rulef("schema.load", "contracts/platform/errors/_base.yaml: %v", err)
		return
	}
	naming, _ := asMap(baseDoc["naming"])
	maxTotal, _ := toInt(naming["max_total"])
	order := stringList(baseDoc["order"])

	seen := map[string]string{}
	fileSet := map[string]bool{}
	baseline := map[string]bool{}
	total := 0
	for _, file := range files {
		rel := relPath(c.root, file)
		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		doc, err := loadYAML(file)
		if err != nil {
			c.rulef("schema.load", "%s: %v", rel, err)
			continue
		}
		wantID := "platform.errors." + strings.TrimPrefix(stem, "_")
		if id, _ := doc["id"].(string); id != wantID {
			c.rulef("schema.id-from-file", "%s: id must be %s, got %q", rel, wantID, id)
		}
		if s, _ := doc["summary"].(string); strings.TrimSpace(s) == "" {
			c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
		}
		switch stem {
		case "_base":
			continue
		case "data_access":
			for _, raw := range asList(doc["common_errors"]) {
				code, _ := raw.(string)
				if code == "" {
					c.rulef("schema.non-empty", "%s: common_errors entries must not be empty", rel)
					continue
				}
				if baseline[code] {
					c.rulef("schema.unique", "%s: duplicate common error %s", rel, code)
				}
				baseline[code] = true
			}
			continue
		}
		errs := asList(doc["errors"])
		if len(errs) == 0 {
			c.rulef("schema.non-empty", "%s: errors must not be empty", rel)
			continue
		}
		fileSet[stem] = true
		for i, raw := range errs {
			m, _ := asMap(raw)
			code, _ := m["code"].(string)
			if code == "" {
				c.rulef("schema.required", "%s: errors[%d]: missing code", rel, i)
				continue
			}
			if prev, ok := seen[code]; ok {
				c.rulef("schema.unique", "%s: duplicate error code %s (also in %s)", rel, code, prev)
			} else {
				seen[code] = rel
			}
			status, ok := toInt(m["status"])
			if !ok || status < 100 || status > 599 {
				c.rulef("schema.limit", "%s: errors[%d] (%s): status must be 100-599", rel, i, code)
			}
			if s, _ := m["i18n"].(string); strings.TrimSpace(s) == "" {
				c.rulef("schema.non-empty", "%s: errors[%d] (%s): i18n must not be empty", rel, i, code)
			}
			if s, _ := m["message"].(string); strings.TrimSpace(s) == "" {
				c.rulef("schema.non-empty", "%s: errors[%d] (%s): message must not be empty", rel, i, code)
			}
			total++
		}
	}
	if maxTotal > 0 && total > maxTotal {
		c.rulef("schema.limit", "contracts/platform/errors: %d errors exceed naming.max_total %d", total, maxTotal)
	}
	for _, stem := range order {
		if !fileSet[stem] {
			c.rulef("schema.ref", "contracts/platform/errors/_base.yaml: order references unknown error file %s", stem)
		}
	}
	for stem := range fileSet {
		found := false
		for _, name := range order {
			if name == stem {
				found = true
				break
			}
		}
		if !found {
			c.rulef("schema.ref", "contracts/platform/errors/%s.yaml: not listed in _base.yaml order", stem)
		}
	}
	for code := range baseline {
		if seen[strings.TrimPrefix(code, "platform.errors.")] == "" {
			c.rulef("schema.ref", "contracts/platform/errors/data_access.yaml: unknown common error %s", code)
		}
	}
}

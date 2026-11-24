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
	schema, err := c.loadSchemaByType("platform.errors")
	if err != nil {
		c.rulef("schema.load", "load platform.errors schema: %v", err)
		return
	}
	c.valueTypes = schema.ValueTypes
	c.checkRules(schema)
	baseDoc, err := loadYAML(filepath.Join(c.root, "contracts/platform/errors/_base.yaml"))
	if err != nil {
		c.rulef("schema.load", "contracts/platform/errors/_base.yaml: %v", err)
		return
	}
	naming, _ := asMap(baseDoc["naming"])
	if naming == nil {
		c.rulef("schema.required", "contracts/platform/errors/_base.yaml: naming is required")
	}
	maxTotal, _ := toInt(naming["max_total"])
	order := stringList(baseDoc["order"])
	if len(order) == 0 {
		c.rulef("schema.required", "contracts/platform/errors/_base.yaml: order is required")
	}

	bizDoc, _ := asMap(baseDoc["biz_code"])
	if bizDoc == nil {
		c.rulef("schema.required", "contracts/platform/errors/_base.yaml: biz_code is required")
	}
	bizSuccess, _ := toInt(bizDoc["success_code"])
	bizRanges := map[string][2]int{}
	ranges, _ := asMap(bizDoc["ranges"])
	for stem, raw := range ranges {
		bounds := asList(raw)
		if len(bounds) != 2 {
			c.rulef("schema.limit", "contracts/platform/errors/_base.yaml: biz_code.ranges.%s must be [min, max]", stem)
			continue
		}
		min, okMin := toInt(bounds[0])
		max, okMax := toInt(bounds[1])
		if !okMin || !okMax || min <= bizSuccess || max < min {
			c.rulef("schema.limit", "contracts/platform/errors/_base.yaml: biz_code.ranges.%s must be increasing integers above success code %d", stem, bizSuccess)
			continue
		}
		bizRanges[stem] = [2]int{min, max}
	}

	seen := map[string]string{}
	seenBiz := map[int]string{}
	c.errorStatus = map[string]int{}
	c.errorBizCode = map[string]int{}
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
		c.validateDoc(doc, schema.Fields, rel)
		c.checkFieldOrder(rel, schema.Fields, orderRules{})
		c.checkStability(doc, rel)
		if raw, ok := doc["summary"]; ok {
			if s, _ := raw.(string); strings.TrimSpace(s) == "" {
				c.rulef("schema.non-empty", "%s: summary must not be empty", rel)
			}
		}
		wantID := "platform.errors." + strings.TrimPrefix(stem, "_")
		if id, _ := doc["id"].(string); id != wantID {
			c.rulef("schema.id-from-file", "%s: id must be %s, got %q", rel, wantID, id)
		}
		switch stem {
		case "_base":
			continue
		case "data_access":
			common := asList(doc["common_errors"])
			if len(common) == 0 {
				c.rulef("schema.required", "%s: common_errors must not be empty", rel)
				continue
			}
			for _, raw := range common {
				code, _ := raw.(string)
				if code == "" {
					continue
				}
				baseline[code] = true
			}
			continue
		}
		errs := asList(doc["errors"])
		if len(errs) == 0 {
			c.rulef("schema.required", "%s: errors must not be empty", rel)
			continue
		}
		fileSet[stem] = true
		fileRange, hasRange := bizRanges[stem]
		if !hasRange {
			c.rulef("schema.ref", "%s: biz_code.ranges.%s is missing in _base.yaml", rel, stem)
		}
		for i, raw := range errs {
			m, _ := asMap(raw)
			code, _ := m["code"].(string)
			if code == "" {
				continue
			}
			if prev, ok := seen[code]; ok {
				if prev != rel {
					c.rulef("schema.unique", "%s: duplicate error code %s (also in %s)", rel, code, prev)
				}
			} else {
				seen[code] = rel
			}
			if status, ok := toInt(m["status"]); ok {
				c.errorStatus[code] = status
			}
			if raw, ok := m["i18n"]; ok {
				if s, _ := raw.(string); strings.TrimSpace(s) == "" {
					c.rulef("schema.non-empty", "%s: errors[%d] (%s): i18n must not be empty", rel, i, code)
				}
			}
			if raw, ok := m["message"]; ok {
				if s, _ := raw.(string); strings.TrimSpace(s) == "" {
					c.rulef("schema.non-empty", "%s: errors[%d] (%s): message must not be empty", rel, i, code)
				}
			}
			bizCode, hasBiz := toInt(m["biz_code"])
			if hasBiz {
				c.errorBizCode[code] = bizCode
			}
			switch {
			case !hasBiz:
				continue
			case bizCode <= bizSuccess:
				c.rulef("schema.limit", "%s: errors[%d] (%s): biz_code %d must be greater than success code %d", rel, i, code, bizCode, bizSuccess)
			case hasRange && (bizCode < fileRange[0] || bizCode > fileRange[1]):
				c.rulef("schema.limit", "%s: errors[%d] (%s): biz_code %d must be in [%d, %d]", rel, i, code, bizCode, fileRange[0], fileRange[1])
			default:
				if prev, ok := seenBiz[bizCode]; ok {
					c.rulef("schema.unique", "%s: duplicate biz_code %d (also in %s)", rel, bizCode, prev)
				} else {
					seenBiz[bizCode] = rel
				}
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
	if bizSuccess != 0 {
		c.rulef("schema.limit", "contracts/platform/errors/_base.yaml: biz_code.success_code must be 0")
	}
	for stem := range bizRanges {
		if stem == "data_access" {
			continue
		}
		found := false
		for _, name := range order {
			if name == stem {
				found = true
				break
			}
		}
		if !found {
			c.rulef("schema.ref", "contracts/platform/errors/_base.yaml: biz_code.ranges.%s has no error file in order", stem)
		}
	}
}

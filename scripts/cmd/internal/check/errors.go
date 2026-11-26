// errors.go 校验 contracts/platform/errors 下的错误码文件与基础设施基线。
package check

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// checkErrorFilePlacement 约束字段只出现在固定文件：
// _base 写全局约定，data_access 写基础设施基线，域文件只写 errors。
func (c *Checker) checkErrorFilePlacement(rel, stem string, doc map[string]any) {
	has := func(key string) bool { _, ok := doc[key]; return ok }
	baseOnly := []string{"naming", "biz_code", "order", "mapping_rules"}
	if stem == "_base" {
		for _, key := range []string{"errors", "common_errors"} {
			if has(key) {
				c.rulef("error-file-placement", "%s: _base.yaml must not declare %s", rel, key)
			}
		}
		return
	}
	for _, key := range baseOnly {
		if has(key) {
			c.rulef("error-file-placement", "%s: %s is only allowed in _base.yaml", rel, key)
		}
	}
	if stem == "data_access" {
		if has("errors") {
			c.rulef("error-file-placement", "%s: data_access.yaml must use common_errors, not errors", rel)
		}
		return
	}
	if has("common_errors") {
		c.rulef("error-file-placement", "%s: common_errors is only allowed in data_access.yaml", rel)
	}
}

// checkErrorI18n 校验 i18n 由错误码派生：errors.<code 小写，首个 _ 换成 .>。
func (c *Checker) checkErrorI18n(rel string, i int, m map[string]any) {
	code, _ := m["code"].(string)
	i18n, _ := m["i18n"].(string)
	if code == "" || i18n == "" {
		return
	}
	want := "errors." + strings.Replace(strings.ToLower(code), "_", ".", 1)
	if i18n != want {
		c.rulef("error-i18n-derivation", "%s: errors[%d] (%s): i18n must be %s, got %s", rel, i, code, want, i18n)
	}
}

// checkErrorLocales 校验 i18n key 存在于 project.defaults.locale 对应的前端资源文件。
func (c *Checker) checkErrorLocales(expected map[string]string) {
	if len(expected) == 0 {
		return
	}
	if _, err := os.Stat(filepath.Join(c.root, "Makefile")); err != nil {
		return // 只拷贝 contracts/ 的局部夹具没有前端资源
	}
	project, err := loadYAML(filepath.Join(c.root, "contracts/project.yaml"))
	if err != nil {
		c.rulef("error-i18n-locale", "contracts/project.yaml: %v", err)
		return
	}
	defaults, _ := asMap(project["defaults"])
	defaultLocale, _ := defaults["locale"].(string)
	if defaultLocale == "" {
		c.rulef("schema.required", "contracts/project.yaml: defaults.locale is required")
		return
	}
	locales := stringList(defaults["locales"])
	if len(locales) == 0 {
		locales = []string{defaultLocale}
	}
	hasDefault := false
	for _, locale := range locales {
		if locale == defaultLocale {
			hasDefault = true
		}
	}
	if !hasDefault {
		c.rulef("error-i18n-locale", "contracts/project.yaml: defaults.locales must include %s", defaultLocale)
		locales = append(locales, defaultLocale)
	}
	for _, locale := range locales {
		if locale != "" {
			c.checkErrorLocaleFile(locale, locale == defaultLocale, expected)
		}
	}
}

// checkErrorLocaleFile 校验单个 locale 文件：key 集合一致；默认 locale 值等于 message，其他 locale 非空。
func (c *Checker) checkErrorLocaleFile(locale string, isDefault bool, expected map[string]string) {
	path := filepath.Join(c.root, "apps/web/admin/src/shared/locales", locale+".json")
	b, err := os.ReadFile(path)
	if err != nil {
		c.rulef("error-i18n-locale", "%s: locale catalog missing", relPath(c.root, path))
		return
	}
	var doc any
	if err := json.Unmarshal(b, &doc); err != nil {
		c.rulef("error-i18n-locale", "%s: %v", relPath(c.root, path), err)
		return
	}
	present := map[string]string{}
	flattenLocale(doc, "", present)
	for key, want := range expected {
		got, ok := present[key]
		if !ok {
			c.rulef("error-i18n-locale", "%s: missing i18n key %s", relPath(c.root, path), key)
			continue
		}
		if isDefault {
			if got != want {
				c.rulef("error-i18n-locale", "%s: i18n message mismatch for %s: want %q, got %q", relPath(c.root, path), key, want, got)
			}
			continue
		}
		if got == "" {
			c.rulef("error-i18n-locale", "%s: empty i18n value for %s", relPath(c.root, path), key)
		}
	}
	for key := range present {
		if _, ok := expected[key]; !ok {
			c.rulef("error-i18n-locale", "%s: unknown i18n key %s", relPath(c.root, path), key)
		}
	}
}

func flattenLocale(v any, prefix string, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for key, child := range x {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenLocale(child, next, out)
		}
	default:
		if prefix != "" {
			if s, ok := x.(string); ok {
				out[prefix] = s
			} else {
				out[prefix] = ""
			}
		}
	}
}

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
	commonPrefix, _ := naming["common_prefix"].(string)
	mappingStatuses := map[int]bool{}
	for _, raw := range asList(baseDoc["mapping_rules"]) {
		item, _ := asMap(raw)
		if status, ok := toInt(item["status"]); ok {
			mappingStatuses[status] = true
		}
	}
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

	i18nMessages := map[string]string{}
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
		c.checkErrorFilePlacement(rel, stem, doc)
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
				if len(mappingStatuses) > 0 && !mappingStatuses[status] {
					c.rulef("error-naming", "%s: errors[%d] (%s): status %d is not in mapping_rules", rel, i, code, status)
				}
			}
			if stem == "common" && commonPrefix != "" && !strings.HasPrefix(code, commonPrefix) {
				c.rulef("error-naming", "%s: errors[%d] (%s): common error code must start with %s", rel, i, code, commonPrefix)
			}
			c.checkErrorI18n(rel, i, m)
			if key, _ := m["i18n"].(string); key != "" {
				message, _ := m["message"].(string)
				i18nMessages[key] = message
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
	c.checkErrorLocales(i18nMessages)
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

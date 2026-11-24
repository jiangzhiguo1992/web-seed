// analytics.go 校验埋点事件的 HTTP/页面引用与禁止属性。
package check

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var analyticsNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`)
var analyticsPropRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

func (c *Checker) checkAnalytics() {
	path := filepath.Join(c.root, "contracts/platform/analytics/events.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	pages := c.featurePageIDs()
	forbidden := map[string]bool{}
	for _, raw := range asList(doc["forbidden_properties"]) {
		if name, _ := raw.(string); name != "" {
			forbidden[name] = true
		}
	}
	commonSeen := map[string]bool{}
	types := c.analyticsTypes()
	neverLog := []string{}
	if lf, err := loadYAML(filepath.Join(c.root, "contracts/platform/observability/log_fields.yaml")); err == nil {
		neverLog = neverLogPatterns(lf)
	}
	propIndex := map[string]analyticsProp{}
	indexProp := func(where string, prop map[string]any) {
		name, _ := prop["name"].(string)
		if name == "" {
			return
		}
		typ, _ := prop["type"].(string)
		pii, _ := prop["pii"].(bool)
		if existing, ok := propIndex[name]; ok {
			if existing.typ != typ {
				c.rulef("analytics-pii", "%s: property %s type %s conflicts with %s (%s)", where, name, typ, existing.where, existing.typ)
			}
			if existing.pii != pii {
				c.rulef("analytics-pii", "%s: property %s pii %t conflicts with %s (%t)", where, name, pii, existing.where, existing.pii)
			}
		} else {
			propIndex[name] = analyticsProp{typ: typ, pii: pii, where: where}
		}
		if pii && forbidden[strings.ToLower(name)] {
			c.rulef("analytics-pii", "%s: forbidden property %s cannot be pii: true", where, name)
		}
		if len(neverLog) > 0 {
			if pii && !matchesNeverLog(name, neverLog) {
				c.rulef("analytics-pii", "%s: pii property %s must be listed in log_fields.never_log", where, name)
			}
			if !pii && matchesNeverLog(name, neverLog) {
				c.rulef("analytics-pii", "%s: property %s is in log_fields.never_log but declared pii: false", where, name)
			}
		}
	}
	for i, raw := range asList(doc["forbidden_properties"]) {
		name, _ := raw.(string)
		if name == "" {
			continue
		}
		if !analyticsPropRe.MatchString(name) {
			c.rulef("analytics-pii", "%s.forbidden_properties[%d]: %s must be snake_case", rel, i, name)
		}
	}
	for i, raw := range asList(doc["common_properties"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		where := rel + ".common_properties[" + strconv.Itoa(i) + "]"
		if commonSeen[name] {
			c.rulef("analytics-refs", "%s: duplicate common property %s", where, name)
		}
		commonSeen[name] = true
		if forbidden[strings.ToLower(name)] {
			c.rulef("analytics-refs", "%s: common property %s is forbidden", where, name)
		}
		if !analyticsPropRe.MatchString(name) {
			c.rulef("analytics-refs", "%s: common property %s must be snake_case", where, name)
		}
		c.checkAnalyticsPropertyType(where, item, types)
		indexProp(where, item)
	}
	seen := map[string]bool{}
	for i, raw := range asList(doc["events"]) {
		item, _ := asMap(raw)
		name, _ := item["name"].(string)
		where := rel + ".events[" + strconv.Itoa(i) + "]"
		if name == "" {
			continue
		}
		if seen[name] {
			c.rulef("analytics-refs", "%s: duplicate event %s", where, name)
		}
		seen[name] = true
		if !analyticsNameRe.MatchString(name) {
			c.rulef("analytics-refs", "%s: event name %s must match <domain>.<past_tense>", where, name)
		}
		trigger, _ := item["trigger"].(string)
		ref, _ := item["ref"].(string)
		if ref != "" {
			if _, ok := c.httpOps[ref]; !ok {
				c.rulef("analytics-refs", "%s: ref %s is not an HTTP operation", where, ref)
			}
		}
		if trigger == "api_success" && ref == "" {
			c.rulef("analytics-refs", "%s: trigger api_success requires ref", where)
		}
		page, _ := item["page"].(string)
		if page != "" && !pages[page] {
			c.rulef("analytics-refs", "%s: page %s does not exist", where, page)
		}
		if trigger == "mount" && page == "" {
			c.rulef("analytics-refs", "%s: trigger mount requires page", where)
		}
		propSeen := map[string]bool{}
		for j, rawProp := range asList(item["properties"]) {
			prop, _ := asMap(rawProp)
			pname, _ := prop["name"].(string)
			if pname == "" {
				continue
			}
			pwhere := where + ".properties[" + strconv.Itoa(j) + "]"
			if propSeen[pname] {
				c.rulef("analytics-refs", "%s: duplicate property %s", pwhere, pname)
			}
			propSeen[pname] = true
			if forbidden[strings.ToLower(pname)] {
				c.rulef("analytics-refs", "%s: property %s is forbidden", pwhere, pname)
			}
			if !analyticsPropRe.MatchString(pname) {
				c.rulef("analytics-refs", "%s: property %s must be snake_case", pwhere, pname)
			}
			c.checkAnalyticsPropertyType(pwhere, prop, types)
			indexProp(pwhere, prop)
		}
	}
	provider, _ := asMap(doc["provider"])
	if def, _ := provider["default"].(string); def != "" {
		options := stringSet(asList(provider["options"]))
		if len(options) > 0 && !options[def] {
			c.rulef("analytics-refs", "%s: provider.default %s is not in provider.options", rel, def)
		}
	}
}

// analyticsTypes 返回可用的 analytics property 类型。
func (c *Checker) analyticsTypes() map[string]bool {
	out := map[string]bool{}
	if types, err := loadYAML(filepath.Join(c.root, "contracts/_meta/_types.yaml")); err == nil {
		basic, _ := asMap(types["basic_types"])
		for name := range basic {
			out["_types."+name] = true
		}
	}
	if custom, err := loadYAML(filepath.Join(c.root, "contracts/data/types.yaml")); err == nil {
		items, _ := asMap(custom["custom_types"])
		for name := range items {
			out["data.types."+name] = true
		}
	}
	files, _ := filepath.Glob(filepath.Join(c.root, "contracts/data/enums/*.yaml"))
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			continue
		}
		for _, raw := range asList(doc["enums"]) {
			item, _ := asMap(raw)
			if name, _ := item["name"].(string); name != "" {
				out["data.enums."+name] = true
			}
		}
	}
	return out
}

func (c *Checker) checkAnalyticsPropertyType(where string, prop map[string]any, types map[string]bool) {
	typ, _ := prop["type"].(string)
	if typ == "" || len(types) == 0 {
		return
	}
	if typ == "_types.any" || typ == "_types.json" {
		c.rulef("analytics-types", "%s: property type %s must be a scalar type, not an untyped payload", where, typ)
		return
	}
	if !types[typ] {
		c.rulef("analytics-types", "%s: unknown property type %s", where, typ)
	}
}

type analyticsProp struct {
	typ   string
	pii   bool
	where string
}

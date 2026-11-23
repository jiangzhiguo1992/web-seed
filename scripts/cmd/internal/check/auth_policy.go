// auth_policy.go 校验 CSRF 豁免路径与密码最小长度。
package check

import (
	"path/filepath"
	"regexp"
	"strings"
)

var apiVersionSegmentRe = regexp.MustCompile(`^v[0-9]+$`)

func (c *Checker) checkAuthPolicy() {
	path := filepath.Join(c.root, "contracts/platform/security/auth_policy.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	paths := map[string]bool{}
	for _, op := range c.httpOps {
		paths[policyPathSuffix(op.Path)] = true
	}
	csrf, _ := asMap(doc["csrf"])
	for _, raw := range asList(csrf["exempt_paths"]) {
		value, _ := raw.(string)
		suffix := policyPathSuffix(value)
		if suffix != "" && !paths[suffix] {
			c.rulef("auth-policy-paths", "%s: csrf.exempt_paths %s does not match an HTTP operation path", rel, value)
		}
	}
	token, _ := asMap(doc["token"])
	access, _ := asMap(token["access"])
	audiences := map[string]bool{}
	if conventions, err := loadYAML(filepath.Join(c.root, "contracts/platform/http_conventions.yaml")); err == nil {
		for _, raw := range asList(conventions["audiences"]) {
			if name, _ := raw.(string); name != "" {
				audiences[name] = true
			}
		}
	}
	for _, raw := range asList(access["audience"]) {
		audience, _ := raw.(string)
		if audience != "" && len(audiences) > 0 && !audiences[audience] {
			c.rulef("auth-policy-audience", "%s: token.access.audience %s is not in platform.http_conventions.audiences", rel, audience)
		}
	}
	password, _ := asMap(doc["password"])
	minLen, _ := toInt(password["min_length"])
	if minLen > 0 {
		for _, file := range c.featurePageFiles() {
			page, err := loadYAML(file)
			if err != nil {
				continue
			}
			for i, raw := range asList(page["fields"]) {
				field, _ := asMap(raw)
				if name, _ := field["name"].(string); name != "password" {
					continue
				}
				if n, ok := toInt(field["minLength"]); ok && n < minLen {
					c.rulef("auth-policy-password", "%s: fields[%d].minLength %d is below auth_policy.password.min_length %d", relPath(c.root, file), i, n, minLen)
				}
			}
		}
	}
}

// policyPathSuffix 把 /<api_version>/auth/login、/<app.api_version>/auth/login 或 /v1/auth/login 归一为 /auth/login。
func policyPathSuffix(value string) string {
	for _, prefix := range []string{"/<api_version>", "/<app.api_version>"} {
		if strings.HasPrefix(value, prefix) {
			return strings.TrimPrefix(value, prefix)
		}
	}
	parts := strings.Split(strings.TrimPrefix(value, "/"), "/")
	if len(parts) > 1 && apiVersionSegmentRe.MatchString(parts[0]) {
		return "/" + strings.Join(parts[1:], "/")
	}
	return value
}

func (c *Checker) featurePageFiles() []string {
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		return nil
	}
	return glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
}

// auth_policy.go 校验无状态 JWT 策略与密码最小长度。
package check

import (
	"path/filepath"
)

var jwtRequiredClaims = map[string]bool{
	"iss": true, "sub": true, "aud": true,
	"exp": true, "iat": true, "jti": true,
}

func (c *Checker) checkAuthPolicy() {
	path := filepath.Join(c.root, "contracts/platform/security/auth_policy.yaml")
	doc, err := loadYAML(path)
	if err != nil {
		return
	}
	rel := relPath(c.root, path)
	token, _ := asMap(doc["token"])
	if issuer, _ := token["issuer"].(string); issuer == "" {
		c.rulef("auth-policy-jwt", "%s: token.issuer required", rel)
	}
	for _, name := range []string{"access", "refresh"} {
		part, _ := asMap(token[name])
		if format, _ := part["format"].(string); format != "jwt" {
			c.rulef("auth-policy-jwt", "%s: token.%s.format must be jwt, got %q", rel, name, format)
		}
		if pii, _ := part["pii_in_token"].(bool); pii {
			c.rulef("auth-policy-jwt", "%s: token.%s.pii_in_token must be false", rel, name)
		}
		claims := map[string]bool{}
		for _, raw := range asList(part["claims_required"]) {
			if claim, _ := raw.(string); claim != "" {
				claims[claim] = true
			}
		}
		for claim := range jwtRequiredClaims {
			if !claims[claim] {
				c.rulef("auth-policy-jwt", "%s: token.%s.claims_required missing %s", rel, name, claim)
			}
		}
		for claim := range claims {
			if !jwtRequiredClaims[claim] {
				c.rulef("auth-policy-jwt", "%s: token.%s.claims_required has unknown claim %s", rel, name, claim)
			}
		}
	}
	revocation, _ := asMap(token["revocation"])
	strategy, _ := revocation["strategy"].(string)
	if strategy != "none" && strategy != "refresh_token" && strategy != "server" {
		c.rulef("auth-policy-jwt", "%s: token.revocation.strategy must be none/refresh_token/server, got %q", rel, strategy)
	}
	if strategy == "refresh_token" || strategy == "server" {
		persistence, _ := token["persistence"].(string)
		refresh, _ := asMap(token["refresh"])
		table, _ := refresh["table"].(string)
		if persistence == "" || table == "" || persistence != table {
			c.rulef("auth-policy-jwt", "%s: token.persistence and token.refresh.table must be the same non-empty table", rel)
		}
	}

	audiences := map[string]bool{}
	if conventions, err := loadYAML(filepath.Join(c.root, "contracts/platform/http_conventions.yaml")); err == nil {
		for _, raw := range asList(conventions["audiences"]) {
			if name, _ := raw.(string); name != "" {
				audiences[name] = true
			}
		}
	}
	for _, name := range []string{"access", "refresh"} {
		part, _ := asMap(token[name])
		for _, raw := range asList(part["audience"]) {
			audience, _ := raw.(string)
			if audience != "" && len(audiences) > 0 && !audiences[audience] {
				c.rulef("auth-policy-audience", "%s: token.%s.audience %s is not in platform.http_conventions.audiences", rel, name, audience)
			}
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

func (c *Checker) featurePageFiles() []string {
	schema, err := c.loadSchemaByType("feature.pages")
	if err != nil {
		return nil
	}
	return glob(c.root, filepath.Join(schema.Targets.Dir, schema.Targets.Glob))
}

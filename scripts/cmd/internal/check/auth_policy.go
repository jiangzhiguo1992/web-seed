// auth_policy.go 校验无状态 JWT 策略与密码最小长度。
package check

import (
	"encoding/json"
	"os"
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
	c.checkAuthStorage(rel, doc)
	c.checkAuthSecretEnv(rel, doc)

	audiences := map[string]bool{}
	if conventions, err := loadYAML(filepath.Join(c.root, "contracts/platform/http_conventions.yaml")); err == nil {
		for _, raw := range asList(conventions["audiences"]) {
			if name, _ := raw.(string); name != "" {
				audiences[name] = true
			}
		}
	}
	appAudiences := map[string]bool{}
	for _, app := range c.loadAPIApps() {
		if app.Audience != "" {
			appAudiences[app.Audience] = true
		}
	}
	for _, name := range []string{"access", "refresh"} {
		part, _ := asMap(token[name])
		for _, raw := range asList(part["audience"]) {
			audience, _ := raw.(string)
			if audience == "" {
				continue
			}
			if len(audiences) > 0 && !audiences[audience] {
				c.rulef("auth-policy-audience", "%s: token.%s.audience %s is not in platform.http_conventions.audiences", rel, name, audience)
			}
			if len(appAudiences) > 0 && !appAudiences[audience] {
				c.rulef("auth-policy-audience", "%s: token.%s.audience %s is not a project api app audience", rel, name, audience)
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
	c.checkAuthMFA(rel, doc)
}

// checkAuthMFA 校验强制 MFA 的角色引用真实存在；MFA 只在本文件声明。
func (c *Checker) checkAuthMFA(rel string, doc map[string]any) {
	mfa, _ := asMap(doc["mfa"])
	if mfa == nil {
		return
	}
	roles := c.roleIDs()
	for _, raw := range asList(mfa["required_for_roles"]) {
		code, _ := raw.(string)
		if code == "" {
			continue
		}
		if !roles["platform.security.roles."+code] {
			c.rulef("auth-policy-mfa", "%s: mfa.required_for_roles references unknown role %s", rel, code)
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

// checkAuthStorage 校验认证策略引用的表与列真实存在。
func (c *Checker) checkAuthStorage(rel string, doc map[string]any) {
	token, _ := asMap(doc["token"])
	if persistence, _ := token["persistence"].(string); persistence != "" && (c.isBaseTableRef(persistence) || !c.hasTable(persistence)) {
		c.rulef("auth-policy-storage", "%s: token.persistence %s is not a declared physical table", rel, persistence)
	}
	if refresh, _ := asMap(token["refresh"]); refresh != nil {
		if table, _ := refresh["table"].(string); table != "" && (c.isBaseTableRef(table) || !c.hasTable(table)) {
			c.rulef("auth-policy-storage", "%s: token.refresh.table %s is not a declared physical table", rel, table)
		}
	}
	if verification, _ := asMap(doc["verification"]); verification != nil {
		if table, _ := verification["table"].(string); table != "" && (c.isBaseTableRef(table) || !c.hasTable(table)) {
			c.rulef("auth-policy-storage", "%s: verification.table %s is not a declared physical table", rel, table)
		}
	}
	password, _ := asMap(doc["password"])
	ref, _ := password["hash_field"].(string)
	if ref == "" {
		return
	}
	table, col, ok := splitColumnRef(ref)
	if !ok || c.isBaseTableRef(table) || !c.hasTable(table) || !c.hasColumn(table, col) {
		c.rulef("auth-policy-storage", "%s: password.hash_field %s is not a declared table column", rel, ref)
		return
	}
	if meta, ok := c.tableColumns[table][col]; ok && normalizeSensitivity(meta.sensitivity) != sensitivitySecret {
		c.rulef("auth-policy-storage", "%s: password.hash_field %s must have sensitivity: secret", rel, ref)
	}
}

// checkAuthSecretEnv 校验验证码 secret 环境变量已在 platform/config/env.json 登记且标记 secret。
func (c *Checker) checkAuthSecretEnv(rel string, doc map[string]any) {
	verification, _ := asMap(doc["verification"])
	code, _ := asMap(verification["code"])
	name, _ := code["secret_env"].(string)
	if name == "" {
		return
	}
	b, err := os.ReadFile(filepath.Join(c.root, "contracts/platform/config/env.json"))
	if err != nil {
		return
	}
	var schema struct {
		Properties map[string]struct {
			Secret bool `json:"secret"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		return
	}
	prop, ok := schema.Properties[name]
	if !ok {
		c.rulef("auth-policy-storage", "%s: verification.code.secret_env %s is not declared in platform/config/env.json", rel, name)
		return
	}
	if !prop.Secret {
		c.rulef("auth-policy-storage", "%s: verification.code.secret_env %s must set secret: true", rel, name)
	}
}

func (c *Checker) hasTable(id string) bool {
	_, ok := c.tableOwners[id]
	return ok
}

func (c *Checker) hasColumn(table, col string) bool {
	cols := c.tableColumns[table]
	if cols == nil {
		return false
	}
	_, ok := cols[col]
	return ok
}

//go:build rules

// rules_policy_test.go 是 feature.rules policy 规则的可执行断言。
//
// 断言分三层：
//  1. 规则自身结构：implemented_by / on_violation / given / when / then 非空；
//  2. 规则引用的契约对象：实体、表列、枚举值、service、错误码必须真实存在；
//  3. 每条 policy 的关键表列/枚举/do_not 期望（policyExpectations）。
//
// 业务实现层的行为（如登录锁定计数）不在契约 checker 内执行；这里保证规则
// 所依赖的契约对象和关键字段可解析，避免规则文本与契约漂移。
package check

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type policyRule struct {
	ID            string   `yaml:"id"`
	Kind          string   `yaml:"kind"`
	Summary       string   `yaml:"summary"`
	OnViolation   string   `yaml:"on_violation"`
	ImplementedBy []string `yaml:"implemented_by"`
	Given         string   `yaml:"given"`
	When          string   `yaml:"when"`
	Then          string   `yaml:"then"`
}

type policyRuleDoc struct {
	Rules []policyRule `yaml:"rules"`
}

type policyExpectation struct {
	tables map[string][]string
	enums  map[string][]string
	doNot  []string
}

// policyExpectations 只列可执行断言所需的最小契约锚点（表列/枚举/do_not），
// 不复制 _meta/feature.rules.yaml 的 given/when/then 业务语义；语义以规则 YAML 为准。
var policyExpectations = map[string]policyExpectation{
	"feature.rules.account.number_unique": {
		tables: map[string][]string{"data.tables.account": {"number"}},
	},
	"feature.rules.account.self_only": {
		tables: map[string][]string{"data.tables.account": {"user_id"}},
	},
	"feature.rules.account.deleted_cannot_modify": {
		tables: map[string][]string{"data.tables.account": {"deleted_at"}},
	},
	"feature.rules.account.delete_revokes_auth": {
		tables: map[string][]string{
			"data.tables.verification_body": {"status"},
			"data.tables.token":             {"status"},
		},
	},
	"feature.rules.account.activate_requires_pending_or_suspended": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"pending", "suspended"}},
	},
	"feature.rules.account.suspend_requires_active": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"active"}},
	},
	"feature.rules.auth.verification_one_time": {
		tables: map[string][]string{"data.tables.verification_body": {"consumed_at", "status"}},
	},
	"feature.rules.auth.verification_expiry": {
		tables: map[string][]string{"data.tables.verification_body": {"expire_at"}},
	},
	"feature.rules.auth.verification_attempts": {
		tables: map[string][]string{"data.tables.verification_body": {"attempt_count"}},
	},
	"feature.rules.auth.verification_auth_binding": {
		tables: map[string][]string{
			"data.tables.auth_phone":        {"account_id"},
			"data.tables.auth_email":        {"account_id"},
			"data.tables.verification_body": {"account_id"},
		},
	},
	"feature.rules.auth.auth_login_requires_verified": {
		tables: map[string][]string{
			"data.tables.auth_pwd":   {"status"},
			"data.tables.auth_phone": {"status"},
			"data.tables.auth_email": {"status"},
		},
	},
	"feature.rules.auth.auth_login_rejects_pending_account": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"pending"}},
	},
	"feature.rules.auth.auth_login_rejects_suspended_account": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"suspended"}},
	},
	"feature.rules.auth.auth_login_rejects_deleted_account": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"deleted"}},
	},
	"feature.rules.auth.auth_binding_rejects_suspended_account": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"suspended"}},
	},
	"feature.rules.auth.auth_binding_rejects_deleted_account": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"deleted"}},
	},
	"feature.rules.auth.auth_token_requires_active_account": {
		tables: map[string][]string{
			"data.tables.token":   {"status"},
			"data.tables.account": {"status"},
		},
		enums: map[string][]string{"data.enums.account_status": {"active"}},
	},
	"feature.rules.auth.verification_purpose_status": {
		tables: map[string][]string{"data.tables.verification_body": {"purpose"}},
		enums: map[string][]string{
			"data.enums.verification_purpose": {"login", "reset_password", "unbind_phone", "unbind_email", "delete_account"},
			"data.enums.auth_status":          {"verified", "disabled"},
		},
	},
	"feature.rules.auth.verification_auth_change": {
		tables: map[string][]string{
			"data.tables.verification_body": {"auth_id"},
			"data.tables.auth_phone":        {"status"},
			"data.tables.auth_email":        {"status"},
		},
	},
	"feature.rules.auth.verification_resend": {
		tables: map[string][]string{"data.tables.verification_body": {"status", "auth_id", "purpose"}},
		enums:  map[string][]string{"data.enums.verification_status": {"pending", "sent"}},
	},
	"feature.rules.auth.verification_target_normalization": {
		tables: map[string][]string{
			"data.tables.auth_phone": {"number"},
			"data.tables.auth_email": {"username"},
		},
	},
	"feature.rules.auth.verification_no_enumeration": {
		tables: map[string][]string{"data.tables.verification_body": {"auth_id"}},
	},
	"feature.rules.auth.verification_account_active": {
		tables: map[string][]string{"data.tables.account": {"status"}},
		enums:  map[string][]string{"data.enums.account_status": {"deleted", "suspended"}},
	},
	"feature.rules.auth.verification_register_account": {
		tables: map[string][]string{"data.tables.verification_body": {"account_id", "purpose"}},
		enums:  map[string][]string{"data.enums.verification_purpose": {"register"}},
	},
	"feature.rules.auth.verification_success_sets_verified": {
		tables: map[string][]string{
			"data.tables.auth_phone": {"status"},
			"data.tables.auth_email": {"status"},
		},
		enums: map[string][]string{"data.enums.auth_status": {"verified"}},
	},
	"feature.rules.auth.verification_action_recorded": {
		tables: map[string][]string{"data.tables.verification_action": {"result", "account_id", "verification_id"}},
		enums: map[string][]string{"data.enums.verification_result": {
			"success", "mismatch", "expired", "attempts_exceeded", "invalid",
		}},
	},
	"feature.rules.auth.verification_action_append_only": {
		tables: map[string][]string{"data.tables.verification_action": {"id"}},
		doNot:  []string{"追加日志类表不提供更新或软删"},
	},
	"feature.rules.auth.login_lockout": {
		tables: map[string][]string{"data.tables.auth_pwd": {"failed_login_count", "locked_until"}},
	},
	"feature.rules.auth.password_strength": {
		tables: map[string][]string{"data.tables.auth_pwd": {"password_hash"}},
	},
}

type policyWorld struct {
	tableCols map[string]map[string]bool
	entities  map[string]bool
	services  map[string]bool
	errors    map[string]bool
	enums     map[string]map[string]bool
	baseDoNot string
}

var policyRefRe = regexp.MustCompile(`(?:platform\.errors|cap\.services|data\.entities|data\.tables|data\.enums)\.[A-Za-z0-9_.]+`)

func policyRuleFiles() []string {
	files, _ := filepath.Glob("../../../../contracts/features/rules/*.yaml")
	return files
}

func loadPolicyRule(id string) (policyRule, error) {
	for _, file := range policyRuleFiles() {
		b, err := os.ReadFile(file)
		if err != nil {
			return policyRule{}, err
		}
		var doc policyRuleDoc
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return policyRule{}, err
		}
		for _, rule := range doc.Rules {
			if rule.ID == id {
				return rule, nil
			}
		}
	}
	return policyRule{}, fmt.Errorf("policy rule not found")
}

var policyWorldCache *policyWorld

func loadPolicyWorld() (*policyWorld, error) {
	if policyWorldCache != nil {
		return policyWorldCache, nil
	}
	root, err := filepath.Abs("../../../../contracts")
	if err != nil {
		return nil, err
	}
	w := &policyWorld{
		tableCols: map[string]map[string]bool{},
		entities:  map[string]bool{},
		services:  map[string]bool{},
		errors:    map[string]bool{},
		enums:     map[string]map[string]bool{},
	}
	base, err := loadYAML(filepath.Join(root, "data/tables/_base.yaml"))
	if err != nil {
		return nil, err
	}
	baseCols := map[string]bool{}
	for _, raw := range asList(base["columns"]) {
		if m, ok := asMap(raw); ok {
			if name, _ := m["name"].(string); name != "" {
				baseCols[name] = true
			}
		}
	}
	w.baseDoNot = strings.Join(stringList(base["do_not"]), "\n")
	for _, file := range mustGlob(filepath.Join(root, "data/tables/*.yaml")) {
		if filepath.Base(file) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		cols := map[string]bool{}
		for name := range baseCols {
			cols[name] = true
		}
		for _, raw := range asList(doc["columns"]) {
			if m, ok := asMap(raw); ok {
				if name, _ := m["name"].(string); name != "" {
					cols[name] = true
				}
			}
		}
		w.tableCols[id] = cols
	}
	for _, file := range mustGlob(filepath.Join(root, "data/entities/*.yaml")) {
		if filepath.Base(file) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		if id, _ := doc["id"].(string); id != "" {
			w.entities[id] = true
		}
	}
	for _, file := range mustGlob(filepath.Join(root, "capabilities/services/*.yaml")) {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["services"]) {
			if m, ok := asMap(raw); ok {
				if name, _ := m["name"].(string); name != "" {
					w.services[fileID+"."+name] = true
				}
			}
		}
	}
	for _, file := range mustGlob(filepath.Join(root, "platform/errors/*.yaml")) {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["errors"]) {
			if m, ok := asMap(raw); ok {
				if code, _ := m["code"].(string); code != "" {
					w.errors[code] = true
				}
			}
		}
	}
	for _, file := range mustGlob(filepath.Join(root, "data/enums/*.yaml")) {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for _, raw := range asList(doc["enums"]) {
			m, ok := asMap(raw)
			if !ok {
				continue
			}
			name, _ := m["name"].(string)
			if name == "" {
				continue
			}
			values := map[string]bool{}
			for _, v := range asList(m["values"]) {
				if vm, ok := asMap(v); ok {
					if value, _ := vm["value"].(string); value != "" {
						values[value] = true
					}
				}
			}
			w.enums["data.enums."+name] = values
		}
	}
	policyWorldCache = w
	return w, nil
}

func mustGlob(pattern string) []string {
	files, _ := filepath.Glob(pattern)
	return files
}

func (w *policyWorld) hasRef(ref string) bool {
	switch {
	case strings.HasPrefix(ref, "platform.errors."):
		return w.errors[strings.TrimPrefix(ref, "platform.errors.")]
	case strings.HasPrefix(ref, "cap.services."):
		return w.services[ref]
	case strings.HasPrefix(ref, "data.entities."):
		return w.entities[ref]
	case strings.HasPrefix(ref, "data.tables."):
		for table := range w.tableCols {
			if ref == table || strings.HasPrefix(ref, table+".") {
				return true
			}
		}
	case strings.HasPrefix(ref, "data.enums."):
		return w.enums[ref] != nil
	}
	return false
}

func checkPolicyRule(id string) error {
	rule, err := loadPolicyRule(id)
	if err != nil {
		return err
	}
	if len(rule.ImplementedBy) == 0 {
		return fmt.Errorf("implemented_by is empty")
	}
	if rule.OnViolation == "" {
		return fmt.Errorf("on_violation is empty")
	}
	if rule.Given == "" || rule.When == "" || rule.Then == "" {
		return fmt.Errorf("given/when/then must be non-empty")
	}
	return nil
}

func assertPolicy(id string) error {
	rule, err := loadPolicyRule(id)
	if err != nil {
		return err
	}
	if err := checkPolicyRule(id); err != nil {
		return err
	}
	w, err := loadPolicyWorld()
	if err != nil {
		return err
	}
	code := strings.TrimPrefix(rule.OnViolation, "platform.errors.")
	if !w.errors[code] {
		return fmt.Errorf("on_violation %s is not declared in platform/errors", rule.OnViolation)
	}
	for _, svc := range rule.ImplementedBy {
		if !w.services[svc] {
			return fmt.Errorf("implemented_by %s is not a declared service", svc)
		}
	}
	text := strings.Join([]string{rule.Summary, rule.Given, rule.When, rule.Then}, " ")
	for _, ref := range policyRefRe.FindAllString(text, -1) {
		if !w.hasRef(ref) {
			return fmt.Errorf("reference %s does not resolve", ref)
		}
	}
	exp, ok := policyExpectations[id]
	if !ok {
		return fmt.Errorf("no policy expectation registered")
	}
	if len(exp.tables) == 0 && len(exp.enums) == 0 && len(exp.doNot) == 0 {
		return fmt.Errorf("policy expectation has no contract anchor")
	}
	for table, cols := range exp.tables {
		got, ok := w.tableCols[table]
		if !ok {
			return fmt.Errorf("table %s does not exist", table)
		}
		for _, col := range cols {
			if !got[col] {
				return fmt.Errorf("column %s.%s does not exist", table, col)
			}
		}
	}
	for enum, values := range exp.enums {
		got, ok := w.enums[enum]
		if !ok {
			return fmt.Errorf("enum %s does not exist", enum)
		}
		for _, value := range values {
			if !got[value] {
				return fmt.Errorf("enum %s value %s does not exist", enum, value)
			}
		}
	}
	for _, want := range exp.doNot {
		if !strings.Contains(w.baseDoNot, want) {
			return fmt.Errorf("base do_not is missing %q", want)
		}
	}
	return nil
}

// policyAssertions 由 registerPolicyAssertions 填充；实现层可覆盖为业务断言。
var policyAssertions = map[string]func() error{}

func registerPolicyAssertions() {
	for _, file := range policyRuleFiles() {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var doc policyRuleDoc
		if err := yaml.Unmarshal(b, &doc); err != nil {
			continue
		}
		for _, rule := range doc.Rules {
			if rule.Kind != "policy" {
				continue
			}
			id := rule.ID
			if _, exists := policyAssertions[id]; exists {
				continue
			}
			policyAssertions[id] = func() error { return assertPolicy(id) }
		}
	}
}

func TestPolicyExpectationsCoverPolicyRules(t *testing.T) {
	rules := map[string]bool{}
	for _, file := range policyRuleFiles() {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc policyRuleDoc
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		for _, rule := range doc.Rules {
			if rule.Kind == "policy" {
				rules[rule.ID] = true
			}
		}
	}
	for id := range policyExpectations {
		if !rules[id] {
			t.Errorf("policy expectation %s has no policy rule", id)
		}
	}
	for id := range rules {
		if _, ok := policyExpectations[id]; !ok {
			t.Errorf("policy rule %s has no expectation", id)
		}
	}
}

// anchorAliases 把契约锚点映射到规则正文里的中英文短名。
// 每个 policyExpectation 的锚点都必须至少有一个别名出现在规则文本中。
var anchorAliases = map[string][]string{
	"data.tables.account":                             {"账号", "account"},
	"data.tables.account.number":                      {"编号", "number"},
	"data.tables.account.user_id":                     {"用户", "user_id"},
	"data.tables.account.deleted_at":                  {"软删", "deleted_at"},
	"data.tables.account.status":                      {"状态", "status"},
	"data.tables.verification_body":                   {"验证体", "验证码", "verification_body", "验证"},
	"data.tables.verification_body.status":            {"状态", "status", "pending", "sent"},
	"data.tables.verification_body.consumed_at":       {"consumed_at", "消费"},
	"data.tables.verification_body.expire_at":         {"expire_at", "过期"},
	"data.tables.verification_body.attempt_count":     {"attempt_count", "次数"},
	"data.tables.verification_body.account_id":        {"account_id", "账号"},
	"data.tables.verification_body.auth_id":           {"auth_id", "认证"},
	"data.tables.verification_body.purpose":           {"purpose", "用途"},
	"data.tables.token":                               {"令牌", "token"},
	"data.tables.token.status":                        {"状态", "status"},
	"data.tables.auth_phone":                          {"auth_phone", "手机", "phone"},
	"data.tables.auth_phone.account_id":               {"account_id", "账号"},
	"data.tables.auth_phone.status":                   {"状态", "status"},
	"data.tables.auth_phone.number":                   {"number", "phone", "手机"},
	"data.tables.auth_email":                          {"auth_email", "邮箱", "email"},
	"data.tables.auth_email.account_id":               {"account_id", "账号"},
	"data.tables.auth_email.status":                   {"状态", "status"},
	"data.tables.auth_email.username":                 {"username", "email", "邮箱"},
	"data.tables.auth_pwd":                            {"auth_pwd", "密码"},
	"data.tables.auth_pwd.status":                     {"状态", "status"},
	"data.tables.auth_pwd.failed_login_count":         {"failed_login_count", "失败"},
	"data.tables.auth_pwd.locked_until":               {"locked_until", "锁定"},
	"data.tables.auth_pwd.password_hash":              {"password", "密码"},
	"data.tables.verification_action":                 {"verification_action", "动作", "日志"},
	"data.tables.verification_action.result":          {"result", "结果"},
	"data.tables.verification_action.account_id":      {"account_id", "账号"},
	"data.tables.verification_action.verification_id": {"verification_id", "验证", "校验"},
	"data.tables.verification_action.id":              {"动作", "日志"},
	"data.enums.account_status":                       {"account_status", "状态", "pending", "suspended", "active", "deleted"},
	"data.enums.verification_purpose":                 {"purpose", "login", "reset_password", "unbind_phone", "unbind_email", "delete_account", "register"},
	"data.enums.auth_status":                          {"auth_status", "verified", "disabled"},
	"data.enums.verification_status":                  {"status", "pending", "sent", "验证码"},
	"data.enums.verification_result":                  {"result", "success", "mismatch", "expired", "attempts_exceeded", "invalid"},
}

func assertAnchorInText(t *testing.T, id, text, anchor string) {
	t.Helper()
	aliases, ok := anchorAliases[anchor]
	if !ok {
		t.Errorf("policy %s: anchor %s has no alias mapping", id, anchor)
		return
	}
	for _, alias := range aliases {
		if strings.Contains(text, alias) {
			return
		}
	}
	t.Errorf("policy %s: anchor %s (%v) is not mentioned in rule text", id, anchor, aliases)
}

func TestPolicyExpectationsAnchorsAppearInText(t *testing.T) {
	for id, exp := range policyExpectations {
		rule, err := loadPolicyRule(id)
		if err != nil {
			t.Fatal(err)
		}
		text := strings.Join([]string{rule.Summary, rule.Given, rule.When, rule.Then}, " ")
		for table, cols := range exp.tables {
			assertAnchorInText(t, id, text, table)
			for _, col := range cols {
				assertAnchorInText(t, id, text, table+"."+col)
			}
		}
		for enum := range exp.enums {
			assertAnchorInText(t, id, text, enum)
		}
	}
}

func policyAnchorSet() map[string]bool {
	anchors := map[string]bool{}
	for _, exp := range policyExpectations {
		for table, cols := range exp.tables {
			anchors[table] = true
			for _, col := range cols {
				anchors[table+"."+col] = true
			}
		}
		for enum := range exp.enums {
			anchors[enum] = true
		}
	}
	return anchors
}

func TestPolicyAnchorAliasesHaveNoExtraKeys(t *testing.T) {
	anchors := policyAnchorSet()
	for key := range anchorAliases {
		if !anchors[key] {
			t.Errorf("anchorAliases has extra key %s", key)
		}
	}
}

func TestPolicyAnchorAliasesCoverExpectations(t *testing.T) {
	for anchor := range policyAnchorSet() {
		if _, ok := anchorAliases[anchor]; !ok {
			t.Errorf("anchor %s has no alias mapping", anchor)
		}
	}
}

func TestPolicyExpectationsAreAnchored(t *testing.T) {
	for id, exp := range policyExpectations {
		if len(exp.tables) == 0 && len(exp.enums) == 0 && len(exp.doNot) == 0 {
			t.Errorf("policy expectation %s has no contract anchor", id)
		}
	}
}

func TestPolicyRulesImplemented(t *testing.T) {
	registerPolicyAssertions()
	for _, file := range policyRuleFiles() {
		b, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var doc policyRuleDoc
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Fatal(err)
		}
		for _, rule := range doc.Rules {
			if rule.Kind != "policy" {
				continue
			}
			rule := rule
			t.Run(rule.ID, func(t *testing.T) {
				assert, ok := policyAssertions[rule.ID]
				if !ok {
					t.Fatalf("policy rule %s has no behavior assertion", rule.ID)
				}
				if err := assert(); err != nil {
					t.Fatalf("policy rule %s failed: %v", rule.ID, err)
				}
			})
		}
	}
}

// enums_history.go 校验枚举只追加：对比基线版本与当前 data/enums.yaml，
// 禁止删除/改名已发布枚举值，禁止复用或修改已分配的 code。
package check

import (
	"os/exec"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// checkEnumAppendOnly 读取基线版本并比较；仓库无 git 历史时跳过。
func (c *Checker) checkEnumAppendOnly(rel string) {
	baseline := "HEAD"
	if out, err := exec.Command("git", "-C", c.root, "rev-list", "--parents", "-n", "1", "HEAD").Output(); err == nil {
		baseline = enumBaselineRef(string(out))
	}
	out, err := exec.Command("git", "-C", c.root, "show", baseline+":"+rel).Output()
	if err != nil {
		return
	}
	var oldDoc map[string]any
	if err := yaml.Unmarshal(out, &oldDoc); err != nil {
		return
	}
	curDoc, err := loadYAML(filepath.Join(c.root, rel))
	if err != nil {
		return
	}
	c.reportEnumRegressions(oldDoc, curDoc, rel)
}

// enumBaselineRef 选择 append-only 的对比基线：
//   - 普通提交：HEAD 就是当前改动前的版本（pre-commit 场景）；
//   - PR merge ref：HEAD 是平台生成的 merge commit，HEAD^1 才是基线；
//   - 浅克隆缺少父提交时 rev-list 只返回自身，退回 HEAD，避免误报。
func enumBaselineRef(revList string) string {
	if len(strings.Fields(revList)) > 2 {
		return "HEAD^1"
	}
	return "HEAD"
}

type enumValueCode struct {
	value string
	code  int
	has   bool
}

// enumValueCodes 把 enums[].values[] 展开为 id -> value -> code。
func enumValueCodes(doc map[string]any) map[string]map[string]enumValueCode {
	out := map[string]map[string]enumValueCode{}
	for _, raw := range asList(doc["enums"]) {
		m, ok := asMap(raw)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			continue
		}
		values := map[string]enumValueCode{}
		for _, rv := range asList(m["values"]) {
			vm, ok := asMap(rv)
			if !ok {
				continue
			}
			v, _ := vm["value"].(string)
			code, has := toInt(vm["code"])
			values[v] = enumValueCode{value: v, code: code, has: has}
		}
		out[id] = values
	}
	return out
}

func (c *Checker) reportEnumRegressions(oldDoc, curDoc map[string]any, rel string) {
	oldEnums := enumValueCodes(oldDoc)
	curEnums := enumValueCodes(curDoc)
	for id, oldValues := range oldEnums {
		curValues, ok := curEnums[id]
		if !ok {
			c.rulef("enum-append-only", "%s: enum %s was removed", rel, id)
			continue
		}
		for value, old := range oldValues {
			cur, ok := curValues[value]
			if !ok {
				c.rulef("enum-append-only", "%s: %s.%s was removed", rel, id, value)
				continue
			}
			if old.has && (!cur.has || cur.code != old.code) {
				c.rulef("enum-append-only", "%s: %s.%s code changed from %d to %d (codes are append-only)", rel, id, value, old.code, cur.code)
			}
		}
	}
}

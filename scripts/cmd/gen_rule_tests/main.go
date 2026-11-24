// gen_rule_tests 生成规则 → 测试映射，暴露没有直接测试断言的规则。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./gen_rule_tests -root ../..
//	go run ./gen_rule_tests -root ../.. -check
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"github.com/yourorg/web-seed/scripts/cmd/internal/ruletests"
	"gopkg.in/yaml.v3"
)

type row struct {
	Type          string
	Rule          string
	ImplementedBy string
	Asserted      int
	Dynamic       int
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify generated file is up to date")
	maxZero := flag.Int("max-zero", -1, "max rules without direct test refs (-1 disables)")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	content, err := build(abs)
	if err != nil {
		fail(err)
	}
	target := filepath.Join(abs, "docs/contracts/generated/rule-tests.md")
	if *check {
		got, err := os.ReadFile(target)
		if err != nil {
			if os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "gen-rule-tests: %s missing; run make gen-rule-tests\n", target)
			} else {
				fmt.Fprintf(os.Stderr, "gen-rule-tests: %s: %v\n", target, err)
			}
			os.Exit(1)
		}
		if !bytes.Equal(got, content) {
			fmt.Fprintf(os.Stderr, "gen-rule-tests: %s is not up to date; run make gen-rule-tests\n", target)
			os.Exit(1)
		}
		zero := 0
		rows, err := loadRows(abs, knownTests(abs))
		if err != nil {
			fail(err)
		}
		for _, r := range rows {
			if r.Asserted == 0 && r.Dynamic == 0 {
				zero++
			}
		}
		if *maxZero >= 0 && zero > *maxZero {
			fmt.Fprintf(os.Stderr, "gen-rule-tests: %d rules have no test refs (max %d)\n", zero, *maxZero)
			os.Exit(1)
		}
		fmt.Println("rule-tests: OK")
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		fail(err)
	}
	fmt.Println("generated", target)
}

func build(root string) ([]byte, error) {
	known := loadKnownRuleIDs(root)
	testRefs := ruletests.Refs(root, known)
	rows, err := loadRows(root, testRefs)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("# 规则 → 测试映射\n\n")
	buf.WriteString("> 自动生成；运行 `make gen-rule-tests`。`asserted` 是测试里对 rule ID 的字面断言次数；`dynamic` 是表驱动测试文件中的动态断言引用（每文件计 1）；两者都为 0 表示没有测试覆盖。\n\n")
	buf.WriteString("| type | rule | implemented_by | asserted | dynamic |\n")
	buf.WriteString("|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&buf, "| %s | %s | %s | %d | %d |\n", r.Type, r.Rule, r.ImplementedBy, r.Asserted, r.Dynamic)
	}
	return buf.Bytes(), nil
}

// knownTests 返回 rule ID → 测试引用统计。
func knownTests(root string) map[string]ruletests.Ref {
	return ruletests.Refs(root, loadKnownRuleIDs(root))
}

// loadRows 读取全部 _meta rules 并填测试引用。
func loadRows(root string, testRefs map[string]ruletests.Ref) ([]row, error) {
	var rows []row
	files, err := filepath.Glob(filepath.Join(root, "contracts/_meta/*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		var head struct {
			Type string `yaml:"type"`
		}
		if err := yaml.Unmarshal(b, &head); err != nil {
			return nil, err
		}
		if head.Type == "" {
			continue
		}
		s, err := contractspec.LoadSchema(file)
		if err != nil {
			return nil, err
		}
		for _, r := range s.Rules {
			tr := testRefs[r.ID]
			rows = append(rows, row{
				Type:          s.Type,
				Rule:          r.ID,
				ImplementedBy: strings.Join(r.ImplementedBy, ", "),
				Asserted:      tr.Asserted,
				Dynamic:       tr.Dynamic,
			})
		}
	}
	return rows, nil
}

// loadKnownRuleIDs 从生成的 rule_ids_gen.go 读取规则 ID 清单。
func loadKnownRuleIDs(root string) []string {
	path := filepath.Join(root, "scripts/cmd/internal/check/rule_ids_gen.go")
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	re := regexp.MustCompile(`"([^"]+)":\s*true`)
	var ids []string
	for _, m := range re.FindAllStringSubmatch(string(b), -1) {
		ids = append(ids, m[1])
	}
	return ids
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-rule-tests:", err)
	os.Exit(1)
}

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
	"gopkg.in/yaml.v3"
)

type row struct {
	Type          string
	Rule          string
	ImplementedBy string
	Refs          int
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
		if zero := bytes.Count(content, []byte("| 0 |")); *maxZero >= 0 && zero > *maxZero {
			fmt.Fprintf(os.Stderr, "gen-rule-tests: %d rules have no direct test refs (max %d)\n", zero, *maxZero)
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
	testRefs := loadTestRefs(root, known)
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
			rows = append(rows, row{
				Type:          s.Type,
				Rule:          r.ID,
				ImplementedBy: strings.Join(r.ImplementedBy, ", "),
				Refs:          testRefs[r.ID],
			})
		}
	}
	var buf bytes.Buffer
	buf.WriteString("# 规则 → 测试映射\n\n")
	buf.WriteString("> 自动生成；运行 `make gen-rule-tests`。`refs` 是测试文件中直接出现 rule ID 的次数；`0` 表示当前只有实现、没有直接断言。\n\n")
	buf.WriteString("| type | rule | implemented_by | refs |\n")
	buf.WriteString("|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&buf, "| %s | %s | %s | %d |\n", r.Type, r.Rule, r.ImplementedBy, r.Refs)
	}
	return buf.Bytes(), nil
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

// loadTestRefs 统计 check 包测试中每个 rule ID 的直接引用次数。
func loadTestRefs(root string, ids []string) map[string]int {
	out := map[string]int{}
	files, _ := filepath.Glob(filepath.Join(root, "scripts/cmd/internal/check/*_test.go"))
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(b)
		for _, id := range ids {
			if strings.Contains(content, id) {
				out[id]++
			}
		}
	}
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-rule-tests:", err)
	os.Exit(1)
}

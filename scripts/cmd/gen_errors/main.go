// generrors 从 contracts/platform/errors/*.yaml 生成人读错误码视图。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./gen_errors -root ../..
//	go run ./gen_errors -root ../.. -check
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type errorEntry struct {
	Code      string `yaml:"code"`
	Status    int    `yaml:"status"`
	Retryable bool   `yaml:"retryable"`
	I18n      string `yaml:"i18n"`
	Message   string `yaml:"message"`
}

type errorFile struct {
	ID           string       `yaml:"id"`
	Summary      string       `yaml:"summary"`
	Errors       []errorEntry `yaml:"errors"`
	CommonErrors []string     `yaml:"common_errors"`
	Order        []string     `yaml:"order"`
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify errors.md is up to date")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	dir := filepath.Join(abs, "contracts/platform/errors")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		fail(err)
	}
	if len(files) == 0 {
		fail(fmt.Errorf("%s: no error files", dir))
	}
	sort.Strings(files)
	docs := map[string]errorFile{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			fail(err)
		}
		var doc errorFile
		if err := yaml.Unmarshal(b, &doc); err != nil {
			fail(fmt.Errorf("%s: %w", file, err))
		}
		stem := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		docs[stem] = doc
	}
	base, ok := docs["_base"]
	if !ok {
		fail(fmt.Errorf("%s/_base.yaml is missing", dir))
	}
	var entries []errorEntry
	used := map[string]bool{}
	for _, stem := range base.Order {
		doc, ok := docs[stem]
		if !ok || len(doc.Errors) == 0 {
			fail(fmt.Errorf("_base.yaml order references missing/empty error file %s", stem))
		}
		entries = append(entries, doc.Errors...)
		used[stem] = true
	}
	for stem, doc := range docs {
		if len(doc.Errors) > 0 && !used[stem] {
			fail(fmt.Errorf("%s.yaml is not listed in _base.yaml order", stem))
		}
	}
	dataAccess := docs["data_access"].CommonErrors
	out := render(entries, dataAccess)
	dst := filepath.Join(abs, "docs/contracts/generated/errors.md")
	if *check {
		old, err := os.ReadFile(dst)
		if err != nil {
			fail(err)
		}
		if string(old) != out {
			fail(fmt.Errorf("%s is stale; run make gen-errors", dst))
		}
		fmt.Println("errors.md: OK")
		return
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(dst, []byte(out), 0o644); err != nil {
		fail(err)
	}
	fmt.Println("generated", dst)
}

func render(entries []errorEntry, dataAccess []string) string {
	var b strings.Builder
	b.WriteString("# 错误码\n\n")
	b.WriteString("> 本文件由 `contracts/platform/errors/*.yaml` 生成，禁止手改。生成命令：`make gen-errors`。\n\n")
	b.WriteString("**ID**: `platform.errors`\n\n")
	b.WriteString("| code | status | retryable | i18n | message |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, e := range entries {
		fmt.Fprintf(&b, "| `%s` | %d | %t | `%s` | %s |\n", e.Code, e.Status, e.Retryable, e.I18n, escape(e.Message))
	}
	if len(dataAccess) > 0 {
		b.WriteString("\n## 数据访问基线\n\n")
		b.WriteString("以下错误由 repository / transaction 层统一映射，不写入每个 query 的 errors：\n\n")
		for _, code := range dataAccess {
			fmt.Fprintf(&b, "- `%s`\n", code)
		}
	}
	return b.String()
}

func escape(s string) string {
	s = strings.ReplaceAll(s, "|", `\|`)
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-errors:", err)
	os.Exit(1)
}

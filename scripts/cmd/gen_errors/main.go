// generrors 从 platform/errors.yaml 生成人读错误码视图。
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

type errorsDoc struct {
	ID      string       `yaml:"id"`
	Summary string       `yaml:"summary"`
	Errors  []errorEntry `yaml:"errors"`
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify errors.md is up to date")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	src := filepath.Join(abs, "contracts/platform/errors.yaml")
	b, err := os.ReadFile(src)
	if err != nil {
		fail(err)
	}
	var doc errorsDoc
	if err := yaml.Unmarshal(b, &doc); err != nil {
		fail(fmt.Errorf("%s: %w", src, err))
	}
	if doc.ID != "platform.errors" {
		fail(fmt.Errorf("%s: id must be platform.errors, got %q", src, doc.ID))
	}
	out := render(doc)
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

func render(doc errorsDoc) string {
	var b strings.Builder
	b.WriteString("# 错误码\n\n")
	b.WriteString("> 本文件由 `contracts/platform/errors.yaml` 生成，禁止手改。生成命令：`make gen-errors`。\n\n")
	fmt.Fprintf(&b, "**ID**: `%s`\n\n", doc.ID)
	b.WriteString("| code | status | retryable | i18n | message |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, e := range doc.Errors {
		fmt.Fprintf(&b, "| `%s` | %d | %t | `%s` | %s |\n", e.Code, e.Status, e.Retryable, e.I18n, escape(e.Message))
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

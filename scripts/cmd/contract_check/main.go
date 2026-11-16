// contract_check 是契约校验命令入口。
//
// 具体校验逻辑在 internal/check，命令层只负责 flag、root 和输出。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yourorg/web-seed/scripts/cmd/internal/check"
	"github.com/yourorg/web-seed/scripts/cmd/internal/spec"
	"gopkg.in/yaml.v3"
)

func main() {
	root := flag.String("root", "../..", "repo root")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "contract-check:", err)
		os.Exit(1)
	}
	errs := check.Run(abs)
	errs = append(errs, validateGeneratedSpecs(abs)...)
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "contract-check: FAILED (%d error(s))\n", len(errs))
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}
	fmt.Println("contract-check: OK")
}

func validateGeneratedSpecs(root string) []string {
	files, err := filepath.Glob(filepath.Join(root, "contracts/generated/openapi/*.yaml"))
	if err != nil {
		return []string{fmt.Sprintf("[schema.openapi] %v", err)}
	}
	var errs []string
	for _, file := range files {
		rel := strings.TrimPrefix(file, root+string(filepath.Separator))
		b, err := os.ReadFile(file)
		if err != nil {
			errs = append(errs, fmt.Sprintf("[schema.openapi] %s: %v", rel, err))
			continue
		}
		var doc map[string]any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			errs = append(errs, fmt.Sprintf("[schema.openapi] %s: %v", rel, err))
			continue
		}
		if err := spec.Validate(doc); err != nil {
			errs = append(errs, fmt.Sprintf("[schema.openapi] %s: %v", rel, err))
		}
	}
	return errs
}

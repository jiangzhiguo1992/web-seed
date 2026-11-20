// audit_types 输出契约 type 词汇审计报告；非法值以非零退出码失败。
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"

	"github.com/yourorg/web-seed/scripts/cmd/internal/check"
)

func main() {
	root := flag.String("root", ".", "repository root")
	flag.Parse()
	report, err := check.AuditTypeVocabulary(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-types: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("audit-types: %d type keys, %d invalid, %d exempt files\n", report.Total, len(report.Invalid), report.Exempt)
	values := make([]string, 0, len(report.Values))
	for value := range report.Values {
		values = append(values, value)
	}
	sort.Slice(values, func(i, j int) bool {
		if report.Values[values[i]] != report.Values[values[j]] {
			return report.Values[values[i]] > report.Values[values[j]]
		}
		return values[i] < values[j]
	})
	for _, value := range values {
		fmt.Printf("  %4d  %s\n", report.Values[value], value)
	}
	if len(report.Invalid) > 0 {
		for _, err := range report.Invalid {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
	fmt.Println("audit-types: OK")
}

// contract_check 是契约校验命令入口。
//
// 具体校验逻辑在 internal/check，命令层只负责 flag、root 和输出。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourorg/web-seed/scripts/cmd/internal/check"
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
	if len(errs) > 0 {
		fmt.Fprintf(os.Stderr, "contract-check: FAILED (%d error(s))\n", len(errs))
		for _, e := range errs {
			fmt.Fprintln(os.Stderr, "  -", e)
		}
		os.Exit(1)
	}
	fmt.Println("contract-check: OK")
}

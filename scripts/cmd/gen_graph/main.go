// gengraph 计算并写回 table/entity 的 used_by。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./gen_graph -root ../..
//	go run ./gen_graph -root ../.. -check
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourorg/web-seed/scripts/cmd/internal/graph"
)

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify used_by is up to date")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	snap, err := graph.Compute(abs)
	if err != nil {
		fail(err)
	}
	if *check {
		drift, err := snap.Check(abs)
		if err != nil {
			fail(err)
		}
		if len(drift) > 0 {
			for _, d := range drift {
				fmt.Fprintln(os.Stderr, "used_by drift:", d)
			}
			os.Exit(1)
		}
		fmt.Println("used_by: OK")
		return
	}
	changed, err := snap.Write(abs)
	if err != nil {
		fail(err)
	}
	if len(changed) == 0 {
		fmt.Println("used_by: up to date")
		return
	}
	for _, f := range changed {
		fmt.Println("updated", f)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-graph:", err)
	os.Exit(1)
}

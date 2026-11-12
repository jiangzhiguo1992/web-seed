// contractnew 从 _meta 字段规范渲染契约骨架。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./contract_new -root ../.. -type data.tables -name note -module notes -summary "备注表"
//	go run ./contract_new -root ../.. -type data.tables -name balance -module accounts -column-type decimal
//	go run ./contract_new -root ../.. -type data.enums  -name note_status
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
)

func main() {
	root := flag.String("root", "../..", "repo root")
	typ := flag.String("type", "", "contract type: data.tables | data.enums")
	name := flag.String("name", "", "snake_case name")
	module := flag.String("module", "", "owner_module (default: name)")
	summary := flag.String("summary", "", "summary (default: name)")
	out := flag.String("out", "", "output file (data.tables); empty prints to stdout")
	columnType := flag.String("column-type", "", "first column type family (default: schema default)")
	flag.Parse()

	if *typ == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "usage: contract_new -type data.tables|data.enums -name <name> [-module <module>] [-summary <text>] [-column-type <family>] [-out <file>]")
		os.Exit(2)
	}
	if *module == "" {
		*module = *name
	}
	if *summary == "" {
		*summary = *name
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	schema, err := contractspec.LoadSchema(filepath.Join(abs, "contracts/_meta", *typ+".yaml"))
	if err != nil {
		fail(err)
	}

	switch *typ {
	case "data.tables":
		b, err := contractspec.RenderTableWithColumnType(schema, *name, *module, *summary, *columnType)
		if err != nil {
			fail(err)
		}
		if *out == "" {
			os.Stdout.Write(b)
			return
		}
		if _, err := os.Stat(*out); err == nil {
			fail(fmt.Errorf("already exists: %s", *out))
		}
		if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(*out, b, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("created %s\n", *out)
		fmt.Printf("next: replace TODO; register in contracts/index.yaml; add data.enums.%s_status if missing; make contract-check; make gen\n", *name)

	case "data.enums":
		b, err := contractspec.RenderEnumItem(schema, *name)
		if err != nil {
			fail(err)
		}
		os.Stdout.Write(b)

	default:
		fail(fmt.Errorf("unsupported type: %s", *typ))
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "contract-new:", err)
	os.Exit(1)
}

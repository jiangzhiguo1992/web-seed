// contractnew 从 _meta 字段规范渲染契约骨架。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./contract_new -root ../.. -type data.tables -name note -module notes -summary "备注表"
//	go run ./contract_new -root ../.. -type data.tables -name balance -module account -column-type decimal
//	go run ./contract_new -root ../.. -type data.enums  -name note_status
//	go run ./contract_new -root ../.. -type data.entities -name note -table note -summary "备注实体"
//	go run ./contract_new -root ../.. -type data.queries -name note -module notes -table note -summary "备注查询"
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yourorg/web-seed/scripts/cmd/internal/contractspec"
	"gopkg.in/yaml.v3"
)

func main() {
	root := flag.String("root", "../..", "repo root")
	typ := flag.String("type", "", "contract type: data.tables | data.enums | data.entities | data.queries")
	name := flag.String("name", "", "snake_case name")
	module := flag.String("module", "", "owner_module / query module (default: name)")
	table := flag.String("table", "", "table stem (default: name)")
	summary := flag.String("summary", "", "summary (default: name)")
	out := flag.String("out", "", "output file (data.tables/entities/queries); empty prints to stdout; auto uses targets.dir")
	columnType := flag.String("column-type", "", "first column type family (default: schema default)")
	flag.Parse()

	if *typ == "" || *name == "" {
		fmt.Fprintln(os.Stderr, "usage: contract_new -type data.tables|data.enums|data.entities|data.queries -name <name> [-module <module>] [-table <table>] [-summary <text>] [-column-type <family>] [-out <file|auto>]")
		os.Exit(2)
	}
	if *module == "" {
		*module = *name
	}
	if *table == "" {
		*table = *name
	}
	if *summary == "" {
		*summary = *name
	}

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	schema, _, err := contractspec.LoadSchemaByType(abs, *typ)
	if err != nil {
		fail(err)
	}
	switch *typ {
	case "data.tables", "data.entities", "data.queries":
		if *out == "auto" {
			if schema.Targets.Dir == "" {
				fail(fmt.Errorf("targets.dir is empty"))
			}
			*out = filepath.Join(abs, schema.Targets.Dir, *name+".yaml")
		}
	}

	switch *typ {
	case "data.tables":
		b, err := contractspec.RenderTableWithColumnType(schema, *name, *module, *summary, *columnType)
		if err != nil {
			fail(err)
		}
		writeOut(*out, b, "next: replace TODO; register in contracts/index.yaml; add table.entities when an entity maps this table; make gen-graph; make contract-check; make gen")

	case "data.entities":
		_, owner, err := loadTableContract(abs, *table)
		if err != nil {
			fail(err)
		}
		b, err := contractspec.RenderEntity(schema, *name, *table, owner, *summary)
		if err != nil {
			fail(err)
		}
		writeOut(*out, b, "next: add business fields; add this entity to table.entities and to target entity model.included_by; register in contracts/index.yaml; make gen-graph; make contract-check; make gen")

	case "data.queries":
		physical, _, err := loadTableContract(abs, *table)
		if err != nil {
			fail(err)
		}
		b, err := contractspec.RenderQuery(schema, *name, *module, *table, physical, *summary)
		if err != nil {
			fail(err)
		}
		writeOut(*out, b, "next: replace the EXISTS skeleton; declare write queries in capabilities persistence.write_queries/writes; register in contracts/index.yaml; make gen-graph; make contract-check; make gen")

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

func writeOut(out string, b []byte, next string) {
	if out == "" {
		os.Stdout.Write(b)
		return
	}
	if _, err := os.Stat(out); err == nil {
		fail(fmt.Errorf("already exists: %s", out))
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		fail(err)
	}
	fmt.Printf("created %s\n", out)
	fmt.Printf("%s\n", next)
}

// loadTableContract 校验目标表契约存在，并返回物理表名与 owner_module。
func loadTableContract(root, table string) (string, string, error) {
	path := filepath.Join(root, "contracts/data/tables", table+".yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", fmt.Errorf("table contract not found: %s (create it first: make contract-new type=data.tables name=%s)", path, table)
	}
	var doc struct {
		ID          string `yaml:"id"`
		OwnerModule string `yaml:"owner_module"`
		Table       struct {
			Name string `yaml:"name"`
		} `yaml:"table"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return "", "", fmt.Errorf("%s: %w", path, err)
	}
	if doc.ID != "data.tables."+table {
		return "", "", fmt.Errorf("%s: id must be data.tables.%s, got %s", path, table, doc.ID)
	}
	if doc.OwnerModule == "" {
		return "", "", fmt.Errorf("%s: owner_module is required", path)
	}
	physical := table
	if doc.Table.Name != "" {
		physical = doc.Table.Name
	}
	return physical, doc.OwnerModule, nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "contract-new:", err)
	os.Exit(1)
}

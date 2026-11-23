package contractspec

import (
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func loadTestSchema(t *testing.T) *Schema {
	t.Helper()
	path, err := filepath.Abs("../../../../contracts/_meta/data.tables.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRenderTableUsesSchemaDefaults(t *testing.T) {
	s := loadTestSchema(t)
	for i := range s.Fields {
		switch s.Fields[i].Name {
		case "stability":
			s.Fields[i].Default = "stable"
		case "columns":
			for j := range s.Fields[i].ItemFields {
				if s.Fields[i].ItemFields[j].Name == "name" {
					s.Fields[i].ItemFields[j].Examples = []any{"custom_name"}
				}
			}
		}
	}

	b, err := RenderTable(s, "note", "notes", "备注表")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.tables.note" || doc["stability"] != "draft" {
		t.Fatalf("scaffold stability must stay draft even if meta default is stable: %#v", doc)
	}
	if _, ok := doc["extends"]; ok {
		t.Fatalf("extends must live under table: %#v", doc)
	}
	table, _ := doc["table"].(map[string]any)
	if table["name"] != "note" {
		t.Fatalf("table.name must be scaffolded: %#v", table)
	}
	extends, _ := table["extends"].([]any)
	if len(extends) != 1 || extends[0] != "data.tables.base" {
		t.Fatalf("table.extends must be hardcoded to [data.tables.base]: %#v", table["extends"])
	}
	if !strings.Contains(string(b), "  extends:\n    - data.tables.base\n") {
		t.Fatalf("table.extends must be block style:\n%s", b)
	}
	cols, _ := doc["columns"].([]any)
	if len(cols) != 1 {
		t.Fatalf("want 1 column, got %d", len(cols))
	}
	first, _ := cols[0].(map[string]any)
	if first["name"] != "custom_name" {
		t.Fatalf("column name not from schema examples: %#v", first)
	}
	if strings.Contains(string(b), "data.enums.") {
		t.Fatalf("table scaffold must not reference enums:\n%s", b)
	}
	if _, ok := table["comment"]; ok {
		t.Fatalf("table.comment must not be scaffolded:\n%s", b)
	}
	if !strings.Contains(string(b), `summary: "备注表"`) {
		t.Fatalf("scaffold summary must be double-quoted:\n%s", b)
	}
}

func loadSchemaByType(t *testing.T, typ string) *Schema {
	t.Helper()
	root, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	s, _, err := LoadSchemaByType(root, typ)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRenderEntity(t *testing.T) {
	s := loadSchemaByType(t, "data.entities")
	b, err := RenderEntity(s, "account_preview", "data.tables.account", "account", "账号预览")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.entities.account_preview" || doc["owner_module"] != "account" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	if _, ok := doc["do_not"]; !ok {
		t.Fatalf("do_not is required and must be scaffolded: %#v", doc)
	}
	model, _ := doc["model"].(map[string]any)
	if model["name"] != "accountPreview" {
		t.Fatalf("model.name not scaffolded: %#v", model)
	}
	tables, _ := model["tables"].([]any)
	if len(tables) != 1 || tables[0] != "data.tables.account" {
		t.Fatalf("model.tables not scaffolded: %#v", model["tables"])
	}
	if _, ok := doc["table"]; ok {
		t.Fatalf("single table field must not be generated: %#v", doc)
	}
	if _, ok := doc["identity"]; ok {
		t.Fatalf("identity must not be generated: %#v", doc)
	}
	extends, _ := model["extends"].([]any)
	if len(extends) != 1 || extends[0] != "data.entities.base" {
		t.Fatalf("model.extends must be [data.entities.base]: %#v", model["extends"])
	}
	if !strings.Contains(string(b), "  extends:\n    - data.entities.base\n") {
		t.Fatalf("model.extends must be block style:\n%s", b)
	}
	fields, _ := doc["fields"].([]any)
	if len(fields) != 0 {
		t.Fatalf("entity scaffold must not repeat base fields: %#v", fields)
	}
	queries, ok := model["queries"]
	if !ok {
		t.Fatalf("model.queries missing: %#v", model)
	}
	if got, ok := queries.([]any); !ok || len(got) != 0 {
		t.Fatalf("model.queries must be an empty list: %#v", queries)
	}
}

func TestRenderQuery(t *testing.T) {
	s := loadSchemaByType(t, "data.queries")
	b, err := RenderQuery(s, "account", "account", "data.tables.account", "账号查询")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != "data.queries.account" || doc["owner_module"] != "account" {
		t.Fatalf("unexpected top-level: %#v", doc)
	}
	if _, ok := doc["do_not"]; !ok {
		t.Fatalf("do_not is required and must be scaffolded: %#v", doc)
	}
	if strings.Index(string(b), "id: data.queries.account") > strings.Index(string(b), "schema_version") {
		t.Fatalf("query scaffold must follow template order (id before schema_version):\n%s", b)
	}
	queries, _ := doc["queries"].([]any)
	if len(queries) != 1 {
		t.Fatalf("want 1 query, got %d", len(queries))
	}
	q, _ := queries[0].(map[string]any)
	if q["name"] != "ExistsAccount" {
		t.Fatalf("query name not scaffolded: %#v", q)
	}
	if _, ok := q["id"]; ok {
		t.Fatalf("query item id must not be scaffolded: %#v", q)
	}
	returns, _ := q["returns"].([]any)
	if len(returns) != 2 {
		t.Fatalf("query returns not scaffolded: %#v", q)
	}
	errRet, _ := returns[1].(map[string]any)
	if errRet["name"] != "err" || errRet["kind"] != "error" {
		t.Fatalf("query err return not scaffolded: %#v", q["returns"])
	}
	params, _ := q["params"].([]any)
	if len(params) != 2 {
		t.Fatalf("query params not scaffolded: %#v", q)
	}
	ctxParam, _ := params[0].(map[string]any)
	if ctxParam["name"] != "ctx" || ctxParam["type"] != "context.Context" {
		t.Fatalf("query ctx param not scaffolded: %#v", q["params"])
	}
	param, _ := params[1].(map[string]any)
	if param["type"] != "_types.integer" || param["column"] != "data.tables.account.col.id" {
		t.Fatalf("query id param must bind the integer primary key: %#v", param)
	}
	ret, _ := returns[0].(map[string]any)
	if ret["kind"] != "scalar" || ret["type"] != "_types.boolean" {
		t.Fatalf("query return shape not scaffolded: %#v", ret)
	}
	targets, _ := q["tables"].([]any)
	if len(targets) != 1 {
		t.Fatalf("tables not scaffolded as {table, write}: %#v", q["tables"])
	}
	firstTarget, _ := targets[0].(map[string]any)
	if firstTarget["table"] != "data.tables.account" {
		t.Fatalf("tables not scaffolded as {table, write}: %#v", q["tables"])
	}
	if _, ok := firstTarget["write"]; ok {
		t.Fatalf("read scaffold must omit write: %#v", firstTarget)
	}
	if _, ok := q["access"]; ok {
		t.Fatalf("access must be omitted for read scaffold: %#v", q)
	}
	if _, ok := q["kind"]; ok {
		t.Fatalf("query kind must not be scaffolded: %#v", q)
	}
	refs, _ := q["index_ref"].([]any)
	if len(refs) != 1 {
		t.Fatalf("index_ref not scaffolded: %#v", q["index_ref"])
	}
	ref, _ := refs[0].(map[string]any)
	cols, _ := ref["columns"].([]any)
	if ref["index"] != "data.tables.account.pk" || len(cols) != 1 || cols[0] != "id" {
		t.Fatalf("index_ref must scaffold {index, columns}: %#v", q["index_ref"])
	}
	if _, ok := q["sql"]; ok {
		t.Fatalf("query must not scaffold sql: %#v", q)
	}
	if _, err := RenderQuery(s, "note", "notes", "", "x"); err == nil {
		t.Fatal("empty table ID should fail")
	}
}

func TestRenderEntityRejectsUnsupportedModelRequired(t *testing.T) {
	s := loadSchemaByType(t, "data.entities")
	for i := range s.Fields {
		if s.Fields[i].Name == "model" {
			s.Fields[i].Fields = append(s.Fields[i].Fields, Field{Name: "new_required", Type: "string", Required: true})
		}
	}
	if _, err := RenderEntity(s, "note", "data.tables.note", "notes", "备注实体"); err == nil || !strings.Contains(err.Error(), "new_required") {
		t.Fatalf("expected unsupported required model field to fail with field name, got %v", err)
	}
}

func TestRenderQueryRejectsUnsupportedReturnRequired(t *testing.T) {
	s := loadSchemaByType(t, "data.queries")
	for i := range s.Fields {
		if s.Fields[i].Name != "queries" {
			continue
		}
		for j := range s.Fields[i].ItemFields {
			if s.Fields[i].ItemFields[j].Name == "returns" {
				s.Fields[i].ItemFields[j].ItemFields = append(s.Fields[i].ItemFields[j].ItemFields, Field{Name: "new_required", Type: "string", Required: true})
			}
		}
	}
	if _, err := RenderQuery(s, "note", "notes", "data.tables.note", "备注查询"); err == nil || !strings.Contains(err.Error(), "new_required") {
		t.Fatalf("expected unsupported required return field to fail with field name, got %v", err)
	}
}

func TestRenderTableDecimalFamilyParams(t *testing.T) {
	s := loadTestSchema(t)
	b, err := RenderTableWithColumnType(s, "note", "notes", "备注表", "decimal")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	cols, _ := doc["columns"].([]any)
	first, _ := cols[0].(map[string]any)
	if first["type"] != "_types.decimal" {
		t.Fatalf("column type not overridden: %#v", first)
	}
	if first["precision"] != 19 {
		t.Fatalf("precision not scaffolded: %#v", first)
	}
	if first["scale"] != 0 {
		t.Fatalf("scale not scaffolded: %#v", first)
	}
}

func TestRenderTableScaffoldRequiresParamValue(t *testing.T) {
	s := loadTestSchema(t)
	s.ValueTypes.TypeFamilies["weird"] = ValueTypeFamily{Params: map[string]Field{
		"missing": {Type: "integer", Required: true},
	}}
	if _, err := RenderTableWithColumnType(s, "note", "notes", "备注表", "weird"); err == nil {
		t.Fatal("expected required param without default/examples to fail")
	}
}

func TestRenderTableValidatesInput(t *testing.T) {
	s := loadTestSchema(t)
	if _, err := RenderTable(s, "Bad-Name", "notes", "备注表"); err == nil {
		t.Fatal("expected invalid name to fail")
	}
	if _, err := RenderTable(s, "note", "Bad-Module", "备注表"); err == nil {
		t.Fatal("expected invalid module to fail")
	}
	if _, err := RenderTable(s, "note", "notes", "  "); err == nil {
		t.Fatal("expected empty summary to fail")
	}
}

func TestRenderTableRejectsUnsupportedRequired(t *testing.T) {
	s := loadTestSchema(t)
	s.Fields = append(s.Fields, Field{Name: "new_required", Type: "string", Required: true})
	if _, err := RenderTable(s, "note", "notes", "备注表"); err == nil {
		t.Fatal("expected unsupported required field to fail")
	}
}

func TestRenderTableOmitsInheritedStatus(t *testing.T) {
	s := loadTestSchema(t)
	b, err := RenderTable(s, "note", "notes", "备注表")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	cols, _ := doc["columns"].([]any)
	for _, raw := range cols {
		col, _ := raw.(map[string]any)
		if col["name"] == "status" {
			t.Fatalf("status is inherited from base and must not be scaffolded: %#v", col)
		}
	}
}

func TestRenderTableFallbacks(t *testing.T) {
	s := loadSchemaByType(t, "data.tables")
	b, err := RenderTable(s, "note", "notes", "备注表")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["stability"] != "draft" {
		t.Fatalf("stability fallback must be draft: %#v", doc["stability"])
	}
	cols, _ := doc["columns"].([]any)
	if len(cols) != 1 {
		t.Fatalf("want one column: %#v", doc["columns"])
	}
	first, _ := cols[0].(map[string]any)
	if first["type"] != "_types.string" {
		t.Fatalf("column type fallback must be _types.string: %#v", first)
	}
}

func TestScaffoldsNeverEmitStable(t *testing.T) {
	cases := []struct {
		typ    string
		render func(*Schema) ([]byte, error)
	}{
		{"data.tables", func(s *Schema) ([]byte, error) { return RenderTable(s, "note", "notes", "备注表") }},
		{"data.entities", func(s *Schema) ([]byte, error) {
			return RenderEntity(s, "note", "data.tables.account", "account", "备注实体")
		}},
		{"data.queries", func(s *Schema) ([]byte, error) {
			return RenderQuery(s, "note", "notes", "data.tables.account", "备注查询")
		}},
		{"feature.pages", func(s *Schema) ([]byte, error) {
			return RenderPage(s, "note_list", "备注列表页")
		}},
	}
	for _, tc := range cases {
		s := loadSchemaByType(t, tc.typ)
		for i := range s.Fields {
			if s.Fields[i].Name == "stability" {
				s.Fields[i].Default = "stable"
			}
		}
		b, err := tc.render(s)
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		var doc map[string]any
		if err := yaml.Unmarshal(b, &doc); err != nil {
			t.Fatalf("%s: generated YAML invalid: %v", tc.typ, err)
		}
		if doc["stability"] != "draft" {
			t.Fatalf("%s: scaffold must emit draft, got %#v", tc.typ, doc["stability"])
		}
	}
}

func TestRenderEnumItemRejectsInvalidName(t *testing.T) {
	enumSchema := &Schema{Type: "data.enums"}
	if _, err := RenderEnumItem(enumSchema, "Bad-Name", "account"); err == nil {
		t.Fatal("expected invalid enum name to fail")
	}
}

func TestRenderEnumItem(t *testing.T) {
	path, err := filepath.Abs("../../../../contracts/_meta/data.enums.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s, err := LoadSchema(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := RenderEnumItem(s, "note_status", "account")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != nil || doc["name"] != "note_status" || doc["go_type"] != "NoteStatus" {
		t.Fatalf("enum item must not carry id and must keep name: %#v", doc)
	}
}

func TestRenderEnumItemUsesNameAsIDSource(t *testing.T) {
	s := &Schema{Type: "data.enums", Targets: Targets{ID: "data.enums.<file>", Dir: "contracts/data/enums"}}
	b, err := RenderEnumItem(s, "note_status", "account")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatalf("generated YAML invalid: %v\n%s", err, b)
	}
	if doc["id"] != nil || doc["name"] != "note_status" {
		t.Fatalf("item id is derived from name and must not be rendered: %#v", doc)
	}
	usedBy, ok := doc["used_by"].([]any)
	if !ok || len(usedBy) != 0 {
		t.Fatalf("used_by list must be scaffolded empty for manual fill: %#v", doc)
	}
	if strings.Index(string(b), "values:") > strings.Index(string(b), "used_by:") {
		t.Fatalf("scaffold must follow template order (values before used_by):\n%s", b)
	}
}

func TestRenderEnumItemRequiresTargets(t *testing.T) {
	if _, err := RenderEnumItem(&Schema{Type: "data.enums"}, "note_status", "account"); err == nil {
		t.Fatal("expected missing targets to fail")
	}
}

func TestRenderService(t *testing.T) {
	s := loadSchemaByType(t, "cap.services")
	b, err := RenderService(s, "invoice", "invoice", "发票服务")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["id"] != "cap.services.invoice" {
		t.Fatalf("id = %v", doc["id"])
	}
	if _, ok := doc["interface"]; ok {
		t.Fatalf("interface scaffold must be removed, got %#v", doc["interface"])
	}
	services, _ := doc["services"].([]any)
	if len(services) != 1 {
		t.Fatalf("services = %#v", services)
	}
	m, _ := services[0].(map[string]any)
	if m["name"] != "get" || m["visibility"] != "public" || m["tx_mode"] != "read_only" {
		t.Fatalf("service = %#v", m)
	}
	steps, _ := m["steps"].([]any)
	if len(steps) != 1 {
		t.Fatalf("steps = %#v", steps)
	}
	step, _ := steps[0].(map[string]any)
	if step["kind"] != "logic" {
		t.Fatalf("step = %#v", step)
	}
}

func TestRenderPage(t *testing.T) {
	s := loadSchemaByType(t, "feature.pages")
	b, err := RenderPage(s, "note_list", "备注列表页")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["id"] != "feature.pages.note_list" || doc["page"] != "note_list" {
		t.Fatalf("id/page = %v/%v", doc["id"], doc["page"])
	}
	if v, ok := doc["route"]; !ok || v != nil {
		t.Fatalf("route = %#v, want null", doc["route"])
	}
	if !strings.Contains(string(b), `when: "loading"`) {
		t.Fatalf("when must be double-quoted:\n%s", b)
	}
	states, _ := doc["view_states"].(map[string]any)
	for _, key := range []string{"loading", "empty", "error", "forbidden"} {
		state, _ := states[key].(map[string]any)
		if state["when"] == "" || state["when"] == nil {
			t.Fatalf("view_states.%s.when missing: %#v", key, states[key])
		}
	}
}

func TestRenderEnumItemQuotesProse(t *testing.T) {
	s := loadSchemaByType(t, "data.enums")
	b, err := RenderEnumItem(s, "note_status", "notes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `description: "TODO: 枚举用途"`) {
		t.Fatalf("enum item prose must be double-quoted:\n%s", b)
	}
}

func TestRenderRule(t *testing.T) {
	s := loadSchemaByType(t, "feature.rules")
	b, err := RenderRule(s, "invoice", "invoice", "发票规则")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["id"] != "feature.rules.invoice" {
		t.Fatalf("id = %v", doc["id"])
	}
	rules, _ := doc["rules"].([]any)
	if len(rules) != 1 {
		t.Fatalf("rules = %#v", rules)
	}
	item, _ := rules[0].(map[string]any)
	if item["kind"] != "policy" {
		t.Fatalf("rule = %#v", item)
	}
}

func TestRenderHTTP(t *testing.T) {
	s := loadSchemaByType(t, "cap.protocols.http")
	b, err := RenderHTTP(s, "invoice", "invoice", "发票 HTTP", "cap.services.invoice.get", "/invoice", "get")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["id"] != "cap.protocols.http.invoice" {
		t.Fatalf("id = %v", doc["id"])
	}
	mount, _ := doc["mount"].(map[string]any)
	if mount["api_version"] != "v1" || mount["path"] != "/" {
		t.Fatalf("mount = %#v", mount)
	}
	paths, _ := doc["paths"].(map[string]any)
	path, _ := paths["/invoice"].(map[string]any)
	op, _ := path["get"].(map[string]any)
	if op["name"] != "getInvoice" || op["id"] != "cap.protocols.http.invoice.get" || op["service"] != "cap.services.invoice.get" {
		t.Fatalf("operation = %#v", op)
	}
	security, ok := op["security"].(map[string]any)
	if !ok || security["BearerAuth"] != false {
		t.Fatalf("security = %#v", op["security"])
	}
	usedBy, _ := op["used_by"].([]any)
	if len(usedBy) != 1 || usedBy[0] != "feature.pages.invoice" {
		t.Fatalf("used_by = %#v", op["used_by"])
	}
	opText := string(b)[strings.Index(string(b), "get:"):]
	order := []string{"id:", "name:", "service:", "summary:", "used_by:", "security:", "request:", "response:"}
	for i := 1; i < len(order); i++ {
		prev := strings.Index(opText, order[i-1])
		next := strings.Index(opText, order[i])
		if prev < 0 || next < 0 || prev > next {
			t.Fatalf("operation key order broken at %s:\n%s", order[i], b)
		}
	}
}

func TestRenderEvents(t *testing.T) {
	s := loadSchemaByType(t, "cap.events")
	b, err := RenderEvents(s, "invoice", "invoice", "发票异步层")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["id"] != "cap.events.invoice" {
		t.Fatalf("id = %v", doc["id"])
	}
	topics, _ := doc["topics"].([]any)
	if len(topics) != 1 {
		t.Fatalf("topics = %#v", topics)
	}
	topic, _ := topics[0].(map[string]any)
	if topic["id"] != "cap.events.invoice.created" || topic["name"] != "invoice.created" {
		t.Fatalf("topic = %#v", topic)
	}
}

func TestRenderLifecycle(t *testing.T) {
	s := loadSchemaByType(t, "cap.lifecycles")
	b, err := RenderLifecycle(s, "invoice", "invoice", "发票生命周期")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc["id"] != "cap.lifecycles.invoice" {
		t.Fatalf("id = %v", doc["id"])
	}
	if doc["initial"] != nil || doc["final"] != nil {
		t.Fatalf("initial/final must be nested under states: %#v", doc)
	}
	subjects, _ := doc["subjects"].([]any)
	if len(subjects) != 1 {
		t.Fatalf("subjects = %#v", subjects)
	}
	subject, _ := subjects[0].(map[string]any)
	if subject["entity"] != "data.entities.invoice" || subject["field"] != "status" {
		t.Fatalf("subjects = %#v", subjects)
	}
	states, _ := doc["states"].(map[string]any)
	values, _ := states["values"].([]any)
	final, _ := states["final"].([]any)
	if len(values) != 1 || values[0] != "active" || states["initial"] != "active" || len(final) != 1 || final[0] != "active" {
		t.Fatalf("states = %#v", states)
	}
	create, _ := doc["create"].(map[string]any)
	implemented, _ := create["implemented_by"].([]any)
	if len(implemented) != 1 || implemented[0] != "cap.services.invoice.create.create" {
		t.Fatalf("create = %#v", create)
	}
}

func TestServiceInterfaceName(t *testing.T) {
	cases := []struct{ owner, file, want string }{
		{"account", "account", "AccountService"},
		{"account", "auth", "AccountAuthService"},
		{"account", "token", "AccountTokenService"},
		{"notes", "notes", "NotesService"},
	}
	for _, c := range cases {
		if got := ServiceInterfaceName(c.owner, c.file); got != c.want {
			t.Fatalf("ServiceInterfaceName(%q, %q) = %q, want %q", c.owner, c.file, got, c.want)
		}
	}
}

func TestRenderFieldValueListMapStruct(t *testing.T) {
	v, err := RenderFieldValue(Field{
		Type: "_types.list<_types.map<_types.object>>",
		ItemFields: []Field{
			{Name: "id", Type: "_types.string"},
			{Name: "count", Type: "_types.integer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("want one-element list, got %#v", v)
	}
	m, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("want map, got %#v", list[0])
	}
	obj, ok := m["key"].(map[string]any)
	if !ok {
		t.Fatalf("want map value object, got %#v", m["key"])
	}
	if obj["id"] != "item_1" || obj["count"] != 1 {
		t.Fatalf("unexpected skeleton: %#v", obj)
	}
}

func TestRenderFieldValueMapInteger(t *testing.T) {
	v, err := RenderFieldValue(Field{
		Type:       "_types.map<_types.integer, _types.object>",
		ItemFields: []Field{{Name: "id", Type: "_types.string"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("want map, got %#v", v)
	}
	obj, ok := m["key"].(map[string]any)
	if !ok || obj["id"] != "" {
		t.Fatalf("unexpected map value skeleton: %#v", m)
	}
}

func TestRenderFieldValueListMapInteger(t *testing.T) {
	v, err := RenderFieldValue(Field{
		Type:       "_types.list<_types.map<_types.integer, _types.object>>",
		ItemFields: []Field{{Name: "id", Type: "_types.string"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 1 {
		t.Fatalf("want one-element list, got %#v", v)
	}
	m, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("want map, got %#v", list[0])
	}
	obj, ok := m["key"].(map[string]any)
	if !ok || obj["id"] != "item_1" {
		t.Fatalf("unexpected nested map skeleton: %#v", m)
	}
}

func TestRenderFieldValueSet(t *testing.T) {
	v, err := RenderFieldValue(Field{Type: "_types.set<_types.string>"})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 1 || list[0] != "item_1" {
		t.Fatalf("unexpected set skeleton: %#v", v)
	}
}

func TestRenderFieldValueArray(t *testing.T) {
	v, err := RenderFieldValue(Field{Type: "_types.array<_types.integer>"})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 1 || list[0] != 1 {
		t.Fatalf("unexpected array skeleton: %#v", v)
	}
}

func TestRenderFieldValueArrayLen(t *testing.T) {
	v, err := RenderFieldValue(Field{Type: "_types.array<_types.integer>", Len: 3})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 3 {
		t.Fatalf("want 3 elements, got %#v", v)
	}
}

func TestRenderFieldValueListMinLen(t *testing.T) {
	v, err := RenderFieldValue(Field{Type: "_types.list<_types.string>", MinItems: 2})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("want 2 elements, got %#v", v)
	}
}

func TestRenderFieldValueListMinMax(t *testing.T) {
	v, err := RenderFieldValue(Field{Type: "_types.list<_types.string>", MinItems: 2, MaxItems: 5})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("want 2 elements, got %#v", v)
	}
}

func TestRenderFieldValueSetDedup(t *testing.T) {
	v, err := RenderFieldValue(Field{Type: "_types.set<_types.string>", MinItems: 2})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 || list[0] == list[1] {
		t.Fatalf("set skeleton not deduped: %#v", v)
	}
}

func TestRenderFieldValueSetObjectDedup(t *testing.T) {
	v, err := RenderFieldValue(Field{
		Type:       "_types.set<_types.object>",
		MinItems:   2,
		UniqueKey:  "name",
		ItemFields: []Field{{Name: "name", Type: "_types.string", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("want 2 elements, got %#v", v)
	}
	a, _ := list[0].(map[string]any)
	b, _ := list[1].(map[string]any)
	if a["name"] == b["name"] {
		t.Fatalf("set<object> unique_key not deduped: %#v", v)
	}
}

func TestRenderFieldValueSetObjectIntegerKey(t *testing.T) {
	v, err := RenderFieldValue(Field{
		Type:       "_types.set<_types.object>",
		MinItems:   2,
		UniqueKey:  "id",
		ItemFields: []Field{{Name: "id", Type: "_types.integer", Required: true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("want 2 elements, got %#v", v)
	}
	a, _ := list[0].(map[string]any)
	b, _ := list[1].(map[string]any)
	if a["id"] != 1 || b["id"] != 2 {
		t.Fatalf("integer unique_key skeleton mismatch: %#v", v)
	}
}

func TestRenderFieldValueCustomIntegerKey(t *testing.T) {
	v, err := RenderFieldValueWithTypes(Field{
		Type:       "_types.set<_types.object>",
		MinItems:   2,
		UniqueKey:  "id",
		ItemFields: []Field{{Name: "id", Type: "_types.page_limit", Required: true}},
	}, ValueTypes{
		CustomTypes: map[string]CustomValueType{
			"page_limit": {Family: "integer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("want 2 elements, got %#v", v)
	}
	a, _ := list[0].(map[string]any)
	b, _ := list[1].(map[string]any)
	if a["id"] != 1 || b["id"] != 2 {
		t.Fatalf("custom integer unique_key skeleton mismatch: %#v", v)
	}
}

func TestRenderFieldValueCustomDateKey(t *testing.T) {
	v, err := RenderFieldValueWithTypes(Field{
		Type:       "_types.set<_types.object>",
		MinItems:   2,
		UniqueKey:  "at",
		ItemFields: []Field{{Name: "at", Type: "_types.date_only", Required: true}},
	}, ValueTypes{
		CustomTypes: map[string]CustomValueType{
			"date_only": {Family: "datetime", Params: map[string]any{"mode": "date"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	list, ok := v.([]any)
	if !ok || len(list) != 2 {
		t.Fatalf("want 2 elements, got %#v", v)
	}
	a, _ := list[0].(map[string]any)
	b, _ := list[1].(map[string]any)
	if a["at"] != "2024-01-01" || b["at"] != "2024-01-02" {
		t.Fatalf("date mode skeleton mismatch: %#v", v)
	}
}

// gencapabilities 从 contracts/capabilities/services/*.yaml 生成 Go 契约测试骨架。
//
// 产物（每个契约文件两份）：
//
//	apps/api/internal/contracttest/generated/<file>_gen.go              规格数据
//	apps/api/internal/contracttest/generated/<file>_contract_gen_test.go 逐 step 断言骨架
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./gen_capabilities -root ../..
//	go run ./gen_capabilities -root ../.. -check
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const generatedDir = "apps/api/internal/contracttest/generated"

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify generated files are up to date")
	flag.Parse()

	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	if err := run(abs, *check); err != nil {
		fail(err)
	}
	if *check {
		fmt.Println("contract test skeletons: OK")
	}
}

func run(root string, check bool) error {
	outputs, err := build(root)
	if err != nil {
		return err
	}
	if check {
		return verify(root, outputs)
	}
	if err := write(root, outputs); err != nil {
		return err
	}
	for _, path := range sortedKeys(outputs) {
		fmt.Println("generated", path)
	}
	return nil
}

// build 渲染全部生成物；键是绝对路径。
func build(root string) (map[string][]byte, error) {
	module, err := modulePath(root)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "contracts/capabilities/services")
	files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("%s: no service contracts", dir)
	}
	sort.Strings(files)

	outputs := map[string][]byte{}
	seenNames := map[string]string{}
	for _, path := range files {
		doc, err := loadYAML(path)
		if err != nil {
			return nil, err
		}
		f, err := parseFile(path, doc)
		if err != nil {
			return nil, err
		}
		if prev, ok := seenNames[f.GoName]; ok {
			return nil, fmt.Errorf("%s and %s derive the same Go name %s", prev, path, f.GoName)
		}
		seenNames[f.GoName] = path
		src, err := renderFile(f, module)
		if err != nil {
			return nil, err
		}
		test, err := renderTest(f, module)
		if err != nil {
			return nil, err
		}
		outputs[filepath.Join(root, generatedDir, f.Stem+"_gen.go")] = src
		outputs[filepath.Join(root, generatedDir, f.Stem+"_contract_gen_test.go")] = test
	}
	return outputs, nil
}

func verify(root string, outputs map[string][]byte) error {
	for _, path := range sortedKeys(outputs) {
		got, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s is missing; run make gen-capabilities", path)
		}
		if !bytes.Equal(got, outputs[path]) {
			return fmt.Errorf("%s is stale; run make gen-capabilities", path)
		}
	}
	orphans, err := findOrphans(root, outputs)
	if err != nil {
		return err
	}
	if len(orphans) > 0 {
		return fmt.Errorf("orphan generated files %v; run make gen-capabilities", orphans)
	}
	return nil
}

func write(root string, outputs map[string][]byte) error {
	orphans, err := findOrphans(root, outputs)
	if err != nil {
		return err
	}
	for _, name := range orphans {
		if err := os.Remove(filepath.Join(root, generatedDir, name)); err != nil {
			return err
		}
	}
	for _, path := range sortedKeys(outputs) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, outputs[path], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// findOrphans 只扫描自身命名模式，避免误删同目录的手写文件。
func findOrphans(root string, outputs map[string][]byte) ([]string, error) {
	dir := filepath.Join(root, generatedDir)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var orphans []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, "_gen.go") && !strings.HasSuffix(name, "_contract_gen_test.go") {
			continue
		}
		path := filepath.Join(dir, name)
		if _, ok := outputs[path]; !ok {
			orphans = append(orphans, name)
		}
	}
	sort.Strings(orphans)
	return orphans, nil
}

func modulePath(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "apps/api/go.mod"))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module")), nil
		}
	}
	return "", fmt.Errorf("apps/api/go.mod: module directive not found")
}

type fileDoc struct {
	Stem        string
	GoName      string
	ID          string
	Summary     string
	OwnerModule string
	Schemas     []string
	Services    []serviceDoc
}

type serviceDoc struct {
	ID            string
	Name          string
	Summary       string
	Visibility    string
	Transaction   string
	MaxIterations int
	Params        []paramDoc
	Returns       []fieldDoc
	Steps         []stepDoc
}

type paramDoc struct {
	Name     string
	Type     string
	Nullable bool
}

type fieldDoc struct {
	Name     string
	Type     string
	Nullable bool
}

type inputDoc struct {
	Name  string
	Value string
}

type queryDoc struct {
	Query string
	Input []inputDoc
}

type onErrorDoc struct {
	Error  string
	Action string
	Target string
}

type stepDoc struct {
	ID          string
	Kind        string
	Summary     string
	DependsOn   []string
	When        string
	Errors      []string
	OnError     []onErrorDoc
	Query       string
	Service     string
	Input       []inputDoc
	Output      []fieldDoc
	Queries     []queryDoc
	Events      []string
	Rules       []string
	Transitions []string
}

func parseFile(path string, doc map[string]any) (fileDoc, error) {
	stem := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	id := str(doc["id"])
	if id == "" {
		return fileDoc{}, fmt.Errorf("%s: missing id", path)
	}
	owner := str(doc["owner_module"])
	if owner == "" {
		return fileDoc{}, fmt.Errorf("%s: missing owner_module", path)
	}
	f := fileDoc{
		Stem:        stem,
		GoName:      goName(owner, stem),
		ID:          id,
		Summary:     str(doc["summary"]),
		OwnerModule: owner,
	}
	if schemas, ok := asMap(doc["schemas"]); ok {
		f.Schemas = sortedMapKeys(schemas)
	}
	for _, raw := range asList(doc["services"]) {
		m, ok := asMap(raw)
		if !ok {
			return fileDoc{}, fmt.Errorf("%s: service item must be an object", path)
		}
		s, err := parseService(m)
		if err != nil {
			return fileDoc{}, fmt.Errorf("%s: %w", path, err)
		}
		s.ID = f.ID + "." + s.Name
		f.Services = append(f.Services, s)
	}
	if len(f.Services) == 0 {
		return fileDoc{}, fmt.Errorf("%s: no services", path)
	}
	return f, nil
}

func parseService(m map[string]any) (serviceDoc, error) {
	name := str(m["name"])
	if name == "" {
		return serviceDoc{}, fmt.Errorf("service missing name")
	}
	s := serviceDoc{
		Name:          name,
		Summary:       str(m["summary"]),
		Visibility:    str(m["visibility"]),
		Transaction:   str(m["transaction"]),
		MaxIterations: intVal(m["max_iterations"]),
	}
	for _, raw := range asList(m["params"]) {
		p, _ := asMap(raw)
		s.Params = append(s.Params, paramDoc{Name: str(p["name"]), Type: str(p["type"]), Nullable: boolVal(p["nullable"])})
	}
	for _, raw := range asList(m["returns"]) {
		r, _ := asMap(raw)
		s.Returns = append(s.Returns, fieldDoc{Name: str(r["name"]), Type: str(r["type"]), Nullable: boolVal(r["nullable"])})
	}
	for _, raw := range asList(m["steps"]) {
		st, ok := asMap(raw)
		if !ok {
			return serviceDoc{}, fmt.Errorf("service %s: step must be an object", name)
		}
		s.Steps = append(s.Steps, parseStep(st))
	}
	if len(s.Steps) == 0 {
		return serviceDoc{}, fmt.Errorf("service %s has no steps", name)
	}
	return s, nil
}

func parseStep(m map[string]any) stepDoc {
	st := stepDoc{
		ID:          str(m["id"]),
		Kind:        str(m["kind"]),
		Summary:     str(m["summary"]),
		DependsOn:   strList(m["depends_on"]),
		When:        str(m["when"]),
		Errors:      strList(m["errors"]),
		Query:       str(m["query"]),
		Service:     str(m["service"]),
		Input:       inputsFromMap(m["input"]),
		Events:      strList(m["events"]),
		Rules:       strList(m["rules"]),
		Transitions: strList(m["transitions"]),
	}
	for _, raw := range asList(m["output"]) {
		o, _ := asMap(raw)
		st.Output = append(st.Output, fieldDoc{Name: str(o["name"]), Type: str(o["type"]), Nullable: boolVal(o["nullable"])})
	}
	for _, raw := range asList(m["queries"]) {
		q, _ := asMap(raw)
		st.Queries = append(st.Queries, queryDoc{Query: str(q["query"]), Input: inputsFromMap(q["input"])})
	}
	for _, raw := range asList(m["on_error"]) {
		e, _ := asMap(raw)
		st.OnError = append(st.OnError, onErrorDoc{Error: str(e["error"]), Action: str(e["action"]), Target: str(e["target"])})
	}
	return st
}

// inputsFromMap 按键排序，保证生成物稳定。
func inputsFromMap(v any) []inputDoc {
	m, ok := asMap(v)
	if !ok {
		return nil
	}
	out := make([]inputDoc, 0, len(m))
	for _, k := range sortedMapKeys(m) {
		out = append(out, inputDoc{Name: k, Value: str(m[k])})
	}
	return out
}

func renderFile(f fileDoc, module string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by make gen-capabilities; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: contracts/capabilities/services/%s.yaml\n\n", f.Stem)
	b.WriteString("package generated\n\n")
	fmt.Fprintf(&b, "import %q\n\n", module+"/internal/contracttest")
	fmt.Fprintf(&b, "// %sContract 是 %s 的步骤断言规格。\n", f.GoName, f.ID)
	fmt.Fprintf(&b, "var %sContract = contracttest.File{\n", f.GoName)
	fmt.Fprintf(&b, "\tID: %q,\n", f.ID)
	if f.Summary != "" {
		fmt.Fprintf(&b, "\tSummary: %q,\n", f.Summary)
	}
	fmt.Fprintf(&b, "\tOwnerModule: %q,\n", f.OwnerModule)
	if len(f.Schemas) > 0 {
		renderStrings(&b, "\t", "Schemas", f.Schemas)
	}
	b.WriteString("\tServices: []contracttest.Service{\n")
	for _, s := range f.Services {
		renderService(&b, s)
	}
	b.WriteString("\t},\n}\n")
	return formatSource(b.String(), f.Stem)
}

func renderService(b *strings.Builder, s serviceDoc) {
	b.WriteString("\t\t{\n")
	fmt.Fprintf(b, "\t\t\tID: %q,\n", s.ID)
	fmt.Fprintf(b, "\t\t\tName: %q,\n", s.Name)
	if s.Summary != "" {
		fmt.Fprintf(b, "\t\t\tSummary: %q,\n", s.Summary)
	}
	fmt.Fprintf(b, "\t\t\tVisibility: %q,\n", s.Visibility)
	fmt.Fprintf(b, "\t\t\tTransaction: %q,\n", s.Transaction)
	if s.MaxIterations > 0 {
		fmt.Fprintf(b, "\t\t\tMaxIterations: %d,\n", s.MaxIterations)
	}
	if len(s.Params) > 0 {
		b.WriteString("\t\t\tParams: []contracttest.Param{\n")
		for _, p := range s.Params {
			fmt.Fprintf(b, "\t\t\t\t{Name: %q, Type: %q, Nullable: %t},\n", p.Name, p.Type, p.Nullable)
		}
		b.WriteString("\t\t\t},\n")
	}
	if len(s.Returns) > 0 {
		b.WriteString("\t\t\tReturns: []contracttest.Field{\n")
		for _, r := range s.Returns {
			renderField(b, "\t\t\t\t", r)
		}
		b.WriteString("\t\t\t},\n")
	}
	if len(s.Steps) > 0 {
		b.WriteString("\t\t\tSteps: []contracttest.Step{\n")
		for _, st := range s.Steps {
			renderStep(b, st)
		}
		b.WriteString("\t\t\t},\n")
	}
	b.WriteString("\t\t},\n")
}

func renderStep(b *strings.Builder, st stepDoc) {
	b.WriteString("\t\t\t\t{\n")
	fmt.Fprintf(b, "\t\t\t\t\tID: %q,\n", st.ID)
	fmt.Fprintf(b, "\t\t\t\t\tKind: %q,\n", st.Kind)
	if st.Summary != "" {
		fmt.Fprintf(b, "\t\t\t\t\tSummary: %q,\n", st.Summary)
	}
	if len(st.DependsOn) > 0 {
		renderStrings(b, "\t\t\t\t\t", "DependsOn", st.DependsOn)
	}
	if st.When != "" {
		fmt.Fprintf(b, "\t\t\t\t\tWhen: %q,\n", st.When)
	}
	if len(st.Errors) > 0 {
		renderStrings(b, "\t\t\t\t\t", "Errors", st.Errors)
	}
	if len(st.OnError) > 0 {
		b.WriteString("\t\t\t\t\tOnError: []contracttest.OnError{\n")
		for _, e := range st.OnError {
			if e.Target != "" {
				fmt.Fprintf(b, "\t\t\t\t\t\t{Error: %q, Action: %q, Target: %q},\n", e.Error, e.Action, e.Target)
			} else {
				fmt.Fprintf(b, "\t\t\t\t\t\t{Error: %q, Action: %q},\n", e.Error, e.Action)
			}
		}
		b.WriteString("\t\t\t\t\t},\n")
	}
	if st.Query != "" {
		fmt.Fprintf(b, "\t\t\t\t\tQuery: %q,\n", st.Query)
	}
	if st.Service != "" {
		fmt.Fprintf(b, "\t\t\t\t\tService: %q,\n", st.Service)
	}
	if len(st.Input) > 0 {
		renderInputs(b, "\t\t\t\t\t", "Input", st.Input)
	}
	if len(st.Output) > 0 {
		b.WriteString("\t\t\t\t\tOutput: []contracttest.Field{\n")
		for _, f := range st.Output {
			renderField(b, "\t\t\t\t\t\t", f)
		}
		b.WriteString("\t\t\t\t\t},\n")
	}
	if len(st.Queries) > 0 {
		b.WriteString("\t\t\t\t\tQueries: []contracttest.QueryCall{\n")
		for _, q := range st.Queries {
			b.WriteString("\t\t\t\t\t\t{\n")
			fmt.Fprintf(b, "\t\t\t\t\t\t\tQuery: %q,\n", q.Query)
			if len(q.Input) > 0 {
				renderInputs(b, "\t\t\t\t\t\t\t", "Input", q.Input)
			}
			b.WriteString("\t\t\t\t\t\t},\n")
		}
		b.WriteString("\t\t\t\t\t},\n")
	}
	if len(st.Events) > 0 {
		renderStrings(b, "\t\t\t\t\t", "Events", st.Events)
	}
	if len(st.Rules) > 0 {
		renderStrings(b, "\t\t\t\t\t", "Rules", st.Rules)
	}
	if len(st.Transitions) > 0 {
		renderStrings(b, "\t\t\t\t\t", "Transitions", st.Transitions)
	}
	b.WriteString("\t\t\t\t},\n")
}

func renderField(b *strings.Builder, indent string, f fieldDoc) {
	fmt.Fprintf(b, "%s{Name: %q, Type: %q, Nullable: %t},\n", indent, f.Name, f.Type, f.Nullable)
}

func renderInputs(b *strings.Builder, indent, field string, inputs []inputDoc) {
	fmt.Fprintf(b, "%s%s: []contracttest.Input{\n", indent, field)
	for _, in := range inputs {
		fmt.Fprintf(b, "%s\t{Name: %q, Value: %q},\n", indent, in.Name, in.Value)
	}
	fmt.Fprintf(b, "%s},\n", indent)
}

func renderStrings(b *strings.Builder, indent, field string, values []string) {
	fmt.Fprintf(b, "%s%s: []string{\n", indent, field)
	for _, v := range values {
		fmt.Fprintf(b, "%s\t%q,\n", indent, v)
	}
	fmt.Fprintf(b, "%s},\n", indent)
}

func renderTest(f fileDoc, module string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("// Code generated by make gen-capabilities; DO NOT EDIT.\n")
	fmt.Fprintf(&b, "// Source: contracts/capabilities/services/%s.yaml\n\n", f.Stem)
	b.WriteString("package generated\n\n")
	b.WriteString("import (\n\t\"testing\"\n\n")
	fmt.Fprintf(&b, "\t%q\n)\n\n", module+"/internal/contracttest")
	fmt.Fprintf(&b, "// Test%sServiceContract 是 %s 的逐 step 断言骨架。\n", f.GoName, f.ID)
	b.WriteString("// 断言 step 规格值级完整；行为断言写在非生成测试中，不要手改本文件。\n")
	fmt.Fprintf(&b, "func Test%sServiceContract(t *testing.T) {\n", f.GoName)
	fmt.Fprintf(&b, "\tcontracttest.RequireFile(t, %sContract)\n", f.GoName)
	for si, s := range f.Services {
		fmt.Fprintf(&b, "\tt.Run(%q, func(t *testing.T) {\n", s.Name)
		comment(&b, 2, "service: "+s.ID)
		comment(&b, 2, "summary: "+oneLine(s.Summary))
		comment(&b, 2, "visibility: "+s.Visibility)
		comment(&b, 2, "transaction: "+s.Transaction)
		commentList(&b, 2, "params", paramLines(s.Params))
		commentList(&b, 2, "returns", fieldLines(s.Returns))
		for ti, st := range s.Steps {
			fmt.Fprintf(&b, "\t\tt.Run(%q, func(t *testing.T) {\n", st.ID)
			comment(&b, 3, "step: "+st.ID+" ("+st.Kind+")")
			comment(&b, 3, "summary: "+oneLine(st.Summary))
			commentList(&b, 3, "depends_on", st.DependsOn)
			comment(&b, 3, "when: "+orDash(st.When))
			commentList(&b, 3, "input", inputLines(st.Input))
			commentList(&b, 3, "output", fieldLines(st.Output))
			commentList(&b, 3, "queries", queryLines(st.Queries))
			commentList(&b, 3, "errors", st.Errors)
			commentList(&b, 3, "on_error", onErrorLines(st.OnError))
			commentList(&b, 3, "events", st.Events)
			commentList(&b, 3, "rules", st.Rules)
			commentList(&b, 3, "transitions", st.Transitions)
			fmt.Fprintf(&b, "\t\t\tcontracttest.RequireStep(t, %sContract.Services[%d].Steps[%d])\n", f.GoName, si, ti)
			comment(&b, 3, "TODO: 行为断言写在非生成测试中（导入 generated 的规格数据）。")
			b.WriteString("\t\t})\n")
		}
		b.WriteString("\t})\n")
	}
	b.WriteString("}\n")
	return formatSource(b.String(), f.Stem+" test")
}

func comment(b *strings.Builder, depth int, s string) {
	b.WriteString(strings.Repeat("\t", depth))
	b.WriteString("// ")
	b.WriteString(s)
	b.WriteString("\n")
}

func commentList(b *strings.Builder, depth int, label string, lines []string) {
	if len(lines) == 0 {
		comment(b, depth, label+": -")
		return
	}
	comment(b, depth, label+":")
	for _, line := range lines {
		comment(b, depth, "  "+line)
	}
}

func paramLines(params []paramDoc) []string {
	var out []string
	for _, p := range params {
		out = append(out, oneLine(p.Name)+": "+oneLine(p.Type)+nullableSuffix(p.Nullable))
	}
	return out
}

func fieldLines(fields []fieldDoc) []string {
	var out []string
	for _, f := range fields {
		out = append(out, oneLine(f.Name)+": "+oneLine(f.Type)+nullableSuffix(f.Nullable))
	}
	return out
}

func inputLines(inputs []inputDoc) []string {
	var out []string
	for _, in := range inputs {
		out = append(out, oneLine(in.Name)+" <- "+oneLine(orDash(in.Value)))
	}
	return out
}

func queryLines(queries []queryDoc) []string {
	var out []string
	for _, q := range queries {
		out = append(out, oneLine(q.Query))
		for _, in := range q.Input {
			out = append(out, "  "+oneLine(in.Name)+" <- "+oneLine(orDash(in.Value)))
		}
	}
	return out
}

func onErrorLines(items []onErrorDoc) []string {
	var out []string
	for _, e := range items {
		line := oneLine(e.Error) + " -> " + oneLine(e.Action)
		if e.Target != "" {
			line += " " + oneLine(e.Target)
		}
		out = append(out, line)
	}
	return out
}

// oneLine 折叠多行文本，保证注释不会破坏 Go 语法。
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func nullableSuffix(nullable bool) string {
	if nullable {
		return " (nullable)"
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatSource(src, name string) ([]byte, error) {
	out, err := format.Source([]byte(src))
	if err != nil {
		return nil, fmt.Errorf("format %s: %w\n%s", name, err, src)
	}
	return out, nil
}

func goName(owner, file string) string {
	if owner == file {
		return pascal(file)
	}
	return pascal(owner) + pascal(file)
}

func pascal(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]any
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return doc, nil
}

func asMap(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func boolVal(v any) bool {
	b, _ := v.(bool)
	return b
}

func intVal(v any) int {
	i, _ := v.(int)
	return i
}

func strList(v any) []string {
	var out []string
	for _, raw := range asList(v) {
		if s, ok := raw.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-capabilities:", err)
	os.Exit(1)
}

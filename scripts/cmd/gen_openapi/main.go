// gen_openapi 合并 contracts/project.yaml 声明的 HTTP 契约为 OpenAPI 3.1 spec。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
	"gopkg.in/yaml.v3"
)

type project struct {
	Project struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"project"`
	Apps map[string]struct {
		Kind      string   `yaml:"kind"`
		Includes  []string `yaml:"includes"`
		Generated struct {
			Spec   string `yaml:"spec"`
			Server string `yaml:"server"`
		} `yaml:"generated"`
	} `yaml:"apps"`
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify generated spec is up to date")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	specs, err := build(abs)
	if err != nil {
		fail(err)
	}
	for _, path := range sortedBytesKeys(specs) {
		out := specs[path]
		target := filepath.Join(abs, "contracts", path)
		if *check {
			got, err := os.ReadFile(target)
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Fprintf(os.Stderr, "gen-openapi: %s missing; run make gen-openapi\n", target)
				} else {
					fmt.Fprintf(os.Stderr, "gen-openapi: %s: %v\n", target, err)
				}
				os.Exit(1)
			}
			if !bytes.Equal(got, out) {
				fmt.Fprintf(os.Stderr, "gen-openapi: %s is not up to date; run make gen-openapi\n", target)
				os.Exit(1)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(target, out, 0o644); err != nil {
			fail(err)
		}
		fmt.Println("generated", target)
	}
	if *check {
		fmt.Println("openapi: OK")
	}
}

func build(root string) (map[string][]byte, error) {
	pj, err := loadProject(filepath.Join(root, "contracts/project.yaml"))
	if err != nil {
		return nil, err
	}
	httpFiles, err := globYAML(filepath.Join(root, "contracts/capabilities/protocols/http"))
	if err != nil {
		return nil, err
	}
	byID := map[string]map[string]any{}
	for _, file := range httpFiles {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id != "" {
			byID[id] = doc
		}
	}
	typeIdx, err := loadServiceTypes(root)
	if err != nil {
		return nil, err
	}
	enumIdx, err := loadEnums(root)
	if err != nil {
		return nil, err
	}
	entityIdx, err := loadEntities(root, enumIdx)
	if err != nil {
		return nil, err
	}
	out := map[string][]byte{}
	for appName, app := range pj.Apps {
		if app.Kind != "api" || app.Generated.Spec == "" {
			continue
		}
		spec := map[string]any{
			"openapi": "3.1.0",
			"info": map[string]any{
				"title":   pj.Project.Name + " " + appName + " API",
				"version": pj.Project.Version,
			},
			"paths":      map[string]any{},
			"components": map[string]any{},
		}
		paths := spec["paths"].(map[string]any)
		components := spec["components"].(map[string]any)
		opDefaults := map[string]map[string]bool{}
		serverURL := ""
		for _, include := range app.Includes {
			doc := byID[include]
			if doc == nil {
				return nil, fmt.Errorf("app %s: include %s not found", appName, include)
			}
			if base, _ := doc["base_path"].(string); base != "" && serverURL == "" {
				serverURL = base
			}
			for path, rawItem := range asMap(doc["paths"]) {
				for method, rawOp := range asMap(rawItem) {
					op := asMap(rawOp)
					if op == nil {
						continue
					}
					key := path + "\x00" + method
					if opDefaults[key] == nil {
						opDefaults[key] = map[string]bool{}
					}
					for _, raw := range asList(doc["default_error_responses"]) {
						opDefaults[key][fmt.Sprint(raw)] = true
					}
				}
			}
			mergeMap(paths, asMap(doc["paths"]))
			mergeMap(components, asMap(doc["components"]))
		}
		expandSchemas(components, typeIdx, enumIdx, entityIdx)
		if serverURL != "" {
			spec["servers"] = []any{map[string]any{"url": serverURL}}
		}
		responses := asMap(asMap(components["responses"]))
		for _, path := range sortedKeys(paths) {
			item := asMap(paths[path])
			for method, rawOp := range item {
				op := asMap(rawOp)
				if op == nil {
					continue
				}
				resp := asMap(op["responses"])
				if resp == nil {
					resp = map[string]any{}
					op["responses"] = resp
				}
				for status := range opDefaults[path+"\x00"+method] {
					if _, exists := resp[status]; exists {
						continue
					}
					n, ok := toInt(status)
					if !ok {
						return nil, fmt.Errorf("app %s: default status %s is not an integer", appName, status)
					}
					name, ok := openapispec.DefaultResponseName(n)
					if !ok {
						return nil, fmt.Errorf("app %s: default status %d has no response mapping", appName, n)
					}
					if _, ok := responses[name]; !ok {
						return nil, fmt.Errorf("app %s: default error %d needs components.responses.%s", appName, n, name)
					}
					resp[status] = map[string]any{"$ref": "#/components/responses/" + name}
				}
				item[method] = op
			}
			paths[path] = item
		}
		if err := openapispec.Validate(spec); err != nil {
			return nil, fmt.Errorf("app %s: %w", appName, err)
		}
		b, err := marshal(spec)
		if err != nil {
			return nil, err
		}
		out[app.Generated.Spec] = b
	}
	return out, nil
}

func loadProject(path string) (*project, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p project
	if err := yaml.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &p, nil
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}

func globYAML(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() && filepath.Ext(e.Name()) == ".yaml" {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out, nil
}

func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		if sm := asMap(v); sm != nil {
			if dm := asMap(dst[k]); dm != nil {
				mergeMap(dm, sm)
				dst[k] = dm
				continue
			}
		}
		dst[k] = v
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

func asList(v any) []any {
	if l, ok := v.([]any); ok {
		return l
	}
	return nil
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case uint64:
		return int(n), true
	case string:
		parsed, err := strconv.Atoi(n)
		return parsed, err == nil
	}
	return 0, false
}

func sortedBytesKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen-openapi:", err)
	os.Exit(1)
}

func loadServiceTypes(root string) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	files, err := globYAML(filepath.Join(root, "contracts/capabilities/services"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		for name, raw := range asMap(doc["types"]) {
			if schema := asMap(raw); schema != nil {
				out[name] = schema
			}
		}
	}
	return out, nil
}

func loadEnums(root string) (map[string][]any, error) {
	doc, err := loadYAML(filepath.Join(root, "contracts/data/enums.yaml"))
	if err != nil {
		return nil, err
	}
	out := map[string][]any{}
	for _, raw := range asList(doc["enums"]) {
		item := asMap(raw)
		name, _ := item["name"].(string)
		if name == "" {
			continue
		}
		var values []any
		for _, rawValue := range asList(item["values"]) {
			vm := asMap(rawValue)
			if v, _ := vm["value"].(string); v != "" {
				values = append(values, v)
			}
		}
		out[name] = values
	}
	return out, nil
}

func loadEntities(root string, enums map[string][]any) (map[string]map[string]any, error) {
	out := map[string]map[string]any{}
	files, err := globYAML(filepath.Join(root, "contracts/data/entities"))
	if err != nil {
		return nil, err
	}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		id, _ := doc["id"].(string)
		if id == "" {
			continue
		}
		out[lastSegment(id)] = entitySchema(asList(doc["fields"]), enums)
	}
	return out, nil
}

func expandSchemas(components map[string]any, types map[string]map[string]any, enums map[string][]any, entities map[string]map[string]any) {
	schemas := asMap(components["schemas"])
	known := map[string]bool{}
	for name := range schemas {
		known[name] = true
	}
	for _, schema := range schemas {
		if s := asMap(schema); s != nil {
			expandSchema(s, types, enums, entities, known, map[string]bool{})
		}
	}
}

func expandSchema(schema map[string]any, types map[string]map[string]any, enums map[string][]any, entities map[string]map[string]any, known map[string]bool, seen map[string]bool) {
	if ref, _ := schema["x-service-type"].(string); ref != "" {
		name := lastSegment(ref)
		if def := types[name]; def != nil && !seen[name] {
			seen[name] = true
			for k, v := range def {
				schema[k] = v
			}
			expandSchema(schema, types, enums, entities, known, seen)
		}
	}
	if ref, _ := schema["x-enum"].(string); ref != "" {
		schema["type"] = "string"
		schema["enum"] = enums[lastSegment(ref)]
	}
	if ref, _ := schema["x-entity"].(string); ref != "" {
		if def := entities[lastSegment(ref)]; def != nil {
			for k, v := range def {
				schema[k] = v
			}
		}
	}
	if typ, _ := schema["type"].(string); strings.HasPrefix(typ, "data.enums.") {
		schema["type"] = "string"
		schema["enum"] = enums[lastSegment(typ)]
	}
	if typ, _ := schema["type"].(string); strings.HasPrefix(typ, "data.entities.") {
		delete(schema, "type")
		schema["$ref"] = "#/components/schemas/" + pascal(lastSegment(typ))
	}
	if typ, _ := schema["type"].(string); known[typ] {
		delete(schema, "type")
		schema["$ref"] = "#/components/schemas/" + typ
	}
	if items := asMap(schema["items"]); items != nil {
		expandSchema(items, types, enums, entities, known, seen)
	}
	if props := asMap(schema["properties"]); props != nil {
		for _, raw := range props {
			if child := asMap(raw); child != nil {
				expandSchema(child, types, enums, entities, known, seen)
			}
		}
	}
}

func entitySchema(fields []any, enums map[string][]any) map[string]any {
	props := map[string]any{}
	var required []any
	for _, raw := range fields {
		f := asMap(raw)
		if f == nil {
			continue
		}
		if sensitivity, _ := f["sensitivity"].(string); sensitivity == "secret" {
			continue
		}
		name, _ := f["name"].(string)
		jsonName, _ := f["json_name"].(string)
		if jsonName == "" {
			jsonName = snake(name)
		}
		if jsonName == "" {
			continue
		}
		typ, _ := f["type"].(string)
		sch := typeSchema(typ, enums)
		if nullable, _ := f["nullable"].(bool); nullable {
			sch["nullable"] = true
		} else {
			required = append(required, jsonName)
		}
		if desc, _ := f["description"].(string); desc != "" {
			sch["description"] = desc
		}
		props[jsonName] = sch
	}
	out := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties":           props,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func typeSchema(typ string, enums map[string][]any) map[string]any {
	typ = strings.TrimPrefix(typ, "*")
	if strings.HasPrefix(typ, "List<") && strings.HasSuffix(typ, ">") {
		return map[string]any{"type": "array", "items": typeSchema(strings.TrimSuffix(strings.TrimPrefix(typ, "List<"), ">"), enums)}
	}
	switch typ {
	case "string":
		return map[string]any{"type": "string"}
	case "integer":
		return map[string]any{"type": "integer"}
	case "boolean":
		return map[string]any{"type": "boolean"}
	case "timestamp", "datetime":
		return map[string]any{"type": "string", "format": "date-time"}
	case "number", "float", "decimal":
		return map[string]any{"type": "number"}
	}
	if strings.HasPrefix(typ, "data.enums.") {
		return map[string]any{"type": "string", "enum": enums[lastSegment(typ)]}
	}
	return map[string]any{"type": "object"}
}

func lastSegment(ref string) string {
	if i := strings.LastIndex(ref, "."); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func pascal(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		if r == '_' || r == '-' || r == '.' {
			up = true
			continue
		}
		if up {
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func snake(name string) string {
	var out []rune
	for i, r := range name {
		if r >= 'A' && r <= 'Z' {
			if i > 0 {
				out = append(out, '_')
			}
			out = append(out, r+'a'-'A')
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

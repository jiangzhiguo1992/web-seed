// gen_scenarios 把 contracts/scenarios/http/*.yaml 投影为验收清单。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./gen_scenarios -root ../..
//	go run ./gen_scenarios -root ../.. -check
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	openapispec "github.com/yourorg/web-seed/scripts/cmd/internal/spec"
	"gopkg.in/yaml.v3"
)

type httpInfo struct {
	Method     string
	Path       string
	Service    string
	Statuses   []int
	Codes      []string
	Secured    bool
	HasBody    bool
	Deprecated bool
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify manifest is up to date")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	manifest, err := build(abs)
	if err != nil {
		fail(err)
	}
	target := filepath.Join(abs, "contracts/generated/scenarios/manifest.yaml")
	if *check {
		got, err := os.ReadFile(target)
		if err != nil {
			fail(fmt.Errorf("%s: %w (run make gen-scenarios)", target, err))
		}
		if !bytes.Equal(got, manifest) {
			fail(fmt.Errorf("%s is not up to date; run make gen-scenarios", target))
		}
		fmt.Println("scenarios: OK")
		return
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		fail(err)
	}
	if err := os.WriteFile(target, manifest, 0o644); err != nil {
		fail(err)
	}
	fmt.Println("generated", target)
}

func build(root string) ([]byte, error) {
	ops, err := loadHTTPOps(root)
	if err != nil {
		return nil, err
	}
	policy, err := loadPolicy(root)
	if err != nil {
		return nil, err
	}
	files, err := filepath.Glob(filepath.Join(root, "contracts/scenarios/http/*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	type entry struct {
		ID        string         `yaml:"id"`
		Operation string         `yaml:"operation"`
		HTTP      httpManifest   `yaml:"http"`
		Scenario  map[string]any `yaml:"scenario"`
	}
	out := []entry{}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["scenarios"]) {
			scenario := asMap(raw)
			opID, _ := scenario["operation"].(string)
			info, ok := ops[opID]
			if !ok {
				return nil, fmt.Errorf("%s: unknown operation %s", file, opID)
			}
			name, _ := scenario["name"].(string)
			out = append(out, entry{
				ID:        fileID + "." + name,
				Operation: opID,
				HTTP: httpManifest{
					Method:   info.Method,
					Path:     info.Path,
					Service:  info.Service,
					Statuses: effectiveStatuses(info, policy),
					Codes:    info.Codes,
				},
				Scenario: scenario,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	manifest := map[string]any{
		"schema_version": 1,
		"generated_from": "contracts/scenarios/http",
		"scenarios":      out,
	}
	return marshal(manifest)
}

type scenarioPolicy struct {
	Always     []int
	Secured    []int
	Body       []int
	Deprecated []int
}

func loadPolicy(root string) (scenarioPolicy, error) {
	doc, err := loadYAML(filepath.Join(root, "contracts/platform/http_conventions.yaml"))
	if err != nil {
		return scenarioPolicy{}, err
	}
	var policy scenarioPolicy
	responses := asMap(doc["error_responses"])
	for key, target := range map[string]*[]int{
		"always": &policy.Always, "secured": &policy.Secured,
		"body": &policy.Body, "deprecated": &policy.Deprecated,
	} {
		for _, raw := range asList(responses[key]) {
			switch value := raw.(type) {
			case int:
				*target = append(*target, value)
			}
		}
	}
	return policy, nil
}

type httpManifest struct {
	Method   string   `yaml:"method"`
	Path     string   `yaml:"path"`
	Service  string   `yaml:"service"`
	Statuses []int    `yaml:"statuses"`
	Codes    []string `yaml:"codes"`
}

func effectiveStatuses(info httpInfo, policy scenarioPolicy) []int {
	seen := map[int]bool{}
	out := []int{}
	add := func(values []int) {
		for _, value := range values {
			if !seen[value] {
				seen[value] = true
				out = append(out, value)
			}
		}
	}
	add(info.Statuses)
	add(policy.Always)
	if info.Secured {
		add(policy.Secured)
	}
	if info.HasBody {
		add(policy.Body)
	}
	if info.Deprecated {
		add(policy.Deprecated)
	}
	sort.Ints(out)
	return out
}

func loadHTTPOps(root string) (map[string]httpInfo, error) {
	files, err := filepath.Glob(filepath.Join(root, "contracts/capabilities/protocols/http/*.yaml"))
	if err != nil {
		return nil, err
	}
	resolve := func(string) (map[string]any, error) { return map[string]any{"type": "string"}, nil }
	out := map[string]httpInfo{}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, err
		}
		if err := openapispec.NormalizeHTTPDocument(doc, resolve, false); err != nil {
			return nil, err
		}
		for path, rawItem := range asMap(doc["paths"]) {
			for method, rawOp := range asMap(rawItem) {
				op := asMap(rawOp)
				if op == nil {
					continue
				}
				id, _ := op["x-contract-id"].(string)
				service, _ := op["x-service"].(string)
				if id == "" || service == "" {
					continue
				}
				info := httpInfo{Method: method, Path: path, Service: service}
				info.Secured = len(asList(op["security"])) > 0
				info.HasBody = op["requestBody"] != nil
				stability, _ := doc["stability"].(string)
				info.Deprecated = stability == "deprecated"
				for status, rawResp := range asMap(op["responses"]) {
					var n int
					if _, err := fmt.Sscanf(status, "%d", &n); err == nil {
						info.Statuses = append(info.Statuses, n)
					}
					resp := asMap(rawResp)
					for _, code := range asList(resp["x-error-codes"]) {
						if value, ok := code.(string); ok {
							info.Codes = append(info.Codes, strings.TrimPrefix(value, "platform.errors."))
						}
					}
				}
				sort.Ints(info.Statuses)
				sort.Strings(info.Codes)
				out[id] = info
			}
		}
	}
	return out, nil
}

func loadYAML(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw any
	if err := yaml.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	m, ok := normalizeYAMLKeys(raw).(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: top-level map required", path)
	}
	return m, nil
}

func normalizeYAMLKeys(v any) any {
	switch value := v.(type) {
	case map[string]any:
		for key, child := range value {
			value[key] = normalizeYAMLKeys(child)
		}
		return value
	case map[any]any:
		out := map[string]any{}
		for key, child := range value {
			out[fmt.Sprint(key)] = normalizeYAMLKeys(child)
		}
		return out
	case []any:
		for i, child := range value {
			value[i] = normalizeYAMLKeys(child)
		}
		return value
	}
	return v
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
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
	fmt.Fprintln(os.Stderr, "gen-scenarios:", err)
	os.Exit(1)
}

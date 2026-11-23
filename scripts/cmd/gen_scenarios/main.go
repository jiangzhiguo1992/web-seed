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
	CodeStatus map[string]int
	CodeBiz    map[string]int
	Secured    bool
	HasBody    bool
	Deprecated bool
}

func main() {
	root := flag.String("root", "../..", "repo root")
	check := flag.Bool("check", false, "only verify manifest is up to date")
	strict := flag.Bool("strict", false, "fail when an operation/status/code has no scenario")
	flag.Parse()
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	manifest, coverage, err := build(abs)
	if err != nil {
		fail(err)
	}
	if *strict {
		if gaps := coverageGaps(coverage); len(gaps) > 0 {
			fail(fmt.Errorf("scenario coverage gaps:\n%s", strings.Join(gaps, "\n")))
		}
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

type scenarioEntry struct {
	ID        string         `yaml:"id"`
	Operation string         `yaml:"operation"`
	HTTP      httpManifest   `yaml:"http"`
	Scenario  map[string]any `yaml:"scenario"`
}

func build(root string) ([]byte, []coverageEntry, error) {
	ops, err := loadHTTPOps(root)
	if err != nil {
		return nil, nil, err
	}
	policy, err := loadPolicy(root)
	if err != nil {
		return nil, nil, err
	}
	biz, err := loadErrorBizCodes(root)
	if err != nil {
		return nil, nil, err
	}
	for code, status := range biz.status {
		for id, info := range ops {
			if !containsString(info.Codes, code) {
				continue
			}
			info.CodeStatus[code] = status
			info.CodeBiz[code] = biz.code[code]
			ops[id] = info
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "contracts/scenarios/http/*.yaml"))
	if err != nil {
		return nil, nil, err
	}
	sort.Strings(files)
	out := []scenarioEntry{}
	for _, file := range files {
		doc, err := loadYAML(file)
		if err != nil {
			return nil, nil, err
		}
		fileID, _ := doc["id"].(string)
		for _, raw := range asList(doc["scenarios"]) {
			scenario := asMap(raw)
			opID, _ := scenario["operation"].(string)
			info, ok := ops[opID]
			if !ok {
				return nil, nil, fmt.Errorf("%s: unknown operation %s", file, opID)
			}
			name, _ := scenario["name"].(string)
			out = append(out, scenarioEntry{
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
	coverage := buildCoverage(ops, out)
	manifest := map[string]any{
		"schema_version": 1,
		"generated_from": "contracts/scenarios/http",
		"scenarios":      out,
		"coverage":       coverage,
	}
	b, err := marshal(manifest)
	return b, coverage, err
}

func coverageGaps(coverage []coverageEntry) []string {
	var gaps []string
	for _, entry := range coverage {
		if entry.Scenarios == 0 {
			gaps = append(gaps, fmt.Sprintf("%s: no scenario", entry.Operation))
			continue
		}
		for _, status := range entry.MissingStatuses {
			gaps = append(gaps, fmt.Sprintf("%s: no 2xx scenario for %d", entry.Operation, status))
		}
		for _, code := range entry.Codes {
			if !code.Covered {
				gaps = append(gaps, fmt.Sprintf("%s: no scenario for %s (%d)", entry.Operation, code.Code, code.Status))
			}
		}
	}
	return gaps
}

type codeCoverage struct {
	Code    string `yaml:"code"`
	Status  int    `yaml:"status"`
	Covered bool   `yaml:"covered"`
}

type coverageEntry struct {
	Operation       string         `yaml:"operation"`
	Method          string         `yaml:"method"`
	Path            string         `yaml:"path"`
	Service         string         `yaml:"service"`
	Scenarios       int            `yaml:"scenarios"`
	SuccessStatuses []int          `yaml:"success_statuses"`
	CoveredStatuses []int          `yaml:"covered_statuses"`
	MissingStatuses []int          `yaml:"missing_statuses"`
	Codes           []codeCoverage `yaml:"codes"`
}

func buildCoverage(ops map[string]httpInfo, scenarios []scenarioEntry) []coverageEntry {
	type opCoverage struct {
		count    int
		statuses map[int]bool
		bizCodes map[int]bool
	}
	byOp := map[string]*opCoverage{}
	for _, sc := range scenarios {
		cov := byOp[sc.Operation]
		if cov == nil {
			cov = &opCoverage{statuses: map[int]bool{}, bizCodes: map[int]bool{}}
			byOp[sc.Operation] = cov
		}
		cov.count++
		then := asMap(sc.Scenario["then"])
		response := asMap(then["response"])
		if status, ok := intValue(response["status"]); ok {
			cov.statuses[status] = true
		}
		if body := asMap(response["body"]); body != nil {
			if code, ok := intValue(body["code"]); ok {
				cov.bizCodes[code] = true
			}
		}
	}
	ids := make([]string, 0, len(ops))
	for id := range ops {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]coverageEntry, 0, len(ids))
	for _, id := range ids {
		info := ops[id]
		cov := byOp[id]
		entry := coverageEntry{Operation: id, Method: info.Method, Path: info.Path, Service: info.Service}
		if cov != nil {
			entry.Scenarios = cov.count
		}
		for _, status := range info.Statuses {
			if status >= 200 && status < 300 {
				entry.SuccessStatuses = append(entry.SuccessStatuses, status)
				if cov != nil && cov.statuses[status] {
					entry.CoveredStatuses = append(entry.CoveredStatuses, status)
				} else {
					entry.MissingStatuses = append(entry.MissingStatuses, status)
				}
			}
		}
		for _, code := range info.Codes {
			item := codeCoverage{Code: code, Status: info.CodeStatus[code]}
			if cov != nil && cov.statuses[item.Status] && cov.bizCodes[info.CodeBiz[code]] {
				item.Covered = true
			}
			entry.Codes = append(entry.Codes, item)
		}
		out = append(out, entry)
	}
	return out
}

func intValue(v any) (int, bool) {
	switch value := v.(type) {
	case int:
		return value, true
	case int64:
		return int(value), true
	}
	return 0, false
}

type errorBizCodes struct {
	code   map[string]int
	status map[string]int
}

func loadErrorBizCodes(root string) (errorBizCodes, error) {
	out := errorBizCodes{code: map[string]int{}, status: map[string]int{}}
	files, err := filepath.Glob(filepath.Join(root, "contracts/platform/errors/*.yaml"))
	if err != nil {
		return out, err
	}
	for _, file := range files {
		if filepath.Base(file) == "_base.yaml" {
			continue
		}
		doc, err := loadYAML(file)
		if err != nil {
			return out, err
		}
		for _, raw := range asList(doc["errors"]) {
			item := asMap(raw)
			code, _ := item["code"].(string)
			if code == "" {
				continue
			}
			if biz, ok := intValue(item["biz_code"]); ok {
				out.code[code] = biz
			}
			if status, ok := intValue(item["status"]); ok {
				out.status[code] = status
			}
		}
	}
	return out, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

type scenarioPolicy = openapispec.HTTPPolicy

func loadPolicy(root string) (scenarioPolicy, error) {
	return openapispec.LoadHTTPPolicy(root)
}

type httpManifest struct {
	Method   string   `yaml:"method"`
	Path     string   `yaml:"path"`
	Service  string   `yaml:"service"`
	Statuses []int    `yaml:"statuses"`
	Codes    []string `yaml:"codes"`
}

func effectiveStatuses(info httpInfo, policy scenarioPolicy) []int {
	return policy.EffectiveStatuses(info.Statuses, info.Secured, info.HasBody, info.Deprecated)
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
				info := httpInfo{Method: method, Path: path, Service: service, CodeStatus: map[string]int{}, CodeBiz: map[string]int{}}
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
					for _, raw := range asList(resp["x-error-codes"]) {
						code, _ := raw.(string)
						short := strings.TrimPrefix(code, "platform.errors.")
						if short != "" {
							info.Codes = append(info.Codes, short)
							info.CodeStatus[short] = n
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

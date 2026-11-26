// coverage_check 读取 contracts/platform/quality.yaml 的覆盖率门槛，
// 对 Go cover profile 做总语句覆盖率比较；只用于 CI/本地门槛，不生成产物。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type qualityDoc struct {
	Coverage map[string]any `yaml:"coverage"`
}

func main() {
	root := flag.String("root", "../..", "repo root")
	profile := flag.String("profile", "", "go cover profile path")
	kind := flag.String("kind", "api", "coverage kind: api or web")
	flag.Parse()
	if *profile == "" {
		fail(fmt.Errorf("-profile is required"))
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	threshold, err := loadThreshold(abs, *kind)
	if err != nil {
		fail(err)
	}
	got, err := totalCoverage(*profile)
	if err != nil {
		fail(err)
	}
	if got < float64(threshold) {
		fail(fmt.Errorf("coverage %s %.1f%% is below quality.yaml threshold %d%%", *kind, got, threshold))
	}
	fmt.Printf("coverage %s: %.1f%% >= %d%%\n", *kind, got, threshold)
}

func loadThreshold(root, kind string) (int, error) {
	path := filepath.Join(root, "contracts/platform/quality.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var doc qualityDoc
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return 0, fmt.Errorf("%s: %w", path, err)
	}
	part, _ := doc.Coverage[kind].(map[string]any)
	line, _ := part["line"].(int)
	if line <= 0 {
		return 0, fmt.Errorf("%s: coverage.%s.line is required", path, kind)
	}
	return line, nil
}

// totalCoverage 返回 cover profile 的总语句覆盖率百分比。
func totalCoverage(path string) (float64, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	total, covered := 0, 0
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			return 0, fmt.Errorf("%s: malformed cover line %q", path, line)
		}
		statements, err := strconv.Atoi(fields[len(fields)-2])
		if err != nil {
			return 0, fmt.Errorf("%s: malformed statement count %q", path, fields[len(fields)-2])
		}
		count, err := strconv.Atoi(fields[len(fields)-1])
		if err != nil {
			return 0, fmt.Errorf("%s: malformed hit count %q", path, fields[len(fields)-1])
		}
		total += statements
		if count > 0 {
			covered += statements
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("%s: no cover statements", path)
	}
	return float64(covered) * 100 / float64(total), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "coverage-check:", err)
	os.Exit(1)
}

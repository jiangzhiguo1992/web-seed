// contract_compat 检查枚举契约的 append-only 兼容性：
// 基线中的枚举名、value、go_type、ts_type 不得删除或改名，value 的 code 不得修改；
// 新增 value/code 允许。
//
// 用法（在 scripts/cmd 下）：
//
//	go run ./contract_compat -root ../.. -ref HEAD^1
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type enumValue struct {
	Value string `yaml:"value"`
	Code  int    `yaml:"code"`
}

type enumDef struct {
	Name   string      `yaml:"name"`
	GoType string      `yaml:"go_type"`
	TSType string      `yaml:"ts_type"`
	Values []enumValue `yaml:"values"`
}

type enumDoc struct {
	Enums []enumDef `yaml:"enums"`
}

// enumInfo 是单个枚举的扁平视图。
type enumInfo struct {
	Values map[string]int
	GoType string
	TSType string
}

// enumSet 是 enum name -> enumInfo 的扁平视图。
type enumSet map[string]enumInfo

func main() {
	root := flag.String("root", "../..", "repo root")
	ref := flag.String("ref", "", "baseline git ref (empty skips the check)")
	staged := flag.Bool("staged", false, "compare the staged index instead of the working tree")
	flag.Parse()

	if strings.TrimSpace(*ref) == "" {
		if *staged {
			fail(fmt.Errorf("-staged requires -ref"))
		}
		fmt.Println("enum append-only: skipped (no baseline ref)")
		return
	}
	abs, err := filepath.Abs(*root)
	if err != nil {
		fail(err)
	}
	before, err := loadBaseline(abs, *ref)
	if err != nil {
		fail(err)
	}
	var after enumSet
	if *staged {
		after, err = loadCurrentIndex(abs)
	} else {
		after, err = loadCurrent(abs)
	}
	if err != nil {
		fail(err)
	}
	violations := compareEnums(before, after)
	if len(violations) > 0 {
		for _, violation := range violations {
			fmt.Fprintln(os.Stderr, "enum append-only:", violation)
		}
		os.Exit(1)
	}
	fmt.Println("enum append-only: OK")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "contract-compat:", err)
	os.Exit(1)
}

// loadBaseline 从 git ref 读取 contracts/data/enums 下的全部枚举文件。
func loadBaseline(root, ref string) (enumSet, error) {
	files, err := gitOutput(root, "ls-tree", "-r", "--name-only", ref, "--", "contracts/data/enums")
	if err != nil {
		return nil, fmt.Errorf("list baseline enums at %s: %w", ref, err)
	}
	out := enumSet{}
	for _, rel := range strings.Split(strings.TrimSpace(string(files)), "\n") {
		if rel == "" {
			continue
		}
		b, err := gitOutput(root, "show", ref+":"+rel)
		if err != nil {
			return nil, fmt.Errorf("read %s at %s: %w", rel, ref, err)
		}
		if err := mergeEnums(out, b); err != nil {
			return nil, fmt.Errorf("%s at %s: %w", rel, ref, err)
		}
	}
	return out, nil
}

// loadCurrentIndex 读取 index（已暂存）中的枚举文件；pre-commit 使用。
func loadCurrentIndex(root string) (enumSet, error) {
	files, err := gitOutput(root, "ls-files", "-z", "--", "contracts/data/enums")
	if err != nil {
		return nil, err
	}
	out := enumSet{}
	for _, rel := range strings.Split(string(files), "\x00") {
		if rel == "" {
			continue
		}
		b, err := gitOutput(root, "show", ":"+rel)
		if err != nil {
			return nil, fmt.Errorf("read staged %s: %w", rel, err)
		}
		if err := mergeEnums(out, b); err != nil {
			return nil, fmt.Errorf("%s (staged): %w", rel, err)
		}
	}
	return out, nil
}

func loadCurrent(root string) (enumSet, error) {
	files, err := filepath.Glob(filepath.Join(root, "contracts/data/enums/*.yaml"))
	if err != nil {
		return nil, err
	}
	out := enumSet{}
	for _, file := range files {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, err
		}
		if err := mergeEnums(out, b); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
	}
	return out, nil
}

func mergeEnums(dst enumSet, data []byte) error {
	var doc enumDoc
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return err
	}
	for _, enum := range doc.Enums {
		if enum.Name == "" {
			continue
		}
		info := dst[enum.Name]
		if info.Values == nil {
			info.Values = map[string]int{}
		}
		if enum.GoType != "" {
			info.GoType = enum.GoType
		}
		if enum.TSType != "" {
			info.TSType = enum.TSType
		}
		for _, value := range enum.Values {
			if value.Value != "" {
				info.Values[value.Value] = value.Code
			}
		}
		dst[enum.Name] = info
	}
	return nil
}

// compareEnums 返回 append-only 违规：删除枚举、删除 value、修改 code。
func compareEnums(before, after enumSet) []string {
	var violations []string
	names := make([]string, 0, len(before))
	for name := range before {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		beforeEnum := before[name]
		current, ok := after[name]
		if !ok {
			violations = append(violations, fmt.Sprintf("enum %s was removed", name))
			continue
		}
		if beforeEnum.GoType != "" && current.GoType != beforeEnum.GoType {
			violations = append(violations, fmt.Sprintf("enum %s go_type changed from %s to %s", name, beforeEnum.GoType, current.GoType))
		}
		if beforeEnum.TSType != "" && current.TSType != beforeEnum.TSType {
			violations = append(violations, fmt.Sprintf("enum %s ts_type changed from %s to %s", name, beforeEnum.TSType, current.TSType))
		}
		values := make([]string, 0, len(beforeEnum.Values))
		for value := range beforeEnum.Values {
			values = append(values, value)
		}
		sort.Strings(values)
		for _, value := range values {
			code, ok := current.Values[value]
			if !ok {
				violations = append(violations, fmt.Sprintf("enum %s value %s was removed", name, value))
				continue
			}
			if code != beforeEnum.Values[value] {
				violations = append(violations, fmt.Sprintf("enum %s value %s code changed from %d to %d", name, value, beforeEnum.Values[value], code))
			}
		}
	}
	return violations
}

func gitOutput(root string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, err
	}
	return out, nil
}

// policy.go 是 platform/http_conventions.yaml 的唯一加载点，避免 checker 与生成器各自解析。
package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// HTTPPolicy 是平台 HTTP 基线：调用端与按条件注入的错误状态。
type HTTPPolicy struct {
	Audiences        []string
	Always           []int
	Secured          []int
	Body             []int
	Deprecated       []int
	SuccessCodes     []int
	ClientErrorCodes []int
	ServerErrorCodes []int
}

// LoadHTTPPolicy 读取 contracts/platform/http_conventions.yaml。
func LoadHTTPPolicy(root string) (HTTPPolicy, error) {
	path := filepath.Join(root, "contracts/platform/http_conventions.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		return HTTPPolicy{}, err
	}
	var doc struct {
		Audiences      []string `yaml:"audiences"`
		ErrorResponses struct {
			Always     []int `yaml:"always"`
			Secured    []int `yaml:"secured"`
			Body       []int `yaml:"body"`
			Deprecated []int `yaml:"deprecated"`
		} `yaml:"error_responses"`
		StatusCodes struct {
			Success     []int `yaml:"success"`
			ClientError []int `yaml:"client_error"`
			ServerError []int `yaml:"server_error"`
		} `yaml:"status_codes"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return HTTPPolicy{}, fmt.Errorf("%s: %w", path, err)
	}
	return HTTPPolicy{
		Audiences:        doc.Audiences,
		Always:           doc.ErrorResponses.Always,
		Secured:          doc.ErrorResponses.Secured,
		Body:             doc.ErrorResponses.Body,
		Deprecated:       doc.ErrorResponses.Deprecated,
		SuccessCodes:     doc.StatusCodes.Success,
		ClientErrorCodes: doc.StatusCodes.ClientError,
		ServerErrorCodes: doc.StatusCodes.ServerError,
	}, nil
}

// EffectiveStatuses 合并 operation 自身状态与平台基线状态。
func (p HTTPPolicy) EffectiveStatuses(base []int, secured, hasBody, deprecated bool) []int {
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
	add(base)
	add(p.Always)
	if secured {
		add(p.Secured)
	}
	if hasBody {
		add(p.Body)
	}
	if deprecated {
		add(p.Deprecated)
	}
	sort.Ints(out)
	return out
}

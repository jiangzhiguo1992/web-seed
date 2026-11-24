// policy.go 是 platform/http_conventions.yaml 的唯一加载点，避免 checker 与生成器各自解析。
package spec

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// HTTPPolicy 是平台 HTTP 基线：调用端、按条件注入的错误状态与平台组件。
type HTTPPolicy struct {
	Audiences        []string
	Always           []int
	Secured          []int
	Body             []int
	Deprecated       []int
	SuccessCodes     []int
	ClientErrorCodes []int
	ServerErrorCodes []int
	TransportHeaders []string
	BodyTypes        map[string]string
	Components       map[string]any
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
		TransportHeaders []string       `yaml:"transport_headers"`
		BodyTypes        map[string]any `yaml:"body_types"`
		Components       map[string]any `yaml:"components"`
	}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return HTTPPolicy{}, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	for _, name := range doc.TransportHeaders {
		if !validHeaderToken(name) {
			return HTTPPolicy{}, fmt.Errorf("%s: transport_headers contains invalid header token %q", path, name)
		}
		key := strings.ToLower(name)
		if seen[key] {
			return HTTPPolicy{}, fmt.Errorf("%s: transport_headers contains duplicate header %q", path, name)
		}
		seen[key] = true
	}
	bodyTypes := map[string]string{}
	mediaOwners := map[string]string{}
	for kind, raw := range doc.BodyTypes {
		value, ok := raw.(string)
		if !ok {
			return HTTPPolicy{}, fmt.Errorf("%s: body_types.%s must be a string", path, kind)
		}
		if kind == "note" {
			continue
		}
		if kind == "" || strings.TrimSpace(value) == "" {
			return HTTPPolicy{}, fmt.Errorf("%s: body_types.%s must be non-empty", path, kind)
		}
		if owner, exists := mediaOwners[value]; exists {
			return HTTPPolicy{}, fmt.Errorf("%s: body_types.%s and body_types.%s share media type %s", path, owner, kind, value)
		}
		mediaOwners[value] = kind
		bodyTypes[kind] = value
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
		TransportHeaders: doc.TransportHeaders,
		BodyTypes:        bodyTypes,
		Components:       doc.Components,
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

func validHeaderToken(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

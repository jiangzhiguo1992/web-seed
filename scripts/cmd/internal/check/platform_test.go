package check

import (
	"strings"
	"testing"
)

func environmentDoc(differences []any) map[string]any {
	return map[string]any{
		"environments": []any{
			map[string]any{"name": "dev", "debug": true, "secrets": "same"},
			map[string]any{"name": "prod", "debug": false, "secrets": "same"},
		},
		"differences":    differences,
		"secrets_source": map[string]any{"dev": ".env", "prod": "vault"},
	}
}

func TestEnvironmentDifferences(t *testing.T) {
	debug := map[string]any{"key": "debug", "environments": []any{"dev", "prod"}}
	c := &Checker{}
	c.checkEnvironmentShape("x.yaml", environmentDoc([]any{debug}))
	if len(c.errs) != 0 {
		t.Fatalf("valid differences reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkEnvironmentShape("x.yaml", environmentDoc(nil))
	if !strings.Contains(strings.Join(c.errs, "\n"), "environment difference debug is not registered") {
		t.Fatalf("missing difference not reported: %v", c.errs)
	}

	c = &Checker{}
	c.checkEnvironmentShape("x.yaml", environmentDoc([]any{map[string]any{"key": "noop", "environments": []any{"dev"}}}))
	if !strings.Contains(strings.Join(c.errs, "\n"), "differences key noop is not actually different") {
		t.Fatalf("fake difference not reported: %v", c.errs)
	}
}

func TestEnvironmentHTTPS(t *testing.T) {
	doc := map[string]any{
		"environments": []any{
			map[string]any{
				"name": "prod", "api_base_url": "http://api.example.com",
				"web_base_url": "https://admin.example.com",
				"security":     map[string]any{"require_https": true},
			},
		},
		"secrets_source": map[string]any{"prod": "vault"},
	}
	c := &Checker{}
	c.checkEnvironmentShape("x.yaml", doc)
	if !strings.Contains(strings.Join(c.errs, "\n"), "api_base_url must use https") {
		t.Fatalf("http base url not reported: %v", c.errs)
	}
	if strings.Contains(strings.Join(c.errs, "\n"), "web_base_url must use https") {
		t.Fatalf("https web base url reported: %v", c.errs)
	}
}

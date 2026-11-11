package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AppEnv != "dev" {
		t.Fatalf("AppEnv = %q, want dev", cfg.AppEnv)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Fatalf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestLoad_InvalidAppEnv(t *testing.T) {
	t.Setenv("APP_ENV", "prodution") // 拼写错误必须被拦截
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid APP_ENV error")
	}
}

func TestLoad_ValidAppEnv(t *testing.T) {
	for _, env := range []string{"dev", "test", "staging", "prod"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.AppEnv != env {
				t.Fatalf("AppEnv = %q, want %q", cfg.AppEnv, env)
			}
		})
	}
}

func TestLoad_HTTPAddrOverride(t *testing.T) {
	t.Setenv("HTTP_ADDR", "127.0.0.1:9090")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9090" {
		t.Fatalf("HTTPAddr = %q, want 127.0.0.1:9090", cfg.HTTPAddr)
	}
}

package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("HTTP_ADDR", "")
	t.Setenv("ACCOUNT_NOTIFY_ENDPOINT", "")
	t.Setenv("ACCOUNT_NOTIFY_TIMEOUT", "")

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
	if cfg.AccountNotifyEndpoint != "" {
		t.Fatalf("AccountNotifyEndpoint = %q, want empty", cfg.AccountNotifyEndpoint)
	}
	if cfg.AccountNotifyTimeout.String() != "10s" {
		t.Fatalf("AccountNotifyTimeout = %s, want 10s", cfg.AccountNotifyTimeout)
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

func TestLoad_AccountNotifyOverride(t *testing.T) {
	t.Setenv("ACCOUNT_NOTIFY_ENDPOINT", "http://notify.example/deleted")
	t.Setenv("ACCOUNT_NOTIFY_TIMEOUT", "3s")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.AccountNotifyEndpoint != "http://notify.example/deleted" {
		t.Fatalf("AccountNotifyEndpoint = %q", cfg.AccountNotifyEndpoint)
	}
	if cfg.AccountNotifyTimeout.String() != "3s" {
		t.Fatalf("AccountNotifyTimeout = %s, want 3s", cfg.AccountNotifyTimeout)
	}
}

func TestLoad_InvalidAccountNotifyTimeout(t *testing.T) {
	t.Setenv("ACCOUNT_NOTIFY_TIMEOUT", "soon")
	if _, err := Load(); err == nil {
		t.Fatal("Load() error = nil, want invalid ACCOUNT_NOTIFY_TIMEOUT error")
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

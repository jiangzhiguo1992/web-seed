// Package config 负责进程配置的加载与校验。
//
// 设计决策：
//   - 12-factor：只从环境变量读取，不从文件读
//   - fail-fast：非法值直接返回错误，由 main 终止进程
//   - P2 起由 contracts/platform/config/env.json 作为真相源，测试断言 key 集合一致
//
// 职责边界：
//   - 只做读取与校验，不持有全局状态
//   - 不感知业务
//
// 使用示例：
//
//	cfg, err := config.Load()
//	if err != nil {
//		log.Error("load config", "err", err)
//		os.Exit(1)
//	}
package config

import (
	"fmt"
	"os"
	"time"
)

// Config 是进程启动所需的全部配置。
type Config struct {
	AppEnv                string
	HTTPAddr              string
	AccountNotifyEndpoint string
	AccountNotifyTimeout  time.Duration
}

// Load 读取并校验环境变量，缺失项使用开发默认值。
func Load() (Config, error) {
	cfg := Config{
		AppEnv:                getenv("APP_ENV", "dev"),
		HTTPAddr:              getenv("HTTP_ADDR", ":8080"),
		AccountNotifyEndpoint: getenv("ACCOUNT_NOTIFY_ENDPOINT", ""),
	}
	switch cfg.AppEnv {
	case "dev", "test", "staging", "prod":
	default:
		return Config{}, fmt.Errorf("config: invalid APP_ENV %q", cfg.AppEnv)
	}
	rawTimeout := getenv("ACCOUNT_NOTIFY_TIMEOUT", "10s")
	timeout, err := time.ParseDuration(rawTimeout)
	if err != nil || timeout <= 0 {
		return Config{}, fmt.Errorf("config: invalid ACCOUNT_NOTIFY_TIMEOUT %q", rawTimeout)
	}
	cfg.AccountNotifyTimeout = timeout
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

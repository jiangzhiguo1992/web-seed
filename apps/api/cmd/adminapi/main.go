// Command adminapi 启动面向管理端的 HTTP API。
//
// 只做四件事：初始化日志 → 加载配置 → 构造应用 → 等待信号并优雅关闭。
// 任何业务逻辑都不允许出现在 main 中。
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/yourorg/web-seed/apps/api/internal/composition/adminapi"
	"github.com/yourorg/web-seed/apps/api/internal/platform/config"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	cfg, err := config.Load()
	if err != nil {
		log.Error("load config", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := adminapi.New(cfg, log).Run(ctx); err != nil {
		log.Error("run app", "err", err)
		os.Exit(1)
	}
	log.Info("stopped", "app", "adminapi")
}

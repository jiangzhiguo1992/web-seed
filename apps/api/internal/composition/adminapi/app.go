// Package adminapi 是面向管理端的 API 应用装配层。
//
// 设计决策：
//   - 只做"选择模块 + 挂路由 + 配中间件 + 聚合 handler"
//   - 业务能力全部来自 internal/modules，本层不写业务分支
//   - P2 起 handlers/ 子包实现生成契约的 StrictServerInterface
//
// 职责边界：
//   - 不直接访问数据库
//   - 不做跨模块编排（编排属于模块 service 或独立 usecase）
package adminapi

import (
	"context"
	"log/slog"

	"github.com/yourorg/web-seed/apps/api/internal/platform/config"
	"github.com/yourorg/web-seed/apps/api/internal/platform/httpserver"
)

// App 持有本应用的依赖，生命周期与进程一致。
type App struct {
	cfg config.Config
	log *slog.Logger
}

// New 构造应用。P2 起在此注入 modules 与 handlers。
func New(cfg config.Config, log *slog.Logger) *App {
	return &App{cfg: cfg, log: log}
}

// Run 启动应用并阻塞到 ctx 取消。
func (a *App) Run(ctx context.Context) error {
	return httpserver.Run(ctx, httpserver.Options{
		Addr: a.cfg.HTTPAddr,
		Log:  a.log,
	})
}

// Package account 是 cap.services.account 的最小实现。
//
// 当前只实现 get：Repository 可替换为 sqlc/DB 实现，Get 的契约签名与
// cap.services.account.get 保持一致。Actor 由上层中间件注入 ctx；
// 没有 actor 时视为系统调用（跳过 self_only）。
package account

import (
	"context"
	"errors"

	"github.com/yourorg/web-seed/apps/api/internal/generated/model"
)

// Account 是 data.entities.account 的生成标量投影；关系字段按需加载，当前不投影。
type Account = model.Account

// Error 携带 platform.errors.* 错误码。
type Error struct {
	Code string
}

func (e *Error) Error() string { return e.Code }

var (
	ErrNotFound  = &Error{Code: "platform.errors.ACCOUNT_NOT_FOUND"}
	ErrForbidden = &Error{Code: "platform.errors.COMMON_FORBIDDEN"}
)

// Actor 是 ctx 注入的调用者；Admin 跳过 self_only。
type Actor struct {
	ID    int64
	Admin bool
}

type actorKey struct{}

// WithActor 把调用者写入 ctx；由 HTTP/认证中间件负责。
func WithActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

func actorFrom(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey{}).(Actor)
	return actor, ok
}

// Repository 是 data.queries.account.* 的 Go 边界；当前只声明 get 用到的查询。
type Repository interface {
	GetAccount(ctx context.Context, id int64) (Account, error)
}

// DeleteNotifier 是 cap.services.account.notify_deleted 的投递边界。
// 返回 error 时由异步队列按 dispatch retry/dead_letter 策略重试。
type DeleteNotifier interface {
	NotifyDeleted(ctx context.Context, accountID int64, deletedAt int64) error
}

// ErrNotifierUnavailable 表示未装配注销通知投递器。
var ErrNotifierUnavailable = errors.New("account: delete notifier is not configured")

// Service 实现 cap.services.account.* 中已声明的能力。
type Service struct {
	repo     Repository
	notifier DeleteNotifier
}

func New(repo Repository, notifier DeleteNotifier) *Service {
	return &Service{repo: repo, notifier: notifier}
}

// Get 对应 cap.services.account.get/load：读账号、软删视为不存在、执行 self_only。
func (s *Service) Get(ctx context.Context, id int64) (Account, error) {
	a, err := s.repo.GetAccount(ctx, id)
	if err != nil {
		return Account{}, err
	}
	if a.DeletedAt != nil {
		return Account{}, ErrNotFound
	}
	if actor, ok := actorFrom(ctx); ok && !actor.Admin {
		if a.UserID == nil || *a.UserID != actor.ID {
			return Account{}, ErrForbidden
		}
	}
	return a, nil
}

// NotifyDeleted 对应 cap.services.account.notify_deleted/notify。
// 投递失败直接返回 error，交给 dispatch 的 retry/dead_letter 策略处理。
func (s *Service) NotifyDeleted(ctx context.Context, accountID int64, deletedAt int64) error {
	if s.notifier == nil {
		return ErrNotifierUnavailable
	}
	return s.notifier.NotifyDeleted(ctx, accountID, deletedAt)
}

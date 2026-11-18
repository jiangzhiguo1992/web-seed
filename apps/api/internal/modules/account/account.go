// Package account 是 cap.services.account 的最小实现。
//
// 当前只实现 get：Repository 可替换为 sqlc/DB 实现，Get 的契约签名与
// cap.services.account.get 保持一致。Actor 由上层中间件注入 ctx；
// 没有 actor 时视为系统调用（跳过 self_only）。
package account

import (
	"context"
	"time"
)

// Account 是 data.entities.account 的标量投影；关系字段按需加载，当前不投影。
type Account struct {
	ID        int64
	Status    int32
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
	OwnType   int32
	OwnID     int64
	Number    string
	Nickname  string
	UserID    *int64
}

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

// Service 实现 cap.services.account.get。
type Service struct {
	repo Repository
}

func New(repo Repository) *Service {
	return &Service{repo: repo}
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

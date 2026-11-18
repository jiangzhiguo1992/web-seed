package account_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yourorg/web-seed/apps/api/internal/contracttest"
	"github.com/yourorg/web-seed/apps/api/internal/contracttest/generated"
	"github.com/yourorg/web-seed/apps/api/internal/modules/account"
)

// memoryRepo 是 Repository 的最小测试实现；替换为 sqlc 实现时测试保持。
type memoryRepo struct {
	accounts map[int64]account.Account
}

func newMemoryRepo(accounts ...account.Account) *memoryRepo {
	m := make(map[int64]account.Account, len(accounts))
	for _, a := range accounts {
		m[a.ID] = a
	}
	return &memoryRepo{accounts: m}
}

func (r *memoryRepo) GetAccount(_ context.Context, id int64) (account.Account, error) {
	a, ok := r.accounts[id]
	if !ok {
		return account.Account{}, account.ErrNotFound
	}
	return a, nil
}

func serviceSpec(t *testing.T, id string) contracttest.Service {
	t.Helper()
	for _, s := range generated.AccountContract.Services {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("service %s not found in contract", id)
	return contracttest.Service{}
}

func TestGetBehaviour(t *testing.T) {
	spec := serviceSpec(t, "cap.services.account.get")
	if len(spec.Steps) != 1 {
		t.Fatalf("get steps = %d, want 1", len(spec.Steps))
	}
	load := spec.Steps[0]

	now := time.Now()
	userID := int64(5)
	otherUserID := int64(6)
	svc := account.New(newMemoryRepo(
		account.Account{ID: 1, UserID: &userID, Number: "ACC-0001", Nickname: "one", CreatedAt: now, UpdatedAt: now},
		account.Account{ID: 2, UserID: &userID, DeletedAt: &now},
		account.Account{ID: 3, UserID: &otherUserID},
	))

	t.Run("found", func(t *testing.T) {
		got, err := svc.Get(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != 1 || got.Number != "ACC-0001" {
			t.Fatalf("got %+v", got)
		}
	})

	t.Run("not_found", func(t *testing.T) {
		_, err := svc.Get(context.Background(), 99)
		requireCode(t, load, err, "platform.errors.ACCOUNT_NOT_FOUND")
	})

	t.Run("soft_deleted_is_not_found", func(t *testing.T) {
		_, err := svc.Get(context.Background(), 2)
		requireCode(t, load, err, "platform.errors.ACCOUNT_NOT_FOUND")
	})

	t.Run("self_only", func(t *testing.T) {
		ctx := account.WithActor(context.Background(), account.Actor{ID: userID})
		_, err := svc.Get(ctx, 3)
		requireCode(t, load, err, "platform.errors.COMMON_FORBIDDEN")
	})

	t.Run("admin_skips_self_only", func(t *testing.T) {
		ctx := account.WithActor(context.Background(), account.Actor{ID: userID, Admin: true})
		if _, err := svc.Get(ctx, 3); err != nil {
			t.Fatal(err)
		}
	})
}

// requireCode 同时断言错误码和“该错误已由契约 step.Errors 声明”。
func requireCode(t *testing.T, step contracttest.Step, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want %s, got nil", code)
	}
	var e *account.Error
	if !errors.As(err, &e) {
		t.Fatalf("want *account.Error, got %T", err)
	}
	if e.Code != code {
		t.Fatalf("code = %s, want %s", e.Code, code)
	}
	declared := false
	for _, want := range step.Errors {
		if want == code {
			declared = true
			break
		}
	}
	if !declared {
		t.Fatalf("error %s is not declared by step.Errors %v", code, step.Errors)
	}
}

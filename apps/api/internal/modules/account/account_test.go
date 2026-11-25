package account

import (
	"context"
	"errors"
	"testing"
)

type fakeNotifier struct {
	accountID int64
	deletedAt int64
	err       error
}

func (f *fakeNotifier) NotifyDeleted(_ context.Context, accountID int64, deletedAt int64) error {
	f.accountID = accountID
	f.deletedAt = deletedAt
	return f.err
}

func TestNotifyDeletedDelegatesToNotifier(t *testing.T) {
	deletedAt := int64(1763985600)
	n := &fakeNotifier{}
	s := &Service{notifier: n}
	if err := s.NotifyDeleted(context.Background(), 42, deletedAt); err != nil {
		t.Fatal(err)
	}
	if n.accountID != 42 || n.deletedAt != deletedAt {
		t.Fatalf("notifier got (%d, %d)", n.accountID, n.deletedAt)
	}
}

func TestNotifyDeletedPropagatesFailure(t *testing.T) {
	want := errors.New("webhook failed")
	s := &Service{notifier: &fakeNotifier{err: want}}
	if err := s.NotifyDeleted(context.Background(), 42, 1); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}

func TestNotifyDeletedRequiresNotifier(t *testing.T) {
	s := &Service{}
	if err := s.NotifyDeleted(context.Background(), 42, 1); !errors.Is(err, ErrNotifierUnavailable) {
		t.Fatalf("err = %v, want ErrNotifierUnavailable", err)
	}
}

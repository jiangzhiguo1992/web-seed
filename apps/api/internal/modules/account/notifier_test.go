package account

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHTTPNotifierPostsPayload(t *testing.T) {
	deletedAt := time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)
	var got deletePayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if err := NewHTTPNotifier(srv.URL).NotifyDeleted(context.Background(), 42, deletedAt); err != nil {
		t.Fatal(err)
	}
	if got.AccountID != 42 || !got.DeletedAt.Equal(deletedAt) {
		t.Fatalf("payload = %#v", got)
	}
}

func TestHTTPNotifierFailureStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	err := NewHTTPNotifier(srv.URL).NotifyDeleted(context.Background(), 42, time.Now())
	if err == nil {
		t.Fatal("expected error for non-2xx status")
	}
}

func TestHTTPNotifierRequiresEndpoint(t *testing.T) {
	err := NewHTTPNotifier("").NotifyDeleted(context.Background(), 42, time.Now())
	if !errors.Is(err, ErrNotifierUnavailable) {
		t.Fatalf("err = %v, want ErrNotifierUnavailable", err)
	}
}

package account

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// HTTPNotifier 是 cap.services.account.notify_deleted 的 HTTP webhook 投递实现。
// 非 2xx 返回 error，由 dispatch 的 retry/dead_letter 策略重试或转死信。
type HTTPNotifier struct {
	Endpoint string
	Client   *http.Client
}

// NewHTTPNotifier 构造 webhook 投递器；endpoint 为空时投递失败。
func NewHTTPNotifier(endpoint string) *HTTPNotifier {
	return &HTTPNotifier{Endpoint: endpoint, Client: &http.Client{Timeout: 10 * time.Second}}
}

type deletePayload struct {
	AccountID int64 `json:"account_id"`
	DeletedAt int64 `json:"deleted_at"`
}

func (n *HTTPNotifier) NotifyDeleted(ctx context.Context, accountID int64, deletedAt int64) error {
	if n.Endpoint == "" {
		return ErrNotifierUnavailable
	}
	body, err := json.Marshal(deletePayload{AccountID: accountID, DeletedAt: deletedAt})
	if err != nil {
		return fmt.Errorf("account: encode delete notification: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, n.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("account: build delete notification: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	client := n.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("account: send delete notification: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("account: delete notification rejected with status %d", resp.StatusCode)
	}
	return nil
}

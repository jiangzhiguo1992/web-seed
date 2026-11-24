package adminapi

import (
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/yourorg/web-seed/apps/api/internal/modules/account"
	"github.com/yourorg/web-seed/apps/api/internal/platform/config"
)

func TestNewWiresAccountNotifier(t *testing.T) {
	app := New(config.Config{
		AccountNotifyEndpoint: "http://notify.example/deleted",
		AccountNotifyTimeout:  3 * time.Second,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	n, ok := app.AccountNotifier.(*account.HTTPNotifier)
	if !ok {
		t.Fatalf("AccountNotifier = %T, want *account.HTTPNotifier", app.AccountNotifier)
	}
	if n.Endpoint != "http://notify.example/deleted" {
		t.Fatalf("Endpoint = %q", n.Endpoint)
	}
	if n.Client.Timeout != 3*time.Second {
		t.Fatalf("Timeout = %s, want 3s", n.Client.Timeout)
	}
}

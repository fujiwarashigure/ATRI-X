package app

import (
	"context"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
	"github.com/fujiwarashigure/ATRI-X/internal/signal"
)

type fakeConnectPlatform struct {
	name     string
	notifier func(context.Context, string)
}

func (p *fakeConnectPlatform) Name() string { return p.name }

func (p *fakeConnectPlatform) Run(context.Context, platform.PlatformHandler) error { return nil }

func (p *fakeConnectPlatform) SendChat(context.Context, []delivery.Output) (delivery.Receipt, error) {
	return delivery.Receipt{}, nil
}

func (p *fakeConnectPlatform) SendNotice(context.Context, delivery.Notice) (delivery.Receipt, error) {
	return delivery.Receipt{}, nil
}

func (p *fakeConnectPlatform) SetConnectNotifier(notifier func(context.Context, string)) {
	p.notifier = notifier
}

type fakePlatformSenders struct{}

func (fakePlatformSenders) RegisterPlatformSender(string, delivery.MessageSender) {}

// 平台连接成功必须发布全局信号：这是 hook 侧唯一的输入来源，而连接回调本身
// 不再直接调用 Agent（见 internal/agent/platform_signals.go）。
func TestRegisterPlatformHooksPublishesPlatformConnectedSignal(t *testing.T) {
	received := make(chan platform.ConnectedEvent, 4)
	connection, err := platform.PlatformConnected.Observe(func(_ context.Context, event platform.ConnectedEvent) error {
		received <- event
		return nil
	}, signal.ConnectOptions{})
	if err != nil {
		t.Fatalf("observe platform connected: %v", err)
	}
	t.Cleanup(connection.Disconnect)

	adapter := &fakeConnectPlatform{name: "qqonebot"}
	registerPlatformHooks(fakePlatformSenders{}, []platformRuntime{adapter}, nil)
	if adapter.notifier == nil {
		t.Fatal("connect notifier was not installed")
	}

	// 适配器不报平台名时用适配器自己的名字。
	adapter.notifier(context.Background(), "")
	select {
	case got := <-received:
		if got.Platform != "qqonebot" {
			t.Fatalf("published platform = %q", got.Platform)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("platform connected signal was not published")
	}
}

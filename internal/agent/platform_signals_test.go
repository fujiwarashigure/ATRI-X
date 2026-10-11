package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/hook"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
)

// newPlatformSignalTestAgent 造一个订阅了全局平台连接信号的 Agent，并注册一个
// platform.connected 探针 hook；返回探针收到的平台名通道。
func newPlatformSignalTestAgent(t *testing.T, ctx context.Context, handler hook.HandlerFunc) (*Agent, <-chan string) {
	t.Helper()
	a := New(&fakePlatform{}, &fakeLLM{}, "test-model", config.ProviderConfig{}, newTestStore(t))
	manager := hook.NewManager()
	a.SetHookManager(manager)
	ran := make(chan string, 16)
	if err := manager.Register(hook.Registration{
		Point: hook.PointPlatformConnected,
		Name:  "test.platform_connected",
		Match: hook.Always(),
		Handler: hook.HandlerFunc(func(handlerCtx context.Context, event hook.Event) (hook.Event, error) {
			ran <- event.Platform.Name
			if handler != nil {
				return handler(handlerCtx, event)
			}
			return event, nil
		}),
	}); err != nil {
		t.Fatalf("register hook: %v", err)
	}
	if err := a.StartPlatformEvents(ctx); err != nil {
		t.Fatalf("StartPlatformEvents: %v", err)
	}
	t.Cleanup(func() { _ = a.ClosePlatformEvents(context.Background()) })
	return a, ran
}

func waitPlatformName(t *testing.T, ran <-chan string, want string) {
	t.Helper()
	select {
	case got := <-ran:
		if got != want {
			t.Fatalf("hook platform = %q, want %q", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("platform.connected hook did not run for %q", want)
	}
}

func TestPlatformConnectedSignalRunsHook(t *testing.T) {
	ctx := context.Background()
	_, ran := newPlatformSignalTestAgent(t, ctx, nil)

	if _, err := platform.PlatformConnected.Emit(ctx, platform.ConnectedEvent{Platform: "qqonebot"}); err != nil {
		t.Fatalf("emit platform connected: %v", err)
	}
	waitPlatformName(t, ran, "qqonebot")
}

// 关键性质：发布方（平台适配器的连接回调）不等 hook 业务跑完。
func TestPlatformConnectedEmitDoesNotWaitForHookWork(t *testing.T) {
	ctx := context.Background()
	entered := make(chan struct{})
	release := make(chan struct{})
	_, _ = newPlatformSignalTestAgent(t, ctx, func(context.Context, hook.Event) (hook.Event, error) {
		close(entered)
		<-release
		return hook.Event{}, nil
	})

	start := time.Now()
	if _, err := platform.PlatformConnected.Emit(ctx, platform.ConnectedEvent{Platform: "cli"}); err != nil {
		t.Fatalf("emit platform connected: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Emit waited for hook work: %s", elapsed)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("hook never started")
	}
	close(release)
}

// 同一平台按到达顺序串行补做：队列默认 Concurrency 为 1。
func TestPlatformConnectedSerializesPerPlatform(t *testing.T) {
	ctx := context.Background()
	var mu sync.Mutex
	running, maxRunning := 0, 0
	_, ran := newPlatformSignalTestAgent(t, ctx, func(context.Context, hook.Event) (hook.Event, error) {
		mu.Lock()
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()
		time.Sleep(30 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return hook.Event{}, nil
	})

	for i := 0; i < 3; i++ {
		if _, err := platform.PlatformConnected.Emit(ctx, platform.ConnectedEvent{Platform: "cli"}); err != nil {
			t.Fatalf("emit %d: %v", i, err)
		}
	}
	for i := 0; i < 3; i++ {
		waitPlatformName(t, ran, "cli")
	}
	mu.Lock()
	defer mu.Unlock()
	if maxRunning != 1 {
		t.Fatalf("same-platform callbacks overlapped: max=%d", maxRunning)
	}
}

func TestClosePlatformEventsStopsAdmission(t *testing.T) {
	ctx := context.Background()
	a, ran := newPlatformSignalTestAgent(t, ctx, nil)
	if err := a.ClosePlatformEvents(ctx); err != nil {
		t.Fatalf("ClosePlatformEvents: %v", err)
	}
	if _, err := platform.PlatformConnected.Emit(ctx, platform.ConnectedEvent{Platform: "cli"}); err != nil {
		t.Fatalf("emit after close: %v", err)
	}
	select {
	case got := <-ran:
		t.Fatalf("hook ran after close: %q", got)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestStartPlatformEventsIsIdempotentAndRejectsRestart(t *testing.T) {
	ctx := context.Background()
	a, _ := newPlatformSignalTestAgent(t, ctx, nil)
	if err := a.StartPlatformEvents(ctx); err != nil {
		t.Fatalf("second StartPlatformEvents: %v", err)
	}
	if err := a.ClosePlatformEvents(ctx); err != nil {
		t.Fatalf("ClosePlatformEvents: %v", err)
	}
	if err := a.StartPlatformEvents(ctx); err == nil {
		t.Fatal("a closed subscriber must not restart")
	}
}

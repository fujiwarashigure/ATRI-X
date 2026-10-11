package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/signal"
)

func newNoticeQueueTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := New(&fakePlatform{}, &fakeLLM{}, "test-model", config.ProviderConfig{}, newTestStore(t))
	t.Cleanup(func() { _ = a.CloseNoticeQueues(context.Background()) })
	return a
}

func noticeFor(scopeID string) delivery.Notice {
	return delivery.Notice{Target: delivery.Target{Platform: "cli", ScopeID: scopeID}}
}

func TestNoticeQueuesSerializeSameTarget(t *testing.T) {
	a := newNoticeQueueTestAgent(t)
	ctx := context.Background()
	notice := noticeFor("local")

	var mu sync.Mutex
	running, maxRunning := 0, 0
	send := func(context.Context) (delivery.Receipt, error) {
		mu.Lock()
		running++
		if running > maxRunning {
			maxRunning = running
		}
		mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		mu.Lock()
		running--
		mu.Unlock()
		return delivery.Receipt{PlatformMessageIDs: []string{"m"}}, nil
	}

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			receipt, err := a.noticeQueues.send(ctx, "cli", notice, send)
			if err != nil {
				t.Errorf("send: %v", err)
				return
			}
			if len(receipt.PlatformMessageIDs) != 1 {
				t.Errorf("receipt = %#v", receipt)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if maxRunning != 1 {
		t.Fatalf("same-target sends overlapped: max=%d", maxRunning)
	}
}

// 不同目标（同一平台）不能互相排队：队列默认并发 8。
func TestNoticeQueuesRunDifferentTargetsConcurrently(t *testing.T) {
	a := newNoticeQueueTestAgent(t)
	ctx := context.Background()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	done := make(chan struct{}, 2)
	send := func(context.Context) (delivery.Receipt, error) {
		entered <- struct{}{}
		<-release
		return delivery.Receipt{}, nil
	}

	for _, scope := range []string{"g1", "g2"} {
		scope := scope
		go func() {
			_, _ = a.noticeQueues.send(ctx, "cli", noticeFor(scope), send)
			done <- struct{}{}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("sends to different targets must not queue behind each other")
		}
	}
	close(release)
	for i := 0; i < 2; i++ {
		<-done
	}
}

// 队列满时必须立刻返回错误，而不是无限堆在慢平台上。
func TestNoticeQueuesRejectWhenFull(t *testing.T) {
	a := newNoticeQueueTestAgent(t)
	a.noticeQueues.options = signal.QueueOptions{Capacity: 1, Concurrency: 1}
	ctx := context.Background()

	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = a.noticeQueues.send(ctx, "cli", noticeFor("g1"), func(context.Context) (delivery.Receipt, error) {
			close(started)
			<-release
			return delivery.Receipt{}, nil
		})
	}()
	<-started

	// 用一个直接提交占满唯一的等待位，下一条就必然撞上限流。
	queue, err := a.noticeQueues.queue("cli")
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	if err := queue.Submit(ctx, signal.Task{
		Shutdown: signal.CancelPending,
		Key:      "g2",
		Run:      func(context.Context) error { <-release; return nil },
	}); err != nil {
		t.Fatalf("occupy waiting slot: %v", err)
	}

	_, err = a.noticeQueues.send(ctx, "cli", noticeFor("g3"), func(context.Context) (delivery.Receipt, error) {
		return delivery.Receipt{}, nil
	})
	if !errors.Is(err, signal.ErrQueueFull) {
		t.Fatalf("full queue error = %v, want ErrQueueFull", err)
	}
	close(release)
}

func TestCloseNoticeQueuesStopsAdmission(t *testing.T) {
	a := newNoticeQueueTestAgent(t)
	ctx := context.Background()
	ok := func(context.Context) (delivery.Receipt, error) { return delivery.Receipt{}, nil }

	if _, err := a.noticeQueues.send(ctx, "cli", noticeFor("g1"), ok); err != nil {
		t.Fatalf("send before close: %v", err)
	}
	if err := a.CloseNoticeQueues(ctx); err != nil {
		t.Fatalf("CloseNoticeQueues: %v", err)
	}
	if _, err := a.noticeQueues.send(ctx, "cli", noticeFor("g1"), ok); !errors.Is(err, signal.ErrClosed) {
		t.Fatalf("send after close = %v, want ErrClosed", err)
	}
}

// 关闭时等待中的发送必须拿到结果，不能永远挂着：要么在准入处被拒，要么从等待位被取消。
// 同时验证关闭预算耗尽会被报出来（运行中的那条还占着 worker）。
func TestCloseNoticeQueuesSettlesWaiterWithinBudget(t *testing.T) {
	a := newNoticeQueueTestAgent(t)
	a.noticeQueues.options = signal.QueueOptions{Capacity: 1, Concurrency: 1}
	ctx := context.Background()

	release := make(chan struct{})
	started := make(chan struct{})
	go func() {
		_, _ = a.noticeQueues.send(ctx, "cli", noticeFor("g1"), func(context.Context) (delivery.Receipt, error) {
			close(started)
			<-release
			return delivery.Receipt{}, nil
		})
	}()
	<-started

	waited := make(chan error, 1)
	go func() {
		_, err := a.noticeQueues.send(ctx, "cli", noticeFor("g2"), func(context.Context) (delivery.Receipt, error) {
			return delivery.Receipt{}, nil
		})
		waited <- err
	}()

	closeCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := a.CloseNoticeQueues(closeCtx); err == nil {
		t.Fatal("close must report the shutdown budget being exhausted while a send is running")
	}
	select {
	case err := <-waited:
		if err == nil {
			t.Fatal("a send stopped by close must report an error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("close did not settle the waiting send")
	}
	close(release)
}

func TestNoticeTargetKeyDistinguishesTargets(t *testing.T) {
	base := noticeFor("g1")
	if noticeTargetKey(base) != noticeTargetKey(noticeFor("g1")) {
		t.Fatal("same target must map to the same key")
	}
	if noticeTargetKey(base) == noticeTargetKey(noticeFor("g2")) {
		t.Fatal("different scopes must map to different keys")
	}
	if noticeTargetKey(delivery.Notice{}) != "context" {
		t.Fatalf("empty target key = %q", noticeTargetKey(delivery.Notice{}))
	}
	if noticeTargetKey(delivery.Notice{Target: delivery.Target{Superadmins: true}}) == noticeTargetKey(delivery.Notice{}) {
		t.Fatal("superadmin notices must not share the context key")
	}
}

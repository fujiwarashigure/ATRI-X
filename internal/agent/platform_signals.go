package agent

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fujiwarashigure/ATRI-X/internal/platform"
	"github.com/fujiwarashigure/ATRI-X/internal/signal"
)

// platformSubscriptions 是 Agent 自己持有的平台连接订阅：信号连接、每个平台的执行队列
// 和关闭都归消费者，发布方只负责 Emit。
//
// 上游把同一事实做成 internal/events 加 HookBridge / Cron 各自订阅；fork 只让 hook 侧
// 订阅，cron 继续挂在 hook 点 `builtin.cron.missed_once` 上（见 upstream-sync-todo.md §8.2）。
type platformSubscriptions struct {
	mu         sync.Mutex
	connection *signal.Connection
	queues     map[string]*signal.Queue
	parent     context.Context
	stopParent func() bool
	closed     bool
	done       chan struct{}
}

// StartPlatformEvents 订阅全局平台连接信号。依赖装配完成后、适配器启动前调用：
// 重复调用不会重复订阅，已关闭的订阅者不能重新启动。
func (a *Agent) StartPlatformEvents(ctx context.Context) error {
	if a == nil {
		return fmt.Errorf("agent unavailable")
	}
	p := &a.platformEvents
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return signal.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.connection != nil {
		return nil
	}
	connection, err := platform.PlatformConnected.Observe(a.enqueuePlatformConnected, signal.ConnectOptions{})
	if err != nil {
		return err
	}
	p.connection = connection
	p.queues = map[string]*signal.Queue{}
	p.parent = ctx
	p.stopParent = context.AfterFunc(ctx, a.beginClosePlatformEvents)
	return nil
}

// enqueuePlatformConnected 只做准入与排队：把平台连接事实交给自己那条按平台的队列后
// 立刻返回，因此平台适配器的连接回调不再被 hook 规则或 cron 补投递拖住。
func (a *Agent) enqueuePlatformConnected(ctx context.Context, event platform.ConnectedEvent) error {
	p := &a.platformEvents
	p.mu.Lock()
	defer p.mu.Unlock()
	// Disconnect 收不回已经发出的信号快照，所以准入判断必须和队列创建、关闭共用同一把锁，
	// 覆盖 AfterFunc 还没跑到的取消窗口。
	if p.closed || p.connection == nil || p.parent.Err() != nil {
		return nil
	}
	name := strings.TrimSpace(event.Platform)
	if name == "" {
		return fmt.Errorf("agent: platform connection has no platform name")
	}
	queue := p.queues[name]
	if queue == nil {
		// Capacity / Concurrency 默认 256 / 1：同一平台按到达顺序串行补做，平台之间互不排队。
		var err error
		queue, err = signal.NewQueue(signal.QueueOptions{Name: name + ".platform.connected"})
		if err != nil {
			return err
		}
		p.queues[name] = queue
	}
	// 保留事件值，但取消权归消费者：连接用的 ctx 会随适配器启动阶段结束，而补做必须在
	// 适配器生命周期之外继续。Submit 有界，不等容量也不等业务执行。
	return queue.Submit(context.WithoutCancel(ctx), signal.Task{
		Shutdown: signal.CancelPending,
		Run: func(runCtx context.Context) error {
			a.handlePlatformConnected(runCtx, name)
			return nil
		},
	})
}

// beginClosePlatformEvents 断开订阅并关闭各平台队列，不等待回调退出。
func (a *Agent) beginClosePlatformEvents() {
	p := &a.platformEvents
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.closed = true
	if p.done == nil {
		p.done = make(chan struct{})
	}
	if p.stopParent != nil {
		p.stopParent()
	}
	if p.connection != nil {
		p.connection.Disconnect()
	}
	queues := make([]*signal.Queue, 0, len(p.queues))
	for _, queue := range p.queues {
		queue.BeginClose()
		queues = append(queues, queue)
	}
	if len(queues) == 0 {
		close(p.done)
		return
	}
	go func() {
		for _, queue := range queues {
			<-queue.Done()
		}
		close(p.done)
	}()
}

// platformEventsDone 只在停止接纳且每个回调都退出后才关闭。
func (a *Agent) platformEventsDone() <-chan struct{} {
	p := &a.platformEvents
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.done == nil {
		p.done = make(chan struct{})
	}
	return p.done
}

// ClosePlatformEvents 停止接纳新的连接事实并等待已接纳的回调真实退出；超出 ctx 预算时
// 返回 ctx 的错误，不宣称仍在跑的回调已经结束。
func (a *Agent) ClosePlatformEvents(ctx context.Context) error {
	if a == nil {
		return nil
	}
	a.beginClosePlatformEvents()
	done := a.platformEventsDone()
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

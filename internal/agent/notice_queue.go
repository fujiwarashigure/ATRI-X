package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/signal"
)

const (
	// noticeQueueCapacity / noticeQueueConcurrency 是每个平台的发送上限：最多同时发 8 条，
	// 最多 256 条在等（与上游 notification.Manager 的默认值一致）。
	noticeQueueCapacity    = 256
	noticeQueueConcurrency = 8
)

// noticeQueueResult 是一次排队发送的结果。
type noticeQueueResult struct {
	receipt delivery.Receipt
	err     error
}

// noticeQueues 是 Agent 自己持有的按平台发送队列。
//
// 平台之间互不排队（每个平台一条 Queue），同一目标（平台 + scope + 收件人）按到达顺序串行。
// 调用方语义不变：提交后等队列里的真实结果并原样返回 Receipt / error，受 ctx 约束；
// 队列满时立刻返回错误，而不是无限堆在一个慢平台上。
type noticeQueues struct {
	mu      sync.Mutex
	queues  map[string]*signal.Queue
	closed  bool
	options signal.QueueOptions // 留空即为默认值；测试用它构造小容量队列
}

// send 把一次发送排进对应平台的队列，并等待真实结果。
func (q *noticeQueues) send(ctx context.Context, platformName string, notice delivery.Notice, send func(context.Context) (delivery.Receipt, error)) (delivery.Receipt, error) {
	queue, err := q.queue(platformName)
	if err != nil {
		return delivery.Receipt{}, err
	}
	result := make(chan noticeQueueResult, 1)
	if err := queue.Submit(ctx, signal.Task{
		Shutdown: signal.CancelPending,
		Key:      noticeTargetKey(notice),
		Run: func(runCtx context.Context) error {
			receipt, err := send(runCtx)
			result <- noticeQueueResult{receipt: receipt, err: err}
			return err
		},
		Finish: func(err error) {
			// Run 可能根本没执行（关闭或调用方取消），保证等待者一定拿到结果。
			select {
			case result <- noticeQueueResult{err: err}:
			default:
			}
		},
	}); err != nil {
		return delivery.Receipt{}, err
	}
	select {
	case got := <-result:
		return got.receipt, got.err
	case <-ctx.Done():
		return delivery.Receipt{}, ctx.Err()
	}
}

func (q *noticeQueues) queue(platformName string) (*signal.Queue, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return nil, signal.ErrClosed
	}
	if q.queues == nil {
		q.queues = map[string]*signal.Queue{}
	}
	if queue := q.queues[platformName]; queue != nil {
		return queue, nil
	}
	options := q.options
	options.Name = platformName + ".notice"
	if options.Capacity == 0 {
		options.Capacity = noticeQueueCapacity
	}
	if options.Concurrency == 0 {
		options.Concurrency = noticeQueueConcurrency
	}
	queue, err := signal.NewQueue(options)
	if err != nil {
		return nil, err
	}
	q.queues[platformName] = queue
	return queue, nil
}

// close 停止接纳新的发送，并等待已接纳的发送真实退出；超出 ctx 预算返回 ctx 的错误，
// 不宣称仍在跑的发送已经结束。
func (q *noticeQueues) close(ctx context.Context) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	queues := make([]*signal.Queue, 0, len(q.queues))
	for _, queue := range q.queues {
		queue.BeginClose()
		queues = append(queues, queue)
	}
	q.mu.Unlock()
	for _, queue := range queues {
		select {
		case <-queue.Done():
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// noticeTargetKey 是"同一目标"的串行键：平台 + scope + 收件人。没有显式目标的通知
// 走当前会话的 sender，按上下文串行即可。
func noticeTargetKey(notice delivery.Notice) string {
	target := notice.Target
	parts := []string{target.Platform, target.ScopeID, target.PrivateUserID, target.GroupID}
	if target.Superadmins {
		parts = append(parts, "superadmins")
	}
	key := strings.Trim(strings.Join(parts, "\x00"), "\x00")
	if strings.TrimSpace(key) == "" {
		return "context"
	}
	return key
}

// CloseNoticeQueues 停止接纳通知发送并等待在途发送退出，由 App 的运行时关闭步骤调用。
func (a *Agent) CloseNoticeQueues(ctx context.Context) error {
	if a == nil {
		return nil
	}
	return a.noticeQueues.close(ctx)
}

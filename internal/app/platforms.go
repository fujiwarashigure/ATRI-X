package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/agent"
	"github.com/fujiwarashigure/ATRI-X/internal/command"
	"github.com/fujiwarashigure/ATRI-X/internal/completion"
	"github.com/fujiwarashigure/ATRI-X/internal/config"
	elcron "github.com/fujiwarashigure/ATRI-X/internal/cron"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/health"
	"github.com/fujiwarashigure/ATRI-X/internal/hook"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
	platformbuiltin "github.com/fujiwarashigure/ATRI-X/internal/platform/builtin"
)

type defaultPlatformFactory struct{}

func (defaultPlatformFactory) Build(req PlatformRequest) (PlatformComponents, error) {
	foundation := req.Foundation
	bundle, err := platformbuiltin.New(
		platformbuiltin.Options{Mode: platformMode(req.Mode)},
		foundation.Config,
		foundation.Store,
		foundation.ChatHistory,
		foundation.Logger,
	)
	if err != nil {
		return PlatformComponents{}, err
	}
	req.Profiler.Mark("platform init")
	return PlatformComponents{Primary: bundle.Primary, Runtimes: bundle.Runtimes}, nil
}

type defaultPlatformExecutor struct{}

func (defaultPlatformExecutor) Run(ctx context.Context, req PlatformRunRequest) error {
	return runPlatforms(ctx, req.Handler, req.Logger, req.Runtimes, req.AfterStart, req.Heartbeat, req.Mode, req.PlatformFailure)
}

type platformRuntime = platform.Runtime

type platformLifecycle interface {
	StopAppOnExit() bool
}

type platformHookAgent interface {
	RegisterPlatformSender(name string, sender delivery.MessageSender)
}

type completionPlatform interface {
	SetCompleter(*completion.Service)
}

type commandCatalogPlatform interface {
	SetCommandCatalog([]command.Info)
}

func registerCompletionPlatforms(agt *agent.Agent, adapters []platformRuntime) {
	if agt == nil {
		return
	}
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		if completer, ok := adapter.(completionPlatform); ok {
			completer.SetCompleter(agt.CompletionService())
		}
	}
}

func registerCronPlatformHook(hooks hook.Registrar, service *elcron.Service) error {
	if hooks == nil || service == nil {
		return nil
	}
	return hooks.Register(hook.Registration{
		Point:       hook.PointPlatformConnected,
		Name:        "builtin.cron.missed_once",
		Description: "平台连接时补投递 missed once cron",
		Match:       hook.Always(),
		Handler: hook.HandlerFunc(func(ctx context.Context, event hook.Event) (hook.Event, error) {
			service.NotifyPlatformConnected(ctx, event.Platform.Name)
			return event, nil
		}),
	})
}

func registerCommandCatalogs(agt *agent.Agent, adapters []platformRuntime) {
	if agt == nil {
		return
	}
	commands := agt.CommandInfos()
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		if catalog, ok := adapter.(commandCatalogPlatform); ok {
			catalog.SetCommandCatalog(commands)
		}
	}
}

func registerPlatformHooks(agt platformHookAgent, adapters []platformRuntime, healthState *health.State) {
	if agt == nil {
		return
	}
	for _, adapter := range adapters {
		if adapter == nil {
			continue
		}
		name := adapter.Name()
		agt.RegisterPlatformSender(name, adapter)
		if notifier, ok := adapter.(platform.ConnectNotifier); ok {
			healthState.ExpectPlatform(name)
			notifier.SetConnectNotifier(func(ctx context.Context, platformName string) {
				if platformName == "" {
					platformName = name
				}
				healthState.MarkPlatformConnected(platformName)
				// 只发布事实：订阅方（hook 侧）自己排在自己的按平台队列里执行，
				// 连接回调不再等 hook 规则或 cron 补投递跑完。
				_, _ = platform.PlatformConnected.Emit(ctx, platform.ConnectedEvent{Platform: platformName})
			})
		}
	}
}

func platformStopsAppOnExit(adapter platformRuntime) bool {
	lifecycle, ok := adapter.(platformLifecycle)
	return ok && lifecycle.StopAppOnExit()
}

// noticeSender 是平台失败通知需要的最小发送能力，*agent.Agent 即满足。
type noticeSender interface {
	SendNotice(context.Context, delivery.Notice) (delivery.Receipt, error)
}

// notifyPlatformFailure 在某个平台非预期退出后通知其他仍在运行的平台上的配置超管。
// 与上游 internal/platform/module 的 notifyPlatformFailure 同名同义：只通知仍然活着
// 的平台、目标 ID 去重且不补造，未配置超管的平台直接跳过；atrinis 是后台运行时而不是
// 消息平台，不作为通知对象。发送保持同步，调用方（失败平台自己的 goroutine）因此会
// 等真实回执，但队列满与平台自身超时都会让它返回，不会无限期挂住。
func notifyPlatformFailure(ctx context.Context, cfg *config.Config, sender noticeSender, failed string, stillRunning []string, failure error) {
	if cfg == nil || sender == nil {
		return
	}
	text := fmt.Sprintf("平台 %s 异常退出：%v", failed, failure)
	for _, name := range stillRunning {
		name = strings.TrimSpace(name)
		if name == "" || name == failed || name == "atrinis" {
			continue
		}
		seen := map[string]bool{}
		for _, id := range cfg.Security.Superadmins[name] {
			id = strings.TrimSpace(id)
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			_, _ = sender.SendNotice(context.WithoutCancel(ctx), delivery.Notice{
				Target:  delivery.Target{Platform: name, PrivateUserID: id},
				Outputs: []delivery.Output{delivery.Text(text)},
				Level:   slog.LevelError,
			})
		}
	}
}

// runPlatforms 启动全部平台运行时并等待它们结束。适配器自己负责重连退避，所以这里
// 只处理"运行时真的退出"的两种情况：
//   - 非预期退出（运行上下文还没结束）：记 WARN，并交给 app 处理（health + 超管通知）；
//   - 交互式 CLI 退出（StopAppOnExit）：非 service 模式下这就是应用退出意图，取消整轮。
//
// service 模式下平台循环不拥有进程生命周期：即使全部运行时退出也继续等外部停止，
// 这样 health 接口（/live、/ready、/tasks）与 systemd/Docker 的重启策略仍然有效。
// 此前只有"一个平台都没启用"才会保活，最后一个平台异常退出会让进程以 exit 0 结束，
// systemd 的 Restart=on-failure 不会把它拉回来。
func runPlatforms(ctx context.Context, handler platform.PlatformHandler, logger *slog.Logger, adapters []platformRuntime, afterStart func(context.Context), heartbeat func(), mode RunMode, failure func(context.Context, string, []string, error)) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	running := make(map[string]bool, len(adapters))
	var wg sync.WaitGroup
	for _, adapter := range adapters {
		adapter := adapter
		name := adapter.Name()
		mu.Lock()
		running[name] = true
		mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := adapter.Run(runCtx, handler)
			mu.Lock()
			delete(running, name)
			stillRunning := make([]string, 0, len(running))
			for other := range running {
				stillRunning = append(stillRunning, other)
			}
			mu.Unlock()
			// 运行上下文已结束时的退出是正常关停：既不记失败，也不通知。
			if err != nil && !(runCtx.Err() != nil && errors.Is(err, runCtx.Err())) {
				if logger != nil {
					logger.WarnContext(runCtx, "platform stopped with error", "platform", name, "error", err.Error())
				}
				if runCtx.Err() == nil && failure != nil {
					sort.Strings(stillRunning)
					failure(runCtx, name, stillRunning, err)
				}
			}
			if mode != RunModeService && platformStopsAppOnExit(adapter) {
				cancel()
			}
		}()
	}
	if heartbeat != nil {
		heartbeat()
	}
	if afterStart != nil {
		afterStart(runCtx)
	}
	if len(adapters) == 0 && logger != nil {
		// 没有任何平台被启用时进程必须保持存活：如果直接返回 nil，进程会立即退出
		// （exit 0）。在 Docker / systemd 下这意味着全新安装的容器会被 restart
		// policy（unless-stopped 对 exit 0 也会重启）反复拉起，health 接口也随进程
		// 一起消失。因此这里保持存活，等待外部信号，并继续发送调度心跳。
		logger.WarnContext(runCtx, "no platform is enabled; service stays alive so health endpoints remain available until a platform is configured")
	}
	exited := make(chan struct{})
	go func() {
		wg.Wait()
		close(exited)
	}()
	allExited := false
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			cancel()
			if !allExited {
				<-exited
			}
			return ctx.Err()
		case <-exited:
			allExited = true
			// 关闭的 channel 永远就绪：置空后只剩外部停止与心跳两个分支。
			exited = nil
			if mode != RunModeService {
				return nil
			}
		case <-ticker.C:
			if heartbeat != nil {
				heartbeat()
			}
		}
	}
}

type atrinisRuntimeAdapter struct {
	runtime interface {
		Run(context.Context) error
	}
}

func (a atrinisRuntimeAdapter) Name() string { return "atrinis" }

func (a atrinisRuntimeAdapter) Run(ctx context.Context, _ platform.PlatformHandler) error {
	return a.runtime.Run(ctx)
}

func (a atrinisRuntimeAdapter) SendChat(context.Context, []delivery.Output) (delivery.Receipt, error) {
	return delivery.Receipt{}, fmt.Errorf("atrinis cannot send chat output")
}

func (a atrinisRuntimeAdapter) SendNotice(context.Context, delivery.Notice) (delivery.Receipt, error) {
	return delivery.Receipt{}, fmt.Errorf("atrinis cannot send notice output")
}

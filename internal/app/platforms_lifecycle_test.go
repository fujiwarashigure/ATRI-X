package app

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/delivery"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
)

// lifecycleRuntime 是平台循环的最小替身：Run 立即返回预设错误（或 nil），
// 需要"一直不退出"时用 released 挡住。
type lifecycleRuntime struct {
	name     string
	err      error
	released <-chan struct{}
}

func (r lifecycleRuntime) Name() string { return r.name }

func (r lifecycleRuntime) Run(ctx context.Context, _ platform.PlatformHandler) error {
	if r.released != nil {
		select {
		case <-r.released:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.err
}

func (r lifecycleRuntime) SendChat(context.Context, []delivery.Output) (delivery.Receipt, error) {
	return delivery.Receipt{}, nil
}

func (r lifecycleRuntime) SendNotice(context.Context, delivery.Notice) (delivery.Receipt, error) {
	return delivery.Receipt{}, nil
}

// stopAppOnExitRuntime 模拟交互式 CLI：它退出就是应用退出意图。
type stopAppOnExitRuntime struct{ lifecycleRuntime }

func (r stopAppOnExitRuntime) StopAppOnExit() bool { return true }

type platformFailureReport struct {
	platform     string
	stillRunning []string
	err          error
}

func recordingPlatformFailure(reports chan<- platformFailureReport) func(context.Context, string, []string, error) {
	return func(_ context.Context, name string, stillRunning []string, err error) {
		reports <- platformFailureReport{platform: name, stillRunning: stillRunning, err: err}
	}
}

// service 模式下平台循环不拥有进程生命周期：全部运行时异常退出后进程继续等待外部
// 停止，并且每次非预期退出都上报（交给 app 标记 health 与通知超管）。
func TestRunPlatformsServiceSurvivesEveryRuntimeFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reports := make(chan platformFailureReport, 4)
	adapters := []platformRuntime{
		lifecycleRuntime{name: "qqonebot", err: errors.New("listener failed")},
		lifecycleRuntime{name: "telegram", err: errors.New("gateway failed")},
	}
	done := make(chan error, 1)
	go func() {
		done <- runPlatforms(ctx, nil, nil, adapters, nil, nil, RunModeService, recordingPlatformFailure(reports))
	}()

	reported := map[string][]string{}
	for i := 0; i < len(adapters); i++ {
		select {
		case report := <-reports:
			reported[report.platform] = report.stillRunning
			if report.err == nil {
				t.Fatalf("failure report for %s lost its error", report.platform)
			}
			for _, name := range report.stillRunning {
				if name == report.platform {
					t.Fatalf("stillRunning of %s contains the failed platform: %v", report.platform, report.stillRunning)
				}
			}
		case <-time.After(5 * time.Second):
			t.Fatal("platform failure was not reported")
		}
	}
	if len(reported) != 2 {
		t.Fatalf("reported failures = %#v", reported)
	}

	select {
	case err := <-done:
		t.Fatalf("service must keep running after every runtime exits, got %v", err)
	case <-time.After(200 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runPlatforms error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPlatforms did not stop after cancellation")
	}
}

// 非 service 模式保持原语义：运行时全部结束就返回 nil（应用退出）。
func TestRunPlatformsFullModeReturnsWhenRuntimesStop(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- runPlatforms(context.Background(), nil, nil, []platformRuntime{
			lifecycleRuntime{name: "qqonebot"},
			lifecycleRuntime{name: "telegram"},
		}, nil, nil, RunModeFull, nil)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runPlatforms error = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("full mode must return once every runtime stops")
	}
}

// 交互式 CLI 退出拥有应用退出职责：非 service 模式下它结束就取消整轮运行。
// 取消与"全部运行时结束"在同一瞬间都成立，两条出口都被 main 当成干净退出，
// 因此这里只断言它确实停了下来。
func TestRunPlatformsInteractiveCLIOwnsProcessLifetime(t *testing.T) {
	done := make(chan error, 1)
	go func() {
		done <- runPlatforms(context.Background(), nil, nil, []platformRuntime{
			stopAppOnExitRuntime{lifecycleRuntime{name: "cli"}},
		}, nil, nil, RunModeFull, nil)
	}()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Fatalf("runPlatforms error = %v, want nil or context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("an interactive CLI exit must end the run")
	}
}

// service 模式下同一个 StopAppOnExit 不再夺走应用所有权。
func TestRunPlatformsServiceIgnoresStopAppOnExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runPlatforms(ctx, nil, nil, []platformRuntime{
			stopAppOnExitRuntime{lifecycleRuntime{name: "cli"}},
		}, nil, nil, RunModeService, nil)
	}()
	select {
	case err := <-done:
		t.Fatalf("service must survive an interactive CLI exit, got %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("runPlatforms error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("runPlatforms did not stop after cancellation")
	}
}

// 没有任何平台时同样保活（合并等待循环后这条路径不能再丢）。
func TestRunPlatformsWithoutPlatformsKeepsServiceAlive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- runPlatforms(ctx, nil, nil, nil, nil, nil, RunModeService, nil)
	}()
	select {
	case err := <-done:
		t.Fatalf("a service without platforms must stay alive, got %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runPlatforms did not stop after cancellation")
	}
}

type recordingNoticeSender struct {
	mu      sync.Mutex
	notices []delivery.Notice
}

func (s *recordingNoticeSender) SendNotice(_ context.Context, notice delivery.Notice) (delivery.Receipt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.notices = append(s.notices, notice)
	return delivery.Receipt{}, nil
}

func (s *recordingNoticeSender) recorded() []delivery.Notice {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]delivery.Notice(nil), s.notices...)
}

// 只通知其他仍在运行平台上的配置超管：失败平台自己、没有超管配置的平台和 atrinis
// 都不发；同一个 ID 只发一次。
func TestNotifyPlatformFailureNotifiesOtherPlatformSuperadmins(t *testing.T) {
	cfg := &config.Config{Security: config.SecurityConfig{Superadmins: map[string][]string{
		"qqonebot":   {"admin", " admin ", "", "admin"},
		"telegram":   {},
		"qqofficial": {"ops"},
		"atrinis":      {"ops"},
	}}}
	sender := &recordingNoticeSender{}
	notifyPlatformFailure(context.Background(), cfg, sender,
		"qqonebot", []string{"atrinis", "qqonebot", "qqofficial", "telegram"}, errors.New("listener failed"))

	notices := sender.recorded()
	if len(notices) != 1 {
		t.Fatalf("notices = %#v, want exactly one for qqofficial/ops", notices)
	}
	notice := notices[0]
	if notice.Target.Platform != "qqofficial" || notice.Target.PrivateUserID != "ops" {
		t.Fatalf("notice target = %#v", notice.Target)
	}
	if notice.Level != slog.LevelError {
		t.Fatalf("notice level = %v, want error", notice.Level)
	}
	text := notice.Outputs[0].Text
	if !strings.Contains(text, "qqonebot") || !strings.Contains(text, "listener failed") {
		t.Fatalf("notice text = %q", text)
	}
}

func TestNotifyPlatformFailureWithoutSuperadminsSendsNothing(t *testing.T) {
	cfg := &config.Config{Security: config.SecurityConfig{Superadmins: map[string][]string{"telegram": nil}}}
	sender := &recordingNoticeSender{}
	notifyPlatformFailure(context.Background(), cfg, sender, "qqonebot", []string{"telegram"}, errors.New("boom"))
	if got := sender.recorded(); len(got) != 0 {
		t.Fatalf("notices = %#v, want none", got)
	}
}

// 启动期 Hook 告警要挑一个真能送达的平台：主平台已注册就用它（CLI 模式），
// headless 的 service 主平台与 atrinis 都不算落点。
func TestHookNoticeTarget(t *testing.T) {
	cases := []struct {
		name     string
		primary  platform.PlatformAdapter
		runtimes []platformRuntime
		want     string
	}{
		{name: "primary is a runtime", primary: lifecycleRuntime{name: "cli"}, runtimes: []platformRuntime{lifecycleRuntime{name: "cli"}}, want: "cli"},
		{name: "headless primary falls back to a real platform", primary: lifecycleRuntime{name: "service"}, runtimes: []platformRuntime{lifecycleRuntime{name: "qqonebot"}}, want: "qqonebot"},
		{name: "atrinis is not a landing spot", primary: lifecycleRuntime{name: "service"}, runtimes: []platformRuntime{lifecycleRuntime{name: "atrinis"}}, want: ""},
		{name: "first real platform wins", primary: lifecycleRuntime{name: "service"}, runtimes: []platformRuntime{lifecycleRuntime{name: "atrinis"}, lifecycleRuntime{name: "telegram"}, lifecycleRuntime{name: "qqonebot"}}, want: "telegram"},
		{name: "no runtime", primary: lifecycleRuntime{name: "service"}, runtimes: nil, want: ""},
		{name: "no primary", primary: nil, runtimes: []platformRuntime{lifecycleRuntime{name: "telegram"}}, want: "telegram"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := hookNoticeTarget(tc.primary, tc.runtimes); got != tc.want {
				t.Fatalf("hookNoticeTarget = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDeliverHookNoticesSendsToTheChosenPlatform(t *testing.T) {
	sender := &recordingNoticeSender{}
	deliverHookNotices(context.Background(), nil, sender, "qqonebot", []string{"  ", "Hook 插件 rules 已禁用：bad toml", ""})
	notices := sender.recorded()
	if len(notices) != 1 {
		t.Fatalf("notices = %#v, want one", notices)
	}
	if notices[0].Target.Platform != "qqonebot" || !notices[0].Target.Superadmins {
		t.Fatalf("notice target = %#v", notices[0].Target)
	}
	if notices[0].Level != slog.LevelWarn {
		t.Fatalf("notice level = %v, want warn", notices[0].Level)
	}
	if text := notices[0].Outputs[0].Text; !strings.Contains(text, "bad toml") {
		t.Fatalf("notice text = %q", text)
	}
}

func TestDeliverHookNoticesWithoutAPlatformSendsNothing(t *testing.T) {
	sender := &recordingNoticeSender{}
	deliverHookNotices(context.Background(), slog.New(slog.DiscardHandler), sender, "", []string{"no platform"})
	if got := sender.recorded(); len(got) != 0 {
		t.Fatalf("notices = %#v, want none", got)
	}
}

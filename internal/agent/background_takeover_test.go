package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/background"
	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/hook"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/platform"
	"github.com/fujiwarashigure/ATRI-X/internal/security"
	"github.com/fujiwarashigure/ATRI-X/internal/session"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

// takeoverProbeTool 模拟“前台用户恰好在这个后台 turn 的工具批次里接管了 Session”：
// 它调用与 /resume 相同的 Session 服务入口，因此接管标记会写在同一个事务里。
type takeoverProbeTool struct {
	sessions   *session.Service
	scope      session.Scope
	sessionID  string
	takeoverAt *int
}

func (t takeoverProbeTool) Name() string { return "takeover_probe" }

func (t takeoverProbeTool) Info() tool.Info {
	return tool.Info{Name: "takeover_probe", Description: "take over the background session", Source: tool.SourceBuiltin, Risk: tool.RiskLow}
}

func (t takeoverProbeTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{Type: "function", Function: llm.ToolFunctionSchema{Name: "takeover_probe", Parameters: map[string]any{"type": "object"}}}
}

func (t takeoverProbeTool) Call(context.Context, tool.CallRequest) (*tool.Result, error) {
	if _, err := t.sessions.Resume(context.Background(), t.scope, t.sessionID); err != nil {
		return nil, err
	}
	if t.takeoverAt != nil {
		*t.takeoverAt++
	}
	return &tool.Result{Content: "taken over"}, nil
}

func takeoverForegroundScope() session.Scope {
	return session.Scope{ActorID: "cli:local", Platform: "cli", PlatformScopeID: "local"}
}

func takeoverRunRequest(sessionID string) background.RunRequest {
	return background.RunRequest{
		Kind:          background.KindCron,
		Name:          "takeover",
		Title:         "Takeover probe",
		Platform:      "cli",
		Actor:         security.Actor{ID: "cli:local", Platform: "cli", PlatformUserID: "local", Role: security.RoleSuperadmin},
		ScopeID:       "cron:takeover",
		SessionID:     sessionID,
		Prompt:        "run the probe",
		ToolListNames: []string{"takeover_probe"},
	}
}

func newTakeoverTestAgent(platformAdapter *fakePlatform, f *fakeLLM, store storage.Store, registry *tool.Registry) *Agent {
	a := New(platformAdapter, f, "test-model", config.ProviderConfig{}, store)
	a.SetSecurityPolicy(security.NewPolicy("low", "critical", map[string][]string{"cli": {"local"}}))
	a.SetToolRuntime(registry, nil)
	return a
}

// countAssistantTextMessages 只统计带正文的助手消息。被接管的后台 turn 允许留下
// 用户消息和工具调用转录（那是安全的停止点），但绝不能写迟到的助手回答：时序上
// 比工具批次更晚的助手正文才代表后台又调用了模型或发布了终态输出。
func countAssistantTextMessages(messages []storage.Message) int {
	count := 0
	for _, message := range messages {
		if message.Role == storage.RoleAssistant && strings.TrimSpace(message.Content) != "" {
			count++
		}
	}
	return count
}

func TestRunBackgroundStopsWhenForegroundTakesOverSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	f := &fakeLLM{chunks: [][]llm.StreamChunk{
		{{ToolCallDeltas: []llm.ToolCallDelta{{ID: "call_1", Name: "takeover_probe", Args: "{}"}}, FinishReason: "tool_calls"}},
		{{DeltaContent: `{"completed":true,"need_report":true,"report":"late report"}`}},
	}}
	a := newTakeoverTestAgent(&fakePlatform{}, f, store, tool.NewRegistry())
	a.SetToolConfig(config.ToolsConfig{MaxRoundsPerTurn: 1})

	// 先建好后台 Session，探针才能恰好接管这一行。
	request := takeoverRunRequest("")
	bgSession, err := a.backgroundSession(ctx, request, session.Scope{ActorID: "cli:local", Platform: "cli", PlatformScopeID: "cron:takeover"})
	if err != nil {
		t.Fatalf("create background session: %v", err)
	}
	request.SessionID = bgSession.ID

	takeovers := 0
	registry := tool.NewRegistry()
	_ = registry.Register(takeoverProbeTool{sessions: a.sessions, scope: takeoverForegroundScope(), sessionID: bgSession.ID, takeoverAt: &takeovers})
	a.SetToolRuntime(registry, nil)

	result, err := a.RunBackground(ctx, request)
	if err != nil {
		t.Fatalf("RunBackground: %v", err)
	}
	if takeovers != 1 {
		t.Fatalf("probe takeovers = %d, want 1", takeovers)
	}
	if !result.TakenOver || result.Outcome != background.OutcomeTakenOver {
		t.Fatalf("result = %#v, want taken over", result)
	}
	if result.SessionID != bgSession.ID {
		t.Fatalf("session id = %q, want %q", result.SessionID, bgSession.ID)
	}
	if result.Text != "" || result.MessageID != "" {
		t.Fatalf("taken-over run produced output: %#v", result)
	}
	// 只在第一次模型调用之后停下：工具批次就是最后一个安全点。
	if requests := f.chatRequests(); len(requests) != 1 {
		t.Fatalf("chat requests = %d, want 1", len(requests))
	}
	waitNoExtraChatRequest(t, f, 1, 100*time.Millisecond)

	row, err := store.Sessions().Get(ctx, bgSession.ID)
	if err != nil {
		t.Fatalf("get session: %v", err)
	}
	if !session.WasPromoted(row) {
		t.Fatalf("session was not promoted: %#v", row)
	}
	messages, err := store.Messages().ListBySession(ctx, bgSession.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if count := countAssistantTextMessages(messages); count != 0 {
		t.Fatalf("taken-over run persisted %d assistant answer(s): %#v", count, messages)
	}
	if !hasStoredUserMessage(messages, "run the probe") {
		t.Fatalf("taken-over run lost the user message: %#v", messages)
	}
}

// takeoverFailingSessions 让 Get 在探针打开开关之后失败，模拟"接管标记查不出来"
// （数据库临时故障）。其余读写照常，因此后台 turn 能正常跑到安全点。
type takeoverFailingSessions struct {
	storage.SessionRepository
	fail bool
}

func (s *takeoverFailingSessions) Get(ctx context.Context, id string) (*storage.Session, error) {
	if s.fail {
		return nil, errors.New("session store is unavailable")
	}
	return s.SessionRepository.Get(ctx, id)
}

type takeoverFailingStore struct {
	storage.Store
	sessions *takeoverFailingSessions
}

func (s *takeoverFailingStore) Sessions() storage.SessionRepository { return s.sessions }

// failingCheckProbeTool 接管 Session 之后让后续的接管检查读不出来。
type failingCheckProbeTool struct {
	sessions *session.Service
	scope    session.Scope
	id       string
	failing  *takeoverFailingSessions
}

func (t failingCheckProbeTool) Name() string { return "takeover_probe" }

func (t failingCheckProbeTool) Info() tool.Info {
	return tool.Info{Name: "takeover_probe", Description: "take over and break the check", Source: tool.SourceBuiltin, Risk: tool.RiskLow}
}

func (t failingCheckProbeTool) Schema() llm.ToolSchema {
	return llm.ToolSchema{Type: "function", Function: llm.ToolFunctionSchema{Name: "takeover_probe", Parameters: map[string]any{"type": "object"}}}
}

func (t failingCheckProbeTool) Call(context.Context, tool.CallRequest) (*tool.Result, error) {
	if _, err := t.sessions.Resume(context.Background(), t.scope, t.id); err != nil {
		return nil, err
	}
	t.failing.fail = true
	return &tool.Result{Content: "taken over"}, nil
}

// 接管标记读不出来时必须保守停下：既不能继续调模型烧 token，也不能写出迟到的助手回答。
func TestRunBackgroundStopsWhenTakeoverCheckFails(t *testing.T) {
	ctx := context.Background()
	base := newTestStore(t)
	sessions := &takeoverFailingSessions{SessionRepository: base.Sessions()}
	store := &takeoverFailingStore{Store: base, sessions: sessions}

	f := &fakeLLM{chunks: [][]llm.StreamChunk{
		{{ToolCallDeltas: []llm.ToolCallDelta{{ID: "call_1", Name: "takeover_probe", Args: "{}"}}, FinishReason: "tool_calls"}},
		{{DeltaContent: `{"completed":true,"need_report":true,"report":"late report"}`}},
	}}
	a := newTakeoverTestAgent(&fakePlatform{}, f, store, tool.NewRegistry())
	a.SetToolConfig(config.ToolsConfig{MaxRoundsPerTurn: 1})

	request := takeoverRunRequest("")
	bgSession, err := a.backgroundSession(ctx, request, session.Scope{ActorID: "cli:local", Platform: "cli", PlatformScopeID: "cron:takeover"})
	if err != nil {
		t.Fatalf("create background session: %v", err)
	}
	request.SessionID = bgSession.ID

	registry := tool.NewRegistry()
	_ = registry.Register(failingCheckProbeTool{sessions: a.sessions, scope: takeoverForegroundScope(), id: bgSession.ID, failing: sessions})
	a.SetToolRuntime(registry, nil)

	result, err := a.RunBackground(ctx, request)
	if err != nil {
		t.Fatalf("RunBackground: %v", err)
	}
	if !result.TakenOver || result.Outcome != background.OutcomeTakenOver {
		t.Fatalf("result = %#v, want taken over", result)
	}
	if result.Text != "" {
		t.Fatalf("taken-over run produced text: %#v", result)
	}
	if requests := f.chatRequests(); len(requests) != 1 {
		t.Fatalf("chat requests = %d, want 1 (read failure must stop the turn)", len(requests))
	}
	waitNoExtraChatRequest(t, f, 1, 100*time.Millisecond)
	messages, err := base.Messages().ListBySession(ctx, bgSession.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if count := countAssistantTextMessages(messages); count != 0 {
		t.Fatalf("failing check persisted %d assistant answer(s): %#v", count, messages)
	}
}

// hookManagerOnTurnOutput 在终态输出 hook 上回调，用来把接管精确放进"安全点 3 之后、
// 助手消息落库之前"这个窗口。
type hookManagerOnTurnOutput struct {
	hook.NoopManager
	onTurnOutput func()
}

func (m hookManagerOnTurnOutput) Run(ctx context.Context, event hook.Event) (hook.Event, error) {
	if event.Point == hook.PointAgentTurnOutputPrepared && m.onTurnOutput != nil {
		m.onTurnOutput()
	}
	return event, nil
}

// 接管在安全点 3 与落库之间到达：turn 必须停下，且不写迟到的助手回答，后台报告也不投递。
func TestRunBackgroundStopsWhenTakeoverArrivesAfterFinalOutputHook(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	f := &fakeLLM{chunks: [][]llm.StreamChunk{
		{{DeltaContent: `{"completed":true,"need_report":true,"report":"late report"}`}},
	}}
	a := newTakeoverTestAgent(&fakePlatform{}, f, store, tool.NewRegistry())

	request := takeoverRunRequest("")
	bgSession, err := a.backgroundSession(ctx, request, session.Scope{ActorID: "cli:local", Platform: "cli", PlatformScopeID: "cron:takeover"})
	if err != nil {
		t.Fatalf("create background session: %v", err)
	}
	request.SessionID = bgSession.ID

	takeovers := 0
	a.SetHookManager(hookManagerOnTurnOutput{onTurnOutput: func() {
		if _, err := a.sessions.Resume(context.Background(), takeoverForegroundScope(), bgSession.ID); err != nil {
			t.Errorf("resume in hook: %v", err)
			return
		}
		takeovers++
	}})

	result, err := a.RunBackground(ctx, request)
	if err != nil {
		t.Fatalf("RunBackground: %v", err)
	}
	if takeovers != 1 {
		t.Fatalf("hook takeovers = %d, want 1", takeovers)
	}
	if !result.TakenOver || result.Outcome != background.OutcomeTakenOver {
		t.Fatalf("result = %#v, want taken over", result)
	}
	if result.Text != "" || result.MessageID != "" {
		t.Fatalf("taken-over run produced output: %#v", result)
	}
	if requests := f.chatRequests(); len(requests) != 1 {
		t.Fatalf("chat requests = %d, want 1", len(requests))
	}
	messages, err := store.Messages().ListBySession(ctx, bgSession.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if count := countAssistantTextMessages(messages); count != 0 {
		t.Fatalf("window takeover persisted %d assistant answer(s): %#v", count, messages)
	}
}

func TestRunBackgroundOnPromotedSessionStopsBeforeAnyModelCall(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	row := &storage.Session{
		OwnerID:         "cli:local",
		Platform:        "cli",
		PlatformScopeID: "cron:report",
		Mode:            storage.SessionModeWork,
		Status:          storage.SessionStatusActive,
		Title:           "Cron: promoted",
		Metadata:        `{"title_renamed":true,"background_kind":"cron","background_name":"promoted"}`,
	}
	if err := store.Sessions().Create(ctx, row); err != nil {
		t.Fatalf("create session: %v", err)
	}
	f := &fakeLLM{chunks: [][]llm.StreamChunk{{{DeltaContent: `{"completed":true,"need_report":true,"report":"late report"}`}}}}
	a := newTakeoverTestAgent(&fakePlatform{}, f, store, tool.NewRegistry())

	// 模拟前台已经接管过这个 Session：它现在是一个普通的前台会话。
	promoted, err := a.sessions.Resume(ctx, takeoverForegroundScope(), row.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !session.WasPromoted(promoted) {
		t.Fatalf("session was not promoted: %#v", promoted)
	}

	result, err := a.RunBackground(ctx, takeoverRunRequest(row.ID))
	if err != nil {
		t.Fatalf("RunBackground: %v", err)
	}
	if !result.TakenOver || result.Outcome != background.OutcomeTakenOver {
		t.Fatalf("result = %#v, want taken over", result)
	}
	if result.Text != "" || result.SessionID != row.ID {
		t.Fatalf("result = %#v", result)
	}
	if requests := f.chatRequests(); len(requests) != 0 {
		t.Fatalf("chat requests = %d, want 0", len(requests))
	}
	messages, err := store.Messages().ListBySession(ctx, row.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 0 {
		t.Fatalf("promoted session was written by the background run: %#v", messages)
	}
}

func TestPromotedSessionStillRunsForegroundTurns(t *testing.T) {
	store := newTestStore(t)
	row := &storage.Session{
		OwnerID:         "cli:local",
		Platform:        "cli",
		PlatformScopeID: "cron:promoted",
		Mode:            storage.SessionModeWork,
		Status:          storage.SessionStatusActive,
		Title:           "Cron: promoted",
		Metadata:        `{"title_renamed":true,"background_kind":"cron","background_name":"promoted"}`,
	}
	if err := store.Sessions().Create(context.Background(), row); err != nil {
		t.Fatalf("create session: %v", err)
	}

	// 同一个 Session 先后台跑一轮、再前台接管，随后前台继续对话。
	f := &fakeLLM{chunks: [][]llm.StreamChunk{
		{{DeltaContent: `{"completed":true,"need_report":false,"report":"background done"}`}},
		{{DeltaContent: "foreground answer"}},
	}}
	platformAdapter := &fakePlatform{}
	registry := tool.NewRegistry()
	_ = registry.Register(tool.NewDiscoverTool(registry))
	a := newTakeoverTestAgent(platformAdapter, f, store, registry)

	// 前台这一轮要落在被接管的 Session 上：复用后台 job 已经写好的那一行。
	request := takeoverRunRequest(row.ID)
	request.ToolListNames = nil
	if _, err := a.RunBackground(context.Background(), request); err != nil {
		t.Fatalf("RunBackground: %v", err)
	}

	ctx := platform.WithMessageContext(context.Background(), platform.MessageContext{Platform: "cli", PlatformUserID: "local", ActorID: "cli:local", ScopeID: "local"})
	promoted, err := a.sessions.Resume(ctx, a.scope(ctx), row.ID)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if !session.WasPromoted(promoted) || session.IsBackground(promoted) {
		t.Fatalf("resume did not promote the background session: %#v", promoted)
	}
	if err := a.HandleMessage(ctx, "continue in foreground"); err != nil {
		t.Fatalf("HandleMessage after takeover: %v", err)
	}
	if !strings.Contains(platformAdapter.out.String(), "foreground answer") {
		t.Fatalf("foreground output = %q", platformAdapter.out.String())
	}
	messages, err := store.Messages().ListBySession(ctx, row.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if !hasStoredUserMessage(messages, "continue in foreground") {
		t.Fatalf("foreground turn was blocked by the takeover gate: %#v", messages)
	}
	answerFound := false
	for _, message := range messages {
		if message.Role == storage.RoleAssistant && strings.Contains(message.Content, "foreground answer") {
			answerFound = true
		}
	}
	if !answerFound {
		t.Fatalf("foreground assistant message missing: %#v", messages)
	}
}

package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/session"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
)

// newModelSwitchTestAgent 造一个"会话历史来源与目标模型协议都可控"的 Agent：
// resp 厂商的客户端报告 Responses（服务端无状态），chat / chat2 厂商报告 Chat Completions。
func newModelSwitchTestAgent(t *testing.T) *Agent {
	t.Helper()
	a := New(&fakePlatform{}, &fakeLLM{}, "resp-model", config.ProviderConfig{}, newTestStore(t))
	a.modelRuntime.providers = map[string]config.ProviderConfig{
		"resp":  {Models: []string{"resp-model", "resp-model-2"}},
		"chat":  {Models: []string{"chat-model"}},
		"chat2": {Models: []string{"chat-model-2"}},
	}
	a.modelRuntime.clients = map[string]llm.LLM{
		"resp":  nativeCompactionClient{fakeLLM: &fakeLLM{}},
		"chat":  &fakeLLM{},
		"chat2": &fakeLLM{},
	}
	a.modelRuntime.modeModels = map[string]config.ModelSelection{
		storage.SessionModeWork: {Provider: "resp", Model: "resp-model"},
	}
	return a
}

func newOriginSession(t *testing.T, a *Agent, origin config.ModelSelection) *storage.Session {
	t.Helper()
	ctx := context.Background()
	created, err := a.sessions.Create(ctx, a.scope(ctx), session.CreateRequest{Title: "switch"})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	a.recordLLMOrigin(ctx, created, origin)
	return created
}

func TestSelectModelRefusesIncompatibleProtocolForCurrentSession(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	created := newOriginSession(t, a, config.ModelSelection{Provider: "resp", Model: "resp-model"})

	_, err := a.SelectModel(ctx, "chat-model")
	if err == nil {
		t.Fatal("跨协议切换必须被拒绝")
	}
	if !strings.Contains(err.Error(), "not compatible") {
		t.Fatalf("error = %v", err)
	}
	// 拒绝时不能改内存：否则命令报了失败、选择却已经落地。
	if got := a.modelForMode(created.Mode); got.Provider != "resp" || got.Model != "resp-model" {
		t.Fatalf("mode model changed after refusal: %#v", got)
	}
}

func TestSelectModelRefusesResponsesSwitchAcrossProviders(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	a.modelRuntime.providers["resp2"] = config.ProviderConfig{Models: []string{"resp-model-3"}}
	a.modelRuntime.clients["resp2"] = nativeCompactionClient{fakeLLM: &fakeLLM{}}
	newOriginSession(t, a, config.ModelSelection{Provider: "resp", Model: "resp-model"})

	_, err := a.SelectModel(ctx, "resp2/resp-model-3")
	if err == nil || !strings.Contains(err.Error(), "not compatible") {
		t.Fatalf("responses 材料跨厂商必须被拒绝, error = %v", err)
	}
}

func TestSelectModelAllowsResponsesSwitchOnSameProvider(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	created := newOriginSession(t, a, config.ModelSelection{Provider: "resp", Model: "resp-model"})

	if _, err := a.SelectModel(ctx, "resp-model-2"); err != nil {
		t.Fatalf("同厂商同协议切换应放行: %v", err)
	}
	if got := a.modelForMode(created.Mode); got.Provider != "resp" || got.Model != "resp-model-2" {
		t.Fatalf("mode model = %#v", got)
	}
}

func TestSelectModelAllowsChatAcrossVendors(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	created := newOriginSession(t, a, config.ModelSelection{Provider: "chat", Model: "chat-model"})

	if _, err := a.SelectModel(ctx, "chat2/chat-model-2"); err != nil {
		t.Fatalf("chat 材料跨厂商应放行: %v", err)
	}
	if got := a.modelForMode(created.Mode); got.Model != "chat-model-2" {
		t.Fatalf("mode model = %#v", got)
	}
}

func TestSelectModelAllowsSwitchWithoutRecordedOrigin(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	// 不记录 llm_origin：旧会话没有这个事实，宁可让它试一次，也不要凭猜测拦人。
	if _, err := a.sessions.Create(ctx, a.scope(ctx), session.CreateRequest{Title: "legacy"}); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, err := a.SelectModel(ctx, "chat-model"); err != nil {
		t.Fatalf("没有来源记录的会话应放行: %v", err)
	}
}

func TestSelectModelForModeSkipsCheckForOtherModeSlots(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	created := newOriginSession(t, a, config.ModelSelection{Provider: "resp", Model: "resp-model"})

	other := storage.SessionModeChat
	if created.Mode == other {
		other = storage.SessionModeWork
	}
	if _, err := a.SelectModelForMode(ctx, other, "chat-model"); err != nil {
		t.Fatalf("切别的模式槽位不该被当前会话的历史拦住: %v", err)
	}
	if got := a.modelForMode(other); got.Provider != "chat" || got.Model != "chat-model" {
		t.Fatalf("other mode model = %#v", got)
	}
}

// `/model --apply` 也是切换当前会话正在用的模式，必须走同一个裁决，否则它就是个绕过口。
func TestApplyModelSnapshotRefusesIncompatibleCurrentMode(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	created := newOriginSession(t, a, config.ModelSelection{Provider: "resp", Model: "resp-model"})
	a.modelSnapshots = map[string]config.StateModelSnapshot{
		"mixed": {ModeModels: map[string]config.ModelSelection{
			created.Mode: {Provider: "chat", Model: "chat-model"},
		}},
	}

	_, err := a.ApplyModelSnapshot(ctx, "mixed")
	if err == nil || !strings.Contains(err.Error(), "not compatible") {
		t.Fatalf("快照把当前会话切到不兼容协议时必须拒绝, error = %v", err)
	}
	if got := a.modelForMode(created.Mode); got.Provider != "resp" || got.Model != "resp-model" {
		t.Fatalf("mode model changed after refusal: %#v", got)
	}
}

func TestApplyModelSnapshotAppliesCompatibleCurrentMode(t *testing.T) {
	ctx := context.Background()
	a := newModelSwitchTestAgent(t)
	created := newOriginSession(t, a, config.ModelSelection{Provider: "resp", Model: "resp-model"})
	a.modelSnapshots = map[string]config.StateModelSnapshot{
		"next": {ModeModels: map[string]config.ModelSelection{
			created.Mode: {Provider: "resp", Model: "resp-model-2"},
		}},
	}

	if _, err := a.ApplyModelSnapshot(ctx, "next"); err != nil {
		t.Fatalf("同厂商同协议的快照应能应用: %v", err)
	}
	if got := a.modelForMode(created.Mode); got.Model != "resp-model-2" {
		t.Fatalf("mode model = %#v", got)
	}
}

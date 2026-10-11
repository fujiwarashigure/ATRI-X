package agent

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/logging"
	"github.com/fujiwarashigure/ATRI-X/internal/security"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

// 工具日志正文的预算按类别取值：读取／查询／说明类 128，其余（执行、编辑、写入、
// 媒体、后台任务）256；未登记的工具按执行类处理。
func TestToolLogResultLimitFollowsToolCategory(t *testing.T) {
	for _, name := range []string{"read_file", "web_search", "web_extract", "discover_tool", "agent_skill", "angel_recall"} {
		if got := toolLogResultLimit(name); got != toolLogQueryLimit {
			t.Fatalf("toolLogResultLimit(%q) = %d, want %d", name, got, toolLogQueryLimit)
		}
	}
	for _, name := range []string{"shell", "edit_file", "rollback_file", "send_file", "view_image", "get_media", "cron", "atriwisp_creator", "chat_history", "", "brand_new_tool"} {
		if got := toolLogResultLimit(name); got != toolLogActionLimit {
			t.Fatalf("toolLogResultLimit(%q) = %d, want %d", name, got, toolLogActionLimit)
		}
	}
	if got := toolLogResultLimit(" read_file "); got != toolLogQueryLimit {
		t.Fatalf("toolLogResultLimit must ignore surrounding spaces, got %d", got)
	}
	if toolLogQueryLimit >= toolLogActionLimit {
		t.Fatalf("query limit %d must stay below the action limit %d", toolLogQueryLimit, toolLogActionLimit)
	}
}

func TestPreviewRunesCountsRunesAndMarksTruncation(t *testing.T) {
	if got := previewRunes("  hello  ", 16); got != "hello" {
		t.Fatalf("previewRunes trimmed = %q", got)
	}
	if got := previewRunes("", 16); got != "" {
		t.Fatalf("empty previewRunes = %q", got)
	}
	// 上限按 rune 计，不能把多字节字符切成半个。
	long := strings.Repeat("喵", 200)
	got := previewRunes(long, toolLogQueryLimit)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated preview must be marked: %q", got)
	}
	if runes := []rune(strings.TrimSuffix(got, "...")); len(runes) != toolLogQueryLimit {
		t.Fatalf("truncated preview kept %d runes, want %d", len(runes), toolLogQueryLimit)
	}
	if strings.Contains(got, "\uFFFD") {
		t.Fatalf("truncation split a multi-byte rune: %q", got)
	}
	if got := previewRunes("short", 16); got != "short" {
		t.Fatalf("previewRunes under the limit = %q", got)
	}
}

// 运行来源只看调用现场：有 SandboxContext 的后台标记就是 cron/atrinis，否则是前台；
// 后台但没有可辨识来源时宁可不写，也不假装成前台。
func TestToolRunSourceFollowsSandboxContext(t *testing.T) {
	if got := toolRunSource(context.Background()); got != logging.RunSourceForeground {
		t.Fatalf("plain context = %q, want %q", got, logging.RunSourceForeground)
	}
	cronCtx := tool.WithSandboxContext(context.Background(), tool.SandboxContext{Background: true, BackgroundKind: tool.BackgroundKindCron})
	if got := toolRunSource(cronCtx); got != logging.RunSourceCron {
		t.Fatalf("cron context = %q, want %q", got, logging.RunSourceCron)
	}
	atrinisCtx := tool.WithSandboxContext(context.Background(), tool.SandboxContext{Background: true, BackgroundKind: tool.BackgroundKindAtrinis})
	if got := toolRunSource(atrinisCtx); got != logging.RunSourceAtrinis {
		t.Fatalf("atrinis context = %q, want %q", got, logging.RunSourceAtrinis)
	}
	unknownCtx := tool.WithSandboxContext(context.Background(), tool.SandboxContext{Background: true})
	if got := toolRunSource(unknownCtx); got != "" {
		t.Fatalf("unknown background source = %q, want empty (must not claim foreground)", got)
	}
}

// 工具调用审计记录必须带 run_source，前台与后台各写一次。
func TestRecordToolCallWritesRunSource(t *testing.T) {
	recorded := func(ctx context.Context) string {
		var output bytes.Buffer
		logger := slog.New(slog.NewTextHandler(&output, nil))
		a := &Agent{}
		a.SetLogManager(fakeLogManager{runtime: logger, audit: logger})
		actorCtx := security.WithActor(ctx, security.Actor{ID: "cli:local", Platform: "cli", PlatformUserID: "local"})
		a.recordToolCall(actorCtx, "s1", llm.ToolCallRequest{ID: "call_1", Name: "shell", Arguments: `{"cmd":"ls"}`}, "low", time.Now(), "ok", nil)
		return output.String()
	}

	foreground := recorded(context.Background())
	if !strings.Contains(foreground, "event=tool_call") || !strings.Contains(foreground, logging.FieldRunSource+"="+logging.RunSourceForeground) {
		t.Fatalf("foreground audit record missing run_source: %s", foreground)
	}
	cron := recorded(tool.WithSandboxContext(context.Background(), tool.SandboxContext{Background: true, BackgroundKind: tool.BackgroundKindCron}))
	if !strings.Contains(cron, logging.FieldRunSource+"="+logging.RunSourceCron) {
		t.Fatalf("cron audit record missing run_source: %s", cron)
	}
	if !strings.Contains(cron, "tool=shell") {
		t.Fatalf("cron audit record lost the tool name: %s", cron)
	}
}

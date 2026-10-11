package agent

import (
	"strings"
	"testing"
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

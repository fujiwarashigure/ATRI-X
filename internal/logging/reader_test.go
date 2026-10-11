package logging

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeLogFile(t *testing.T, dir, prefix, content string) {
	t.Helper()
	path := filepath.Join(dir, prefix+"-"+time.Now().Format("2006-01-02")+".log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

func TestReaderParsesAndFiltersTextLogs(t *testing.T) {
	dir := t.TempDir()
	content := "time=\"2026-06-03 15:00:00\" level=INFO msg=\"audit event\" event=tool_call risk=high tool=shell error=\"bad thing\"\n" +
		"time=\"2026-06-03 15:01:00\" level=INFO msg=\"audit event\" event=llm_usage model=foo total_tokens=42\n"
	writeLogFile(t, dir, "audit", content)

	scanned, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{
		Prefix: "audit",
		Fields: map[string]string{"event": "tool_call", "risk": "high"},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(scanned.Entries) != 1 {
		t.Fatalf("entries = %#v", scanned.Entries)
	}
	entry := scanned.Entries[0]
	if entry.Message != "audit event" || entry.Fields["tool"] != "shell" || entry.Fields["error"] != "bad thing" {
		t.Fatalf("entry = %#v", entry)
	}
	if scanned.DamagedCount != 0 || len(scanned.DamagedLines) != 0 {
		t.Fatalf("clean log reported damage: %#v", scanned)
	}
}

func TestReaderContainsMatchesStructuredFieldsAndRaw(t *testing.T) {
	dir := t.TempDir()
	content := "time=\"2026-06-03 15:00:00\" level=INFO msg=\"user input\" event=user_message text=\"hello world\"\n" +
		"time=\"2026-06-03 15:01:00\" level=INFO msg=\"tool call\" event=tool_call arguments=\"{\\\"path\\\":\\\"a.txt\\\"}\" result=\"file content\"\n" +
		"time=\"2026-06-03 15:02:00\" level=INFO msg=other custom_field=needle\n"
	writeLogFile(t, dir, "atri-x", content)

	scanned, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "atri-x", Contains: "file content"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(scanned.Entries) != 1 || scanned.Entries[0].Fields["event"] != "tool_call" {
		t.Fatalf("entries = %#v", scanned.Entries)
	}

	scanned, err = (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "atri-x", Contains: "needle"})
	if err != nil {
		t.Fatalf("Query raw: %v", err)
	}
	if len(scanned.Entries) != 1 || scanned.Entries[0].Message != "other" {
		t.Fatalf("entries = %#v", scanned.Entries)
	}
}

func TestReaderFieldContainsMatchesOnlyRequestedField(t *testing.T) {
	dir := t.TempDir()
	content := "time=\"2026-06-03 15:00:00\" level=INFO msg=\"atrinis event accepted\" tags=\"[\\\"windows\\\",\\\"onedrive\\\"]\" raw_text=watchdog\n" +
		"time=\"2026-06-03 15:01:00\" level=INFO msg=\"atrinis event accepted\" tags=\"[\\\"watchdog\\\"]\" raw_text=onedrive\n"
	writeLogFile(t, dir, "atrinis", content)

	scanned, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "atrinis", FieldContains: map[string][]string{"tags": {"\"onedrive\""}}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(scanned.Entries) != 1 || scanned.Entries[0].Fields["tags"] != "[\"windows\",\"onedrive\"]" {
		t.Fatalf("entries = %#v", scanned.Entries)
	}

	scanned, err = (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "atrinis", FieldContains: map[string][]string{"tags": {"\"windows\"", "\"onedrive\""}}})
	if err != nil {
		t.Fatalf("Query both tags: %v", err)
	}
	if len(scanned.Entries) != 1 || scanned.Entries[0].Fields["tags"] != "[\"windows\",\"onedrive\"]" {
		t.Fatalf("entries = %#v", scanned.Entries)
	}
}

func TestReaderAppliesMinLevelAndLimit(t *testing.T) {
	dir := t.TempDir()
	content := "time=\"2026-06-03 15:00:00\" level=DEBUG msg=debug\n" +
		"time=\"2026-06-03 15:01:00\" level=INFO msg=info\n" +
		"time=\"2026-06-03 15:02:00\" level=ERROR msg=error\n"
	writeLogFile(t, dir, "atri-x", content)

	scanned, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "atri-x", MinLevel: "info", Limit: 1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(scanned.Entries) != 1 || scanned.Entries[0].Message != "error" {
		t.Fatalf("entries = %#v", scanned.Entries)
	}
}

// 损坏行（解析不出 time/level/msg 的行）必须被跳过并计数：既不能占用 --limit 配额，
// 也不能在结果里冒充一条零字段记录。样本只在 --raw 查询里收集。
func TestReaderSkipsAndCountsDamagedLines(t *testing.T) {
	dir := t.TempDir()
	content := "this is a damaged line not key=value at all\n" +
		"{\"time\":\"2026-06-03T15:00:00Z\"}\n" +
		"\n" +
		"time=\"2026-06-03 15:00:00\" level=INFO msg=\"audit event\" event=tool_call tool=shell\n" +
		"time=\"2026-06-03 15:01:00\" level=INFO msg=\"audit event\" event=tool_call tool=web_search\n" +
		"truncated tail without any field\n"
	writeLogFile(t, dir, "audit", content)

	scanned, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "audit", Limit: 2, MinLevel: "debug"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if scanned.DamagedCount != 3 {
		t.Fatalf("DamagedCount = %d, want 3", scanned.DamagedCount)
	}
	// 空白行不算损坏，损坏行也不吃配额：limit=2 必须拿到两条真实记录。
	if len(scanned.Entries) != 2 || scanned.Entries[0].Fields["tool"] != "web_search" {
		t.Fatalf("entries = %#v", scanned.Entries)
	}
	for _, entry := range scanned.Entries {
		if entry.Message == "" || entry.Level == "" {
			t.Fatalf("damaged line leaked into the result: %#v", entry)
		}
	}
	if len(scanned.DamagedLines) != 0 {
		t.Fatalf("samples must only be collected for --raw queries: %#v", scanned.DamagedLines)
	}

	raw, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "audit", Raw: true, MinLevel: "debug"})
	if err != nil {
		t.Fatalf("Query raw: %v", err)
	}
	if raw.DamagedCount != 3 || len(raw.DamagedLines) != 3 {
		t.Fatalf("raw query damage = %#v", raw)
	}
	if !strings.Contains(raw.DamagedLines[0], "damaged line") {
		t.Fatalf("damaged samples = %#v", raw.DamagedLines)
	}
}

func TestReaderCapsDamagedSamples(t *testing.T) {
	dir := t.TempDir()
	var content strings.Builder
	for i := 0; i < damagedSampleLimit+5; i++ {
		content.WriteString("damaged line without fields\n")
	}
	content.WriteString("time=\"2026-06-03 15:00:00\" level=INFO msg=\"audit event\"\n")
	writeLogFile(t, dir, "audit", content.String())

	scanned, err := (Reader{Dir: dir}).Query(context.Background(), LogQuery{Prefix: "audit", Raw: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if scanned.DamagedCount != damagedSampleLimit+5 {
		t.Fatalf("DamagedCount = %d, want %d", scanned.DamagedCount, damagedSampleLimit+5)
	}
	if len(scanned.DamagedLines) != damagedSampleLimit {
		t.Fatalf("sampled %d damaged lines, want %d", len(scanned.DamagedLines), damagedSampleLimit)
	}
}

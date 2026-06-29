package skill

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

func TestFinalizeAtrixSkillTextOnlyReloads(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "textonly", "#skill textonly - Text.\n** risk low\n<- $text:str!\n-> $result:str\n")
	registry := tool.NewRegistry()
	finalizer := NewFinalizeAtrixSkillTool(NewManager(root, registry))

	result, err := finalizer.Call(context.Background(), tool.CallRequest{Arguments: []byte(`{"name":"textonly"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Go source: not found") || !strings.Contains(result.Content, "reload: ok") {
		t.Fatalf("result = %q", result.Content)
	}
	if registered, ok := registry.Get("textonly"); !ok || registered.Info().Risk != tool.RiskLow {
		t.Fatalf("registered=%#v ok=%v", registered, ok)
	}
}

func TestFinalizeAtrixSkillReturnsAtriyphWarnings(t *testing.T) {
	root := t.TempDir()
	writeTestSkill(t, root, "warn_finalize", "#skill warn_finalize - Text.\n** 清单：\n")
	finalizer := NewFinalizeAtrixSkillTool(NewManager(root, tool.NewRegistry()))

	result, err := finalizer.Call(context.Background(), tool.CallRequest{Arguments: []byte(`{"name":"warn_finalize"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Atriyph: ok") || !strings.Contains(result.Content, "Atriyph warnings:") || !strings.Contains(result.Content, "line 2:") {
		t.Fatalf("result = %q", result.Content)
	}
}

func TestFinalizeAtrixSkillFormatsBuildsAndReloads(t *testing.T) {
	root := t.TempDir()
	writeTestGoSkill(t, root, "finalize", "package main\nfunc main(){println(\"ok\")}\n")
	registry := tool.NewRegistry()
	finalizer := NewFinalizeAtrixSkillTool(NewManager(root, registry))

	result, err := finalizer.Call(context.Background(), tool.CallRequest{Arguments: []byte(`{"name":"finalize"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "gofmt: changed") || !strings.Contains(result.Content, "go build: ok") || !strings.Contains(result.Content, "reload: ok") {
		t.Fatalf("result = %q", result.Content)
	}
	if got := readTestGoSource(t, root, "finalize"); got != "package main\n\nfunc main() { println(\"ok\") }\n" {
		t.Fatalf("formatted source = %q", got)
	}
	binary := filepath.Join(root, "go", "finalize", "finalize")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("binary missing: %v", err)
	}
	if registered, ok := registry.Get("finalize"); !ok || registered.Info().Source != tool.SourceSkillGo {
		t.Fatalf("registered=%#v ok=%v", registered, ok)
	}
}

func TestFinalizeAtrixSkillReturnsBuildFailure(t *testing.T) {
	root := t.TempDir()
	writeTestGoSkill(t, root, "broken", "package main\n\nfunc main() { missing() }\n")
	finalizer := NewFinalizeAtrixSkillTool(NewManager(root, tool.NewRegistry()))

	result, err := finalizer.Call(context.Background(), tool.CallRequest{Arguments: []byte(`{"name":"broken"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "go build: failed") || !strings.Contains(result.Content, "stderr:") || !strings.Contains(result.Content, "missing") {
		t.Fatalf("result = %q", result.Content)
	}
}

func TestFinalizeAtrixSkillReturnsGofmtFailure(t *testing.T) {
	root := t.TempDir()
	writeTestGoSkill(t, root, "badfmt", "package main\n\nfunc main( {\n")
	finalizer := NewFinalizeAtrixSkillTool(NewManager(root, tool.NewRegistry()))

	result, err := finalizer.Call(context.Background(), tool.CallRequest{Arguments: []byte(`{"name":"badfmt"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "gofmt: failed") {
		t.Fatalf("result = %q", result.Content)
	}
}

func TestFinalizeAtrixSkillReturnsAtriyphFailure(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "go", "badatriyph")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.atriyph"), []byte("missing header\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	finalizer := NewFinalizeAtrixSkillTool(NewManager(root, tool.NewRegistry()))
	args, _ := json.Marshal(map[string]string{"name": "badatriyph"})

	result, err := finalizer.Call(context.Background(), tool.CallRequest{Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "Atriyph: failed") {
		t.Fatalf("result = %q", result.Content)
	}
}

package builtin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

func TestAtriwispCreatorToolInfo(t *testing.T) {
	info := NewAtriwispCreatorTool().Info()
	if info.Name != AtriwispCreatorName {
		t.Fatalf("name = %q", info.Name)
	}
	if info.Source != tool.SourceBuiltin {
		t.Fatalf("source = %q", info.Source)
	}
	if info.Risk != tool.RiskLow {
		t.Fatalf("risk = %q", info.Risk)
	}
	if !info.SuperadminOnly {
		t.Fatal("expected superadmin-only tool")
	}
	wantDeps := []string{"read_file", "edit_file", "shell"}
	if strings.Join(info.DependsOn, ",") != strings.Join(wantDeps, ",") {
		t.Fatalf("depends_on = %#v, want %#v", info.DependsOn, wantDeps)
	}
}

func TestAtriwispCreatorToolSchemaHasNoParameters(t *testing.T) {
	schema := NewAtriwispCreatorTool().Schema()
	if schema.Function.Name != AtriwispCreatorName {
		t.Fatalf("schema name = %q", schema.Function.Name)
	}
	properties, ok := schema.Function.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties = %#v", schema.Function.Parameters["properties"])
	}
	if len(properties) != 0 {
		t.Fatalf("expected no parameters, got %#v", properties)
	}
	if _, ok := schema.Function.Parameters["required"]; ok {
		t.Fatalf("unexpected required parameters: %#v", schema.Function.Parameters["required"])
	}
}

func TestAtriwispCreatorToolCallReturnsGuide(t *testing.T) {
	result, err := NewAtriwispCreatorTool().Call(context.Background(), tool.CallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || !strings.Contains(result.Content, "#task create_atriwisp") || !strings.Contains(result.Content, "Atriena v3") || !strings.Contains(result.Content, "atriyph_rule_card") {
		t.Fatalf("unexpected guide content: %#v", result)
	}
	if _, err := atriyph.ParseTask(result.Content, "create_atriwisp"); err != nil {
		t.Fatalf("guide is not valid Atriyph: %v\n%s", err, result.Content)
	}
	if !strings.Contains(result.Content, "token 值请设置到系统环境变量或配置目录 .env") {
		t.Fatalf("guide missing token setup hint: %s", result.Content)
	}
	if !strings.Contains(result.Content, "// "+strings.Split(atriyph.RuleCard(), "\n")[0]) {
		t.Fatalf("guide missing shared Atriyph rule card: %s", result.Content)
	}
	for _, forbidden := range []string{"~ 硬编码 token", "~ 记录 token", "~ 编造 endpoint/token"} {
		if !strings.Contains(result.Content, forbidden) {
			t.Fatalf("guide missing forbidden rule %q: %s", forbidden, result.Content)
		}
	}
}

func TestAtriwispCreatorGuideIncludesConfigSummary(t *testing.T) {
	dir := t.TempDir()
	appPath := filepath.Join(dir, "app.toml")
	providersPath := filepath.Join(dir, "providers.toml")
	atrinisPath := filepath.Join(dir, "atrinis.toml")
	writeAtriwispCreatorTestFile(t, appPath, `
[config_files]
providers = "providers.toml"
atrinis = "atrinis.toml"

[mode_models.work]
provider = "test"
model = "work-model"

[mode_models.chat]
provider = "test"
model = "chat-model"
`)
	writeAtriwispCreatorTestFile(t, providersPath, `
[providers.test]
base_url = "https://example.invalid"
models = ["work-model", "chat-model"]
`)
	writeAtriwispCreatorTestFile(t, atrinisPath, `
enabled = true
allowed_tools = ["web_extract", "web_search"]

[http]
addr = "127.0.0.1:45678"

[tokens.home]
token_env = ["ATRINIS_HOME_TOKEN"]

`)
	t.Setenv(config.EnvConfigFile, appPath)

	result, err := NewAtriwispCreatorTool().Call(context.Background(), tool.CallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"endpoint: http://127.0.0.1:45678/atriena/v3/events",
		"token_env: [ATRINIS_HOME_TOKEN]",
		"allowed_tools: [web_extract, web_search]",
	} {
		if !strings.Contains(result.Content, want) {
			t.Fatalf("guide missing %q: %s", want, result.Content)
		}
	}
}

func TestAtriwispCreatorGuideWarnsWhenConfigMissing(t *testing.T) {
	t.Setenv(config.EnvConfigFile, filepath.Join(t.TempDir(), "missing.toml"))

	result, err := NewAtriwispCreatorTool().Call(context.Background(), tool.CallRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Content, "读取 ATRI-X 配置失败") || !strings.Contains(result.Content, "修复配置") {
		t.Fatalf("guide missing config warning: %s", result.Content)
	}
}

func writeAtriwispCreatorTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestRegisterAllIncludesAtriwispCreator(t *testing.T) {
	registry := tool.NewRegistry()
	if err := RegisterAll(registry, RegisterOptions{}); err != nil {
		t.Fatal(err)
	}
	registered, ok := registry.Get(AtriwispCreatorName)
	if !ok {
		t.Fatal("atriwisp_creator not registered")
	}
	if got := strings.Join(registered.Info().DependsOn, ","); got != "read_file,edit_file,shell" {
		t.Fatalf("depends_on = %q", got)
	}
	details, errors := registry.DiscoverDetails(context.Background(), []string{AtriwispCreatorName}, func(tool.Tool) bool { return true })
	if len(errors) > 0 {
		t.Fatalf("discover errors = %#v", errors)
	}
	seen := map[string]bool{}
	for _, detail := range details {
		seen[detail.Info.Name] = true
	}
	for _, name := range []string{AtriwispCreatorName, "read_file", "edit_file", "shell"} {
		if !seen[name] {
			t.Fatalf("discover details missing %s: %#v", name, details)
		}
	}
}

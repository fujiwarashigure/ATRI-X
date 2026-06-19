package builtin

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/atriyph"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

const AtriwispCreatorName = "atriwisp_creator"

type AtriwispCreatorTool struct{}

type atriwispCreatorConfigSnapshot struct {
	AppConfigPath    string
	AtrinisConfigPath  string
	Enabled          bool
	Addr             string
	TokenNames       []string
	TokenEnvNames    []string
	AllowedTools     []string
	DefaultPlatforms []string
	AllowSuperadmins bool
	Warnings         []string
}

func NewAtriwispCreatorTool() AtriwispCreatorTool {
	return AtriwispCreatorTool{}
}

func (AtriwispCreatorTool) Name() string {
	return AtriwispCreatorName
}

func (AtriwispCreatorTool) Info() tool.Info {
	return tool.NewBuilder(AtriwispCreatorName).
		Description("获取创建 Atriwisp 监听器所需的精简 Atrinis/Atriena/Atriyph 说明，并附带当前 Atrinis 配置摘要。").
		Source(tool.SourceBuiltin).
		Risk(tool.RiskLow).
		SuperadminOnly().
		DependsOn("read_file", "edit_file", "shell").
		BuildInfo()
}

func (AtriwispCreatorTool) Schema() llm.ToolSchema {
	return tool.NewBuilder(AtriwispCreatorName).
		Description("无参数。调用后返回精简的 Atriwisp 创建任务卡，包含当前 Atrinis 配置摘要、Atriena 协议、Atriyph 写法和安全注意事项。").
		BuildSchema()
}

func (AtriwispCreatorTool) Call(ctx context.Context, req tool.CallRequest) (*tool.Result, error) {
	_ = req
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &tool.Result{Content: buildAtriwispCreatorGuide(loadAtriwispCreatorConfig())}, nil
}

func loadAtriwispCreatorConfig() atriwispCreatorConfigSnapshot {
	appConfigPath, ok := findAtriwispCreatorAppConfigPath()
	if !ok {
		return atriwispCreatorConfigSnapshot{Warnings: []string{
			"未找到 ATRI-X app.toml，无法确认 Atrinis endpoint 和 token 配置。请提示用户先配置 Atrinis，或让用户要求 Agent 修改 app.toml / atrinis.toml / .env。",
		}}
	}

	cfg, err := config.Load(appConfigPath)
	if err != nil {
		return atriwispCreatorConfigSnapshot{
			AppConfigPath: appConfigPath,
			Warnings: []string{
				fmt.Sprintf("读取 ATRI-X 配置失败：%v。请提示用户修复配置后再创建 Atriwisp。", err),
			},
		}
	}

	tokenNames := make([]string, 0, len(cfg.Atrinis.Tokens))
	tokenEnvSet := map[string]bool{}
	for name, token := range cfg.Atrinis.Tokens {
		tokenNames = append(tokenNames, name)
		for _, envName := range token.TokenEnv {
			envName = strings.TrimSpace(envName)
			if envName != "" {
				tokenEnvSet[envName] = true
			}
		}
	}
	sort.Strings(tokenNames)
	tokenEnvNames := make([]string, 0, len(tokenEnvSet))
	for envName := range tokenEnvSet {
		tokenEnvNames = append(tokenEnvNames, envName)
	}
	sort.Strings(tokenEnvNames)

	snapshot := atriwispCreatorConfigSnapshot{
		AppConfigPath:    cfg.ConfigPath,
		AtrinisConfigPath:  cfg.AtrinisConfigPath,
		Enabled:          cfg.Atrinis.Enabled,
		Addr:             strings.TrimSpace(cfg.Atrinis.HTTP.Addr),
		TokenNames:       tokenNames,
		TokenEnvNames:    tokenEnvNames,
		AllowedTools:     append([]string(nil), cfg.Atrinis.AllowedTools...),
		DefaultPlatforms: append([]string(nil), cfg.Atrinis.Delivery.DefaultPlatforms...),
		AllowSuperadmins: cfg.Atrinis.Delivery.AllowSuperadmins,
	}
	sort.Strings(snapshot.AllowedTools)
	sort.Strings(snapshot.DefaultPlatforms)

	if !snapshot.Enabled {
		snapshot.Warnings = append(snapshot.Warnings, "当前 Atrinis 未启用。测试 Atriwisp 前，请提示用户设置 atrinis.toml 的 enabled=true。")
	}
	if snapshot.Addr == "" {
		snapshot.Warnings = append(snapshot.Warnings, "未读取到 Atrinis HTTP addr。请提示用户配置 atrinis.toml 的 [http].addr。")
	}
	if len(snapshot.TokenEnvNames) == 0 {
		snapshot.Warnings = append(snapshot.Warnings, "当前未配置 Atrinis token_env。请提示用户配置 token_env，并在系统环境变量或配置目录 .env 中保存 token。")
	}
	if len(snapshot.DefaultPlatforms) == 0 {
		snapshot.Warnings = append(snapshot.Warnings, "当前未配置 Atrinis delivery.default_platforms。请提示用户确认要投递的平台，或让用户配置 atrinis.toml 的 [delivery].default_platforms。")
	}
	return snapshot
}

func findAtriwispCreatorAppConfigPath() (string, bool) {
	if envPath := strings.TrimSpace(os.Getenv(config.EnvConfigFile)); envPath != "" {
		return filepath.Clean(envPath), true
	}
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		name := config.XDGAppDirName
		if runtime.GOOS == "windows" {
			name = config.AppDirName
		}
		path := filepath.Join(dir, name, "app.toml")
		if fileExists(path) {
			return path, true
		}
	}
	return "", false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func buildAtriwispCreatorGuide(cfg atriwispCreatorConfigSnapshot) string {
	var b strings.Builder
	b.WriteString(`#task create_atriwisp - 帮用户设计或创建 Atriwisp 监听器
<- $user_goal:str!
<- $atrinis_config:object?
-> $design:object
-> $files:list
-> $test_commands:list
-> $security_notes:list
// brief: Atrinis 是 ATRI-X 内部事件监听枢纽；Atriwisp 是外部监听器；Atriena v1 是 Atriwisp 向 Atrinis 投递事件的 JSON over HTTP 协议。
// tool_scope: 这个工具只提供创建指引；读文件用 read_file，写文件用 edit_file，测试命令用 shell。
// roles: Atrinis 负责鉴权、协议校验、去重、记录、目标裁决，并按 record/direct/llm 分发。
// roles: Atriwisp 负责观察服务器、RSS、Webhook、日志、脚本、游戏或设备状态，并上报 Atriena 事件。
// roles: Atriena 统一描述来源、事件 id、内容、处理模式、目标请求和可选工具声明。
`)
	writeAtriwispCreatorConfig(&b, cfg)
	b.WriteString(`// atriena_v1: endpoint
`)
	if cfg.Addr != "" {
		b.WriteString("// POST ")
		b.WriteString(endpointURL(cfg.Addr, "/atriena/v1/events"))
		b.WriteByte('\n')
		b.WriteString("// GET  ")
		b.WriteString(endpointURL(cfg.Addr, "/healthz"))
		b.WriteByte('\n')
	} else {
		b.WriteString("// endpoint: unknown; ask user to configure atrinis.toml [http].addr\n")
	}
	b.WriteString(`// auth: Authorization: Bearer $TOKEN
// auth: X-Atrinis-Token: $TOKEN
// token_setup: token_env 只声明变量名，token 值请设置到系统环境变量或配置目录 .env。
// required_fields: version="atriena.v1", atriwisp.name, source, id, mode, content
// modes: record 只记录；direct 直接通知；llm 进入后台 LLM Session。
// optional_fields: atriwisp.tags, created_at, title, format, model_slot, tool_list_names, tools, targets, meta
// model_slot: atriwisp1 | atriwisp2 | atriwisp3；未配置时由 Atrinis fallback。
// minimal_direct_payload: {"version":"atriena.v1","atriwisp":{"name":"example-atriwisp","tags":["example"]},"source":"example-source","id":"stable-event-id","mode":"direct","title":"事件标题","content":"可直接通知给管理员的文本。","targets":{"platforms":["cli"],"superadmins":true}}
// minimal_llm_payload: {"version":"atriena.v1","atriwisp":{"name":"example-atriwisp","tags":["example"]},"source":"example-source","id":"stable-event-id","mode":"llm","title":"需要分析的事件","format":"atriyph","content":"#task review_event - 判断事件是否需要通知\n<- $event:object!\n-> $report:str\n** 基于事件内容、meta 和工具结果判断\n~ 编造日志、指标或结论\n> 如果需要通知，给出原因和建议；否则说明无需打扰。","tool_list_names":[],"targets":{"platforms":["cli"],"superadmins":true},"meta":{}}
`)
	writeGuideCommentBlock(&b, "atriyph_rule_card", atriyph.RuleCard())
	b.WriteString(`** 如果 current_atrinis 有 warning，先提醒用户处理。
** event id 在同一 source 内稳定且唯一。
** Atriwisp 只上报事件，由 Atrinis/ATRI-X 决定记录、通知、分析或调用工具。
** 真实文件修改使用 edit_file；本地检查、curl 或脚本测试使用 shell。
** token 值由用户设置到系统环境变量或配置目录 .env；Atriwisp 代码只读取环境变量。
** 投递平台优先参考 current_atrinis.default_platforms；为空或用户目标不明确时，先询问用户要推送到哪些平台。
** tool_list_names 请求 ATRI-X 内部工具或 Skill，最终可用性由 atrinis.toml allowed_tools、ToolRun 和 Security Policy 裁决。
** tools 是 Atriwisp 随事件声明的外部工具，适合查询 Atriwisp 所在环境的状态、日志或详情。
** Atriwisp 外部工具 endpoint 默认绑定 127.0.0.1，除非用户明确需要远程访问。
~ 硬编码 token
~ 记录 token
~ 直接读取系统环境变量或配置目录 .env 中的 token/key 值
~ 编造 endpoint/token
~ 让 Atriwisp 直接向聊天平台发消息
> 创建或修改 Atriwisp 后，简要说明设计、文件路径、配置项、环境变量、测试命令和安全注意事项。
`)
	return b.String()
}

func writeAtriwispCreatorConfig(b *strings.Builder, cfg atriwispCreatorConfigSnapshot) {
	b.WriteString("// current_atrinis\n")
	writeGuideField(b, "app_config", cfg.AppConfigPath)
	writeGuideField(b, "atrinis_config", cfg.AtrinisConfigPath)
	writeGuideField(b, "enabled", fmt.Sprintf("%t", cfg.Enabled))
	if cfg.Addr != "" {
		writeGuideField(b, "endpoint", endpointURL(cfg.Addr, "/atriena/v1/events"))
		writeGuideField(b, "healthz", endpointURL(cfg.Addr, "/healthz"))
	} else {
		writeGuideField(b, "endpoint", "unknown")
	}
	writeGuideList(b, "token_names", cfg.TokenNames)
	writeGuideList(b, "token_env", cfg.TokenEnvNames)
	writeGuideList(b, "allowed_tools", cfg.AllowedTools)
	writeGuideList(b, "default_platforms", cfg.DefaultPlatforms)
	writeGuideField(b, "allow_superadmins", fmt.Sprintf("%t", cfg.AllowSuperadmins))
	for _, warning := range cfg.Warnings {
		writeGuideField(b, "warning", warning)
	}
}

func writeGuideField(b *strings.Builder, key, value string) {
	if strings.TrimSpace(value) == "" {
		value = "unknown"
	}
	b.WriteString("// ")
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(value)
	b.WriteByte('\n')
}

func writeGuideCommentBlock(b *strings.Builder, title, text string) {
	b.WriteString("// ")
	b.WriteString(title)
	b.WriteByte('\n')
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		b.WriteString("// ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
}

func writeGuideList(b *strings.Builder, key string, values []string) {
	b.WriteString("// ")
	b.WriteString(key)
	b.WriteString(": ")
	if len(values) == 0 {
		b.WriteString("[]\n")
		return
	}
	b.WriteString("[")
	for i, value := range values {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(value)
	}
	b.WriteString("]\n")
}

func endpointURL(addr, path string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "unknown"
	}
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/") + path
	}
	return "http://" + strings.TrimRight(addr, "/") + path
}

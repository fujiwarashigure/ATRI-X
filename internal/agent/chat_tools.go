package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/fujiwarashigure/ATRI-X/internal/config"
	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/logging"
	"github.com/fujiwarashigure/ATRI-X/internal/storage"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
	"github.com/fujiwarashigure/ATRI-X/internal/toolrun"
)

type pendingUserMessage struct {
	message      storage.Message
	messageIndex int
	platformText string
}

func (a *Agent) drainPendingUserInput(ctx context.Context, sessionID string, messages []llm.LLMMessage) ([]llm.LLMMessage, *pendingUserMessage) {
	pending := a.turns.DrainMergedInput(sessionID)
	if pending.Text == "" && len(pending.Segments) == 0 {
		return messages, nil
	}
	segments := append([]llm.MessageSegment(nil), pending.Segments...)
	if len(segments) == 0 {
		segments = llm.TextSegments(pending.Text)
	}
	segments = a.attachSpeakerMarker(ctx, pending, segments)
	message := storage.Message{
		ID:        storage.NewID(),
		SessionID: sessionID,
		Role:      storage.RoleUser,
		Content:   llm.SegmentsContentText(segments),
		Segments:  storedMessageSegments(segments),
		Metadata:  inputSpeakerMetadata(pending),
	}
	binding := &pendingUserMessage{message: message, messageIndex: len(messages), platformText: pending.PlatformText}
	return append(messages, llm.LLMMessage{Role: llm.RoleUser, Segments: segments}), binding
}

func (a *Agent) executeToolCalls(ctx context.Context, session *storage.Session, calls []llm.ToolCallRequest, assistantText, assistantRawText string, out turnOutput) toolrun.RunResult {
	if !sessionToolsEnabled(session) {
		return toolrun.RunResult{}
	}
	return a.toolRunManager().Run(ctx, agentToolRunDeps{agent: a, output: out}, toolrun.RunRequest{
		Session:          session,
		Calls:            calls,
		AssistantText:    assistantText,
		AssistantRawText: assistantRawText,
		CachedTools:      a.cachedToolsForSession(session),
		Actor:            a.actor(ctx),
		AuthorizeTool:    a.authorizeToolCall,
		BeforeExecute:    a.beforeExecuteToolCall,
	})
}

func riskReasonsText(reasons []string) string {
	if len(reasons) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n风险原因：")
	for _, reason := range reasons {
		reason = strings.TrimSpace(reason)
		if reason != "" {
			sb.WriteString("\n- ")
			sb.WriteString(reason)
		}
	}
	return sb.String()
}

func (a *Agent) isSessionAutoConfirmed(sessionID string) bool {
	a.autoConfirmMu.Lock()
	defer a.autoConfirmMu.Unlock()
	return a.autoConfirmSession[sessionID]
}

func (a *Agent) setSessionAutoConfirmed(sessionID string) {
	a.autoConfirmMu.Lock()
	defer a.autoConfirmMu.Unlock()
	a.autoConfirmSession[sessionID] = true
}

func (a *Agent) isToolAutoConfirmed(sessionID, toolName string) bool {
	a.autoConfirmMu.Lock()
	defer a.autoConfirmMu.Unlock()
	return a.autoConfirmTools[sessionID] != nil && a.autoConfirmTools[sessionID][toolName]
}

func (a *Agent) setToolAutoConfirmed(sessionID, toolName string) {
	a.autoConfirmMu.Lock()
	defer a.autoConfirmMu.Unlock()
	if a.autoConfirmTools[sessionID] == nil {
		a.autoConfirmTools[sessionID] = map[string]bool{}
	}
	a.autoConfirmTools[sessionID][toolName] = true
}

func (a *Agent) maxToolRoundsPerTurn() int {
	if a.toolRuntime.config.MaxRoundsPerTurn <= 0 {
		return 2
	}
	return a.toolRuntime.config.MaxRoundsPerTurn
}

func skippedToolMessages(calls []llm.ToolCallRequest, maxRounds int) []llm.LLMMessage {
	messages := make([]llm.LLMMessage, 0, len(calls))
	for _, call := range calls {
		messages = append(messages, llm.LLMMessage{
			Role:       llm.RoleTool,
			Name:       call.Name,
			ToolCallID: call.ID,
			Segments:   llm.TextSegments(fmt.Sprintf("tool call skipped: max_rounds_per_turn=%d reached. Please summarize current progress without calling more tools.", maxRounds)),
		})
	}
	return messages
}

func (a *Agent) toolCallRisk(ctx context.Context, call llm.ToolCallRequest) string {
	if a.toolRuntime.registry == nil {
		return "unknown"
	}
	t, ok := a.toolRuntime.registry.Get(call.Name)
	if !ok {
		return "unknown"
	}
	assessment, err := tool.AssessRisk(ctx, t, tool.CallRequest{ID: call.ID, Name: call.Name, Arguments: json.RawMessage(call.Arguments)})
	if err != nil {
		return "unknown"
	}
	return string(assessment.Level)
}

func (a *Agent) recordToolCall(ctx context.Context, sessionID string, call llm.ToolCallRequest, risk string, startedAt time.Time, result string, callErr error) {
	resultPreview := ""
	argumentsPreview := ""
	if a.historyEnabled(ctx) {
		resultPreview = previewRunes(result, toolLogResultLimit(call.Name))
		argumentsPreview = previewArguments(call.Arguments)
	}
	runSource := toolRunSource(ctx)
	record := &storage.ToolCallRecord{
		SessionID:     sessionID,
		ToolCallID:    call.ID,
		ToolName:      call.Name,
		ActorID:       a.actor(ctx).ID,
		RiskLevel:     risk,
		Success:       callErr == nil,
		ResultPreview: resultPreview,
		StartedAt:     startedAt,
		FinishedAt:    storage.Now(),
	}
	if callErr != nil {
		record.Error = callErr.Error()
	}
	if a.store != nil && a.store.ToolCalls() != nil {
		if err := a.store.ToolCalls().Create(ctx, record); err != nil && a.logger != nil {
			a.logger.Warn("record tool call failed", "session_id", sessionID, "tool", call.Name, "error", err)
		}
	}
	if a.logger != nil {
		a.logger.Info("tool call",
			"event", "tool_call",
			"session_id", sessionID,
			"arguments", argumentsPreview,
			"result", resultPreview,
			"tool", call.Name,
			logging.FieldRunSource, runSource,
			"tool_call_id", call.ID,
			"actor_id", record.ActorID,
			"risk", risk,
			"success", record.Success,
			"elapsed_ms", record.FinishedAt.Sub(record.StartedAt).Milliseconds(),
			"error", record.Error,
		)
	}
	a.audit("tool_call",
		"session_id", sessionID,
		"arguments", argumentsPreview,
		"tool", call.Name,
		logging.FieldRunSource, runSource,
		"tool_call_id", call.ID,
		"actor_id", record.ActorID,
		"risk", risk,
		"success", record.Success,
		"elapsed_ms", record.FinishedAt.Sub(record.StartedAt).Milliseconds(),
		"error", record.Error,
		"result", resultFromSuccess(record.Success),
	)
}

// toolRunSource 固定这次工具调用的运行来源。后台运行（Cron / Atrinis）在进入模型前就把
// SandboxContext 的 BackgroundKind 写进 ctx，前台调用没有该标记。取值只看调用现场：
// 会话被前台接管后 background_kind 会被清掉，用会话元数据回推会把历史算成前台。
func toolRunSource(ctx context.Context) string {
	sandbox, ok := tool.SandboxContextFromContext(ctx)
	if !ok || (!sandbox.Background && strings.TrimSpace(string(sandbox.BackgroundKind)) == "") {
		return logging.RunSourceForeground
	}
	switch sandbox.BackgroundKind {
	case tool.BackgroundKindCron:
		return logging.RunSourceCron
	case tool.BackgroundKindAtrinis:
		return logging.RunSourceAtrinis
	default:
		// 后台运行但没有可辨识来源：宁可不写，也不要假装是前台。
		return ""
	}
}

// resultFromSuccess 把工具执行的布尔结果映射成契约里的操作结果。工具"执行了但失败"
// 是 failed；被策略拒绝走的是 permission_denied（rejected），不经过这里。
func resultFromSuccess(success bool) string {
	if success {
		return logging.ResultSucceeded
	}
	return logging.ResultFailed
}

func (a *Agent) logRiskConfirmationWait(sessionID string, call llm.ToolCallRequest, risk tool.RiskLevel, reasons []string) {
	auditAttrs := []any{"session_id", sessionID, "tool", call.Name, "risk", risk, "arguments", previewArguments(call.Arguments)}
	if len(reasons) > 0 {
		auditAttrs = append(auditAttrs, "risk_reasons", strings.Join(reasons, "; "))
	}
	auditAttrs = append(auditAttrs, "result", logging.ResultSucceeded)
	a.audit("risk_confirmation_wait", auditAttrs...)
}

func (a *Agent) logRiskConfirmationResult(sessionID string, call llm.ToolCallRequest, risk tool.RiskLevel, action, extra, reason string) {
	a.audit("risk_confirmation_result", "session_id", sessionID, "tool", call.Name, "risk", risk, "action", action, "extra", extra, "reason", reason, "result", riskConfirmationResult(action))
}

// riskConfirmationResult 把用户的确认动作映射成操作结果。动作取值来自
// toolrun_adapter：expire（确认过期）、stop（用户停止）、reject（用户拒绝）、
// confirm / confirmtool / confirmall（放行）。拒绝是 rejected；过期与停止是 canceled
// （不是故障，也不是策略拒绝）；放行记 succeeded。
func riskConfirmationResult(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "reject", "rejected":
		return logging.ResultRejected
	case "expire", "expired", "stop", "stopped":
		return logging.ResultCanceled
	default:
		return logging.ResultSucceeded
	}
}

func previewLogText(text string) string {
	return previewRunes(text, previewTextRunes)
}

// 工具日志正文的预算按工具类别取值，对齐上游 internal/tool 的分类：读取／查询／说明类
// 用短正文，执行／编辑／写入／媒体类留长一点；参数正文统一按上游 Runner 的口径取 256。
const (
	toolLogQueryLimit    = 128
	toolLogActionLimit   = 256
	toolLogArgumentLimit = 256
	// previewTextRunes 是用户输入、助手正文这类非工具正文的预览长度。
	previewTextRunes = 120
)

// toolLogQueryTools 是"读取／查询／说明"类工具。它们的结果正文是摘要性质，留在 128
// 字符内既够定位又不会把整篇文档抄进日志；其余工具（执行、编辑、写入、媒体、后台任务）
// 按 256 处理。新增读取类工具时在这里登记，未登记的工具默认走执行类。
var toolLogQueryTools = map[string]bool{
	"read_file":             true,
	"web_search":            true,
	"web_extract":           true,
	"discover_tool":         true,
	"workspace":             true,
	"resident_memory_read":  true,
	"long_memory_search":    true,
	"atrix_skill":           true,
	"read_atrix_skill":      true,
	"agent_skill":           true,
	"character_list":        true,
	"character_read":        true,
	"character_search":      true,
	"prompt_library_search": true,
	"angel_recall":          true,
}

func toolLogResultLimit(toolName string) int {
	if toolLogQueryTools[strings.TrimSpace(toolName)] {
		return toolLogQueryLimit
	}
	return toolLogActionLimit
}

func previewRunes(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxRunes <= 0 {
		return ""
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

func joinAssistantText(first, second string) string {
	first = strings.TrimSpace(first)
	second = strings.TrimSpace(second)
	switch {
	case first == "":
		return second
	case second == "":
		return first
	default:
		return first + "\n\n" + second
	}
}

func joinToolNames(calls []llm.ToolCallRequest) string {
	names := make([]string, 0, len(calls))
	for _, call := range calls {
		if call.Name != "" {
			names = append(names, call.Name)
		}
	}
	return strings.Join(names, ", ")
}

func compactArguments(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return "{}"
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(args)); err == nil {
		return compact.String()
	}
	return args
}

func previewArguments(args string) string {
	return previewRunes(compactArguments(args), toolLogArgumentLimit)
}

// sessionToolsEnabled reports whether one session may use tools at all. Only
// work sessions do; chat sessions must ignore tool schemas and tool calls even
// when a hook injects them.
func sessionToolsEnabled(session *storage.Session) bool {
	return session != nil && session.Mode == storage.SessionModeWork
}

// withToolCapabilities annotates the request context with what the model that
// serves this session can accept. Only an explicit vision = false declaration
// hides image tools; VisionUnknown keeps the historic behaviour.
func (a *Agent) withToolCapabilities(ctx context.Context, session *storage.Session) context.Context {
	if session == nil {
		return ctx
	}
	selected := a.modelForMode(session.Mode)
	return a.withModelCapabilities(ctx, selected.Provider, selected.Model)
}

// withModelCapabilities is withToolCapabilities for one explicit provider/model.
func (a *Agent) withModelCapabilities(ctx context.Context, providerName, model string) context.Context {
	provider, ok := a.modelRuntime.providers[providerName]
	if !ok {
		return ctx
	}
	vision := provider.VisionSupport(model) != config.VisionUnsupported
	return tool.WithCapabilities(ctx, tool.Capabilities{Vision: vision})
}

func (a *Agent) toolsForSession(ctx context.Context, session *storage.Session) ([]llm.ToolSchema, error) {
	if !sessionToolsEnabled(session) {
		return nil, nil
	}
	var schemas []llm.ToolSchema
	var err error
	if a.toolRuntime.provider != nil && !a.toolRuntime.defaultProvider {
		schemas, err = a.toolRuntime.provider.Schemas(ctx, session.Mode, session, a.scope(ctx))
	} else {
		schemas, err = a.toolRunManager().Schemas(ctx, toolrun.Context{Mode: session.Mode, Session: session, Scope: a.scope(ctx), Actor: a.actor(ctx), DisableBaseTools: isBackgroundSession(session)}, a.cachedToolsForSession(session))
	}
	if err != nil {
		return nil, err
	}
	if name := turnToolProfile(ctx); name != "" {
		schemas = mergeToolSchemas(schemas, a.turnToolSchemas(ctx, name))
	}
	if allowlist := a.groupToolAllowlist(ctx); allowlist != nil {
		schemas = filterToolSchemasByAllowlist(schemas, allowlist)
	}
	schemas = a.filterGroupFeatureSchemas(ctx, schemas)
	return schemas, nil
}

func filterToolSchemasByAllowlist(schemas []llm.ToolSchema, allowlist map[string]bool) []llm.ToolSchema {
	if allowlist == nil {
		return schemas
	}
	out := make([]llm.ToolSchema, 0, len(schemas))
	for _, schema := range schemas {
		name := strings.ToLower(strings.TrimSpace(schema.Function.Name))
		if allowlist[name] {
			out = append(out, schema)
		}
	}
	return out
}

func mergeToolSchemas(base, extra []llm.ToolSchema) []llm.ToolSchema {
	if len(extra) == 0 {
		return base
	}
	seen := make(map[string]bool, len(base)+len(extra))
	out := make([]llm.ToolSchema, 0, len(base)+len(extra))
	for _, schema := range base {
		if name := schema.Function.Name; name != "" && !seen[name] {
			seen[name] = true
			out = append(out, schema)
		}
	}
	for _, schema := range extra {
		if name := schema.Function.Name; name != "" && !seen[name] {
			seen[name] = true
			out = append(out, schema)
		}
	}
	return out
}

func isBackgroundSession(session *storage.Session) bool {
	if session == nil {
		return false
	}
	return strings.TrimSpace(decodeSessionMetadata(session.Metadata).BackgroundKind) != ""
}

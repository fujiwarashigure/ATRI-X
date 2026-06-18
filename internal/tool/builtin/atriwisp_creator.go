package builtin

import (
	"context"

	"github.com/fujiwarashigure/ATRI-X/internal/llm"
	"github.com/fujiwarashigure/ATRI-X/internal/tool"
)

const AtriwispCreatorName = "atriwisp_creator"

type AtriwispCreatorTool struct{}

func NewAtriwispCreatorTool() AtriwispCreatorTool {
	return AtriwispCreatorTool{}
}

func (AtriwispCreatorTool) Name() string {
	return AtriwispCreatorName
}

func (AtriwispCreatorTool) Info() tool.Info {
	return tool.NewBuilder(AtriwispCreatorName).
		Description("获取创建 Atriwisp 监听器所需的 Atrinis/Atriena/Atriyph 说明、配置片段、事件模板、代码脚手架和安全检查清单。").
		Source(tool.SourceBuiltin).
		Risk(tool.RiskLow).
		SuperadminOnly().
		DependsOn("read_file", "edit_file", "shell").
		BuildInfo()
}

func (AtriwispCreatorTool) Schema() llm.ToolSchema {
	return tool.NewBuilder(AtriwispCreatorName).
		Description("无参数。调用后返回创建 Atriwisp 监听器的完整指南，包括 Atriena 协议、Atriyph 任务写法、Atrinis 配置、事件模板、代码脚手架和安全检查清单。").
		BuildSchema()
}

func (AtriwispCreatorTool) Call(ctx context.Context, req tool.CallRequest) (*tool.Result, error) {
	_ = req
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &tool.Result{Content: atriwispCreatorGuide}, nil
}

const atriwispCreatorGuide = `# Atriwisp Creator Guide

Use this guide to create or review an Atriwisp listener for ATRI-X Atrinis. This tool only returns instructions. To inspect existing files, use read_file. To create or modify files, use edit_file. To run checks, curl, or local scripts, use shell and follow the normal risk confirmation flow.

## Core concepts

Atrinis is ATRI-X's internal listening hub. It receives Atriena events, authenticates tokens, validates protocol fields, deduplicates events, records logs/audit entries, resolves delivery targets, and dispatches events as record, direct, or llm.

Atriwisp is an external listener. It can be a script, daemon, webhook bridge, RSS watcher, game plugin, log watcher, server monitor, or local device watcher. It observes external world events and sends Atriena JSON to Atrinis.

Atriena is the JSON over HTTP event protocol used by Atriwisp to report events to Atrinis.

Atriyph is ATRI-X's lightweight task notation. In Atrinis llm mode, use Atriyph content when the event needs structured analysis, constraints, and report decisions.

## Safety boundaries

- Atrinis makes the final delivery decision. Atriwisp targets are requests, not authority.
- Do not hardcode tokens in committed code. Read tokens from environment variables or local secret files.
- Do not log token values.
- Use stable event ids. Atrinis deduplicates by atriwisp.name + source + id.
- Prefer local binding such as 127.0.0.1 for Atrinis and Atriwisp tool endpoints unless the user explicitly needs remote access.
- Real file writes and shell commands must use edit_file and shell and obey ATRI-X risk confirmation.

## Creation workflow

1. Clarify the event source: server alert, RSS, webhook, log, script output, game event, device state, or another external system.
2. Choose mode:
   - record: only persist/log the event.
   - direct: send a simple text notification after Atrinis target resolution.
   - llm: run a background LLM session for analysis, judgment, summarization, or tool use.
3. Choose identity fields:
   - atriwisp.name: stable listener name, for example server-watchdog.
   - source: concrete source under that Atriwisp, for example minecraft-main.
   - id: stable source-local event id. Avoid reusing ids for changed content.
4. Decide target request:
   - platforms: normally ["cli"] or ["all"].
   - superadmins: normally true.
5. Decide whether the background task needs ATRI-X internal tools or Skills with tool_list_names.
6. Decide whether the Atriwisp needs to declare external tools in tools.
7. Inspect existing config with read_file before editing app.toml or atrinis.toml.
8. Use edit_file to create listener code, config snippets, and tests.
9. Use shell for syntax checks, local test servers, or curl tests.
10. Return a concise final summary with file paths, commands, and security notes.

## Atriena v1 HTTP endpoint

Endpoint:

    POST http://<atrinis-addr>/atriena/v1/events
    GET  http://<atrinis-addr>/healthz

Authentication headers:

    Authorization: Bearer <token>

or:

    X-Atrinis-Token: <token>

The token name is used for logs and audit only. It is not the Atriwisp identity. Atriwisp identity comes from atriwisp.name in the JSON body.

## Atriena v1 request fields

Required fields:

- version: must be "atriena.v1".
- atriwisp.name: stable Atriwisp name.
- source: concrete event source.
- id: source-local event id.
- mode: "record", "direct", or "llm".
- content: event body.

Common optional fields:

- atriwisp.tags: string tags for logs/statistics.
- created_at: external event time. If omitted, Atrinis uses receive time.
- title: notification/session title.
- format: "text" or "atriyph". Default is text.
- model_slot: llm mode slot, one of atriwisp1, atriwisp2, atriwisp3. Falls back to work when unset or unconfigured.
- tool_list_names: ATRI-X internal tool names or Skill names requested for background llm mode. Tools inject schemas; Skills inject task instructions and activate their runner. Atrinis allowed_tools still decides final availability. discover_tool is ignored in background.
- tools: external tool declarations supplied by Atriwisp for this event.
- targets: desired delivery target. Atrinis still resolves final target.
- meta: original extra metadata; useful for host, severity, URL, path, commit id, etc.

Basic direct event payload:

    {
      "version": "atriena.v1",
      "atriwisp": {"name": "server-watchdog", "tags": ["server"]},
      "source": "minecraft-main",
      "id": "cpu-alert-001",
      "mode": "direct",
      "title": "服务器 CPU 异常",
      "content": "minecraft-main CPU 使用率超过阈值。",
      "targets": {"platforms": ["cli"], "superadmins": true}
    }

Basic llm event payload:

    {
      "version": "atriena.v1",
      "atriwisp": {"name": "server-watchdog", "tags": ["server", "prod"]},
      "source": "minecraft-main",
      "id": "cpu-alert-001",
      "mode": "llm",
      "title": "服务器 CPU 异常",
      "format": "atriyph",
      "content": "#task investigate_cpu_alert - 检查服务器 CPU 异常并判断是否需要通知\n<- $event:object!\n-> $report:str\n** 根据事件内容和可用工具判断严重程度\n~ 编造未观察到的日志或指标\n> 分析事件并在需要时生成管理员报告",
      "model_slot": "atriwisp1",
      "tool_list_names": ["web_search"],
      "targets": {"platforms": ["cli"], "superadmins": true},
      "meta": {"severity": "warning", "host": "mc-main-01"}
    }

HTTP response only means Atrinis accepted or rejected the event. It does not wait for LLM completion.

## Mode selection

Use record when:

- The user only wants durable event records.
- The signal is low priority or used for testing.
- No platform notification should be sent.

Use direct when:

- The external system already produced a human-readable message.
- No LLM analysis is needed.
- The event should become a simple notification after Atrinis delivery policy.

Use llm when:

- The event needs judgment, summarization, filtering, or severity classification.
- The event may need tools.
- The Atriwisp should not decide whether to notify.
- The user wants a structured report.

## Atriyph mini guide for llm mode

Use format="atriyph" and start content with #task.

Useful tokens:

- #task <name> - <description>: task header.
- <- $name:type!: required input.
- -> $name:type: output.
- ** constraint: rule to follow.
- ~ forbidden behavior: what must not happen.
- > text: output instruction.
- ?if(condition) { ... } and ?else { ... }: conditional flow.
- @tool tool_name(key=value): planned tool call in task text.

Minimal llm task:

    #task review_event - 判断事件是否需要通知
    <- $event:object!
    -> $report:str

    ** 基于事件内容、meta 和工具结果判断
    ** 报告必须简短明确
    ~ 编造不存在的日志、指标或结论
    > 分析事件。如果需要通知，给出原因和建议；否则说明无需打扰。

Do not write a huge Atriyph program. Keep it short and let the LLM reason with event metadata and tools.

## Internal ATRI-X tools

tool_list_names requests built-in ATRI-X tools for llm mode. Atrinis filters this list with allowed_tools from atrinis.toml. Typical values:

- web_search, web_extract: search or extract web pages.
- shell: only when Atrinis policy allows it and sandbox constraints are acceptable.
- read_file/edit_file: only when explicitly allowed and appropriate.

Do not include discover_tool. Background Atrinis tasks do not use the default discover entry.

## Atriwisp external tools

An Atriwisp may declare event-local tools in the tools array. Use this when Atrinis needs to ask the Atriwisp side for extra data, such as service status, recent logs, or RSS item details.

Declaration example:

    {
      "name": "server_status",
      "description": "查询 minecraft-main 当前服务状态和最近错误摘要",
      "schema": {
        "type": "object",
        "properties": {
          "detail": {"type": "boolean"}
        }
      },
      "timeout_seconds": 10,
      "endpoint": "http://127.0.0.1:32171/tools/server_status"
    }

Atrinis calls the endpoint with JSON:

    {
      "tool": "server_status",
      "event_key": "server-watchdog/minecraft-main/cpu-alert-001",
      "arguments": {"detail": true}
    }

Endpoint response can be plain text or JSON. JSON may use one of:

    {"content": "..."}
    {"text": "..."}
    {"result": "..."}
    {"error": "..."}

External tool risk is owned by the Atriwisp environment. Atrinis treats it as an external low-risk HTTP call, but Atrinis policy may disable named external tools.

## atrinis.toml snippet

Default disabled config:

    enabled = false
    allowed_tools = ["web_search", "web_extract"]

    [http]
    addr = "127.0.0.1:32170"
    max_body_bytes = 1048576
    queue_size = 128
    workers = 2

    [tokens.home]
    token_env = ["ATRINIS_HOME_TOKEN"]

    [delivery]
    default_platforms = ["cli"]
    allow_superadmins = true

    [atriwisps.server-watchdog]
    allowed_tokens = ["home"]
    allowed_tools = ["shell", "web_search"]
    disabled_external_tools = ["danger_tool"]

    [atriwisps.server-watchdog.delivery]
    default_platforms = ["cli"]
    allow_superadmins = true

Enable Atrinis by changing enabled to true and setting the token environment variable or .env entry.

## Python polling Atriwisp template

Use this when the listener periodically checks a source and sends an event when state changes.

    import json
    import os
    import time
    import urllib.request

    ATRINIS_URL = os.environ.get("ATRINIS_URL", "http://127.0.0.1:32170/atriena/v1/events")
    ATRINIS_TOKEN = os.environ["ATRINIS_HOME_TOKEN"]

    def post_event(payload):
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        req = urllib.request.Request(
            ATRINIS_URL,
            data=data,
            headers={
                "Content-Type": "application/json",
                "Authorization": "Bearer " + ATRINIS_TOKEN,
            },
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            print(resp.status, resp.read().decode("utf-8"))

    def read_state():
        # TODO: replace with real observation logic.
        return {"status": "ok", "value": 1}

    def main():
        last = None
        while True:
            state = read_state()
            event_id = "state-" + str(state.get("value"))
            if state != last:
                post_event({
                    "version": "atriena.v1",
                    "atriwisp": {"name": "example-poller", "tags": ["polling"]},
                    "source": "example-source",
                    "id": event_id,
                    "mode": "direct",
                    "title": "Example state changed",
                    "content": "Observed state: " + json.dumps(state, ensure_ascii=False),
                    "targets": {"platforms": ["cli"], "superadmins": True},
                    "meta": state,
                })
                last = state
            time.sleep(60)

    if __name__ == "__main__":
        main()

## Python webhook Atriwisp template

Use this when another system calls the Atriwisp, and the Atriwisp forwards normalized Atriena events to Atrinis.

    import json
    import os
    import urllib.request
    from http.server import BaseHTTPRequestHandler, HTTPServer

    ATRINIS_URL = os.environ.get("ATRINIS_URL", "http://127.0.0.1:32170/atriena/v1/events")
    ATRINIS_TOKEN = os.environ["ATRINIS_HOME_TOKEN"]

    def post_event(payload):
        data = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        req = urllib.request.Request(
            ATRINIS_URL,
            data=data,
            headers={"Content-Type": "application/json", "Authorization": "Bearer " + ATRINIS_TOKEN},
            method="POST",
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, resp.read().decode("utf-8")

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            length = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(length)
            incoming = json.loads(body.decode("utf-8"))
            event_id = str(incoming.get("id") or incoming.get("timestamp") or hash(body))
            payload = {
                "version": "atriena.v1",
                "atriwisp": {"name": "example-webhook", "tags": ["webhook"]},
                "source": "external-webhook",
                "id": event_id,
                "mode": "llm",
                "title": incoming.get("title", "Webhook event"),
                "format": "text",
                "content": json.dumps(incoming, ensure_ascii=False),
                "targets": {"platforms": ["cli"], "superadmins": True},
                "meta": incoming,
            }
            status, text = post_event(payload)
            self.send_response(200)
            self.end_headers()
            self.wfile.write((str(status) + " " + text).encode("utf-8"))

    if __name__ == "__main__":
        HTTPServer(("127.0.0.1", 32172), Handler).serve_forever()

## External tool endpoint template

Use this only when llm mode needs to call back to Atriwisp for more data.

    import json
    from http.server import BaseHTTPRequestHandler, HTTPServer

    class Handler(BaseHTTPRequestHandler):
        def do_POST(self):
            length = int(self.headers.get("Content-Length", "0"))
            req = json.loads(self.rfile.read(length).decode("utf-8"))
            tool = req.get("tool")
            args = req.get("arguments") or {}
            if tool == "server_status":
                result = {"content": "server is running; detail=" + str(args.get("detail"))}
            else:
                result = {"error": "unknown tool: " + str(tool)}
            data = json.dumps(result, ensure_ascii=False).encode("utf-8")
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.end_headers()
            self.wfile.write(data)

    if __name__ == "__main__":
        HTTPServer(("127.0.0.1", 32171), Handler).serve_forever()

## curl tests

Health check:

    curl -sS http://127.0.0.1:32170/healthz

Direct event:

    curl -sS http://127.0.0.1:32170/atriena/v1/events -H "Content-Type: application/json" -H "Authorization: Bearer $ATRINIS_HOME_TOKEN" -d "{\"version\":\"atriena.v1\",\"atriwisp\":{\"name\":\"test-atriwisp\"},\"source\":\"manual\",\"id\":\"test-001\",\"mode\":\"direct\",\"title\":\"Atriwisp test\",\"content\":\"Hello from Atriwisp\",\"targets\":{\"platforms\":[\"cli\"],\"superadmins\":true}}"

If a request returns duplicate, the same atriwisp.name + source + id was already accepted. Change id for a new test event.

## Final answer format when creating an Atriwisp

Return these sections:

1. Design summary: source, mode, event identity, target, tools.
2. Files created or changed.
3. Atrinis config snippet and required environment variables.
4. Atriwisp source code.
5. Test commands.
6. Run instructions.
7. Security checklist.

## Review checklist

- Atrinis enabled only when the user wants it.
- Atrinis token comes from env or .env, not committed source.
- Atriwisp event id is stable and deduplicates correctly.
- Atriwisp does not directly send platform messages.
- direct mode events contain human-readable content.
- llm mode content is either clear text or concise Atriyph #task.
- tool_list_names only asks for tools allowed by atrinis.toml.
- External tool endpoints bind locally by default.
- Generated code has timeouts for HTTP calls.
- Test curl uses the correct Atrinis address and token.
`

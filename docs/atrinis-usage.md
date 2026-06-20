# Atrinis 配置与使用

本文承接 [Atrinis 监听枢纽](atrinis.md)，讲如何启用 Atrinis、配置 Atriwisp 策略，并用 Atriena 投递事件。

## 快速开始

Atrinis 默认关闭。启用时先在 `app.toml` 中声明配置文件入口：

```toml
[config_files]
atrinis = "atrinis.toml"
```

然后在 `atrinis.toml` 中配置：

```toml
enabled = true # Atrinis 总开关；false 时不启动 HTTP runtime。
allowed_tools = ["web_search", "web_extract"] # 默认允许 Atriwisp 预加载的 ATRI-X 内部工具。

[http]
addr = "127.0.0.1:32170" # 建议先绑定本地地址，需要暴露时再用反代或内网转发。
max_body_bytes = 1048576 # 单个 Atriena 请求 body 上限。
queue_size = 128 # llm 模式后台队列长度。
workers = 2 # llm 模式后台 worker 数。

[tokens.home]
token_env = ["ATRINIS_HOME_TOKEN"] # 从系统环境变量或配置目录 .env 读取。

[delivery]
default_platforms = ["cli"] # Atrinis 策略允许的默认投递平台。
allow_superadmins = true # 是否允许投递给目标平台超级管理员。

# Atriwisp 默认启用；只有需要限制 token、覆盖投递策略或禁用时才配置。
[atriwisps.server-watchdog]
allowed_tokens = ["home"]
allowed_tools = ["shell", "web_search"] # 存在时覆盖顶层 allowed_tools。
disabled_external_tools = ["danger_tool"] # 外部工具默认允许，此处只禁用指定工具。

[atriwisps.server-watchdog.delivery]
default_platforms = ["cli"]
allow_superadmins = true

[atriwisps.spike-checker]
enabled = false # 只有显式 enabled=false 才禁用该 Atriwisp。
```

`.env` 示例：

```dotenv
ATRINIS_HOME_TOKEN=change-me
```

配置说明：

- `enabled=false` 时不启动 Atrinis HTTP runtime。
- `atri-x cli` 是 CLI-only 模式，即使 `enabled=true` 也不会启动 Atrinis HTTP runtime；需要接收 Atriena 事件时请使用 `atri-x run` 或 `atri-x service run`。
- 当前 `targets.platforms=["cli"]` 只适用于 Atrinis 与 CLI 同处一个 `atri-x run` 前台进程的场景；`atri-x service run` 暂不能把消息投递到另一个独立 `atri-x cli` 进程。
- token 原文不写入配置，推荐放在系统环境变量或配置目录 `.env`。
- `token_env` 可以写多个环境变量名，按顺序尝试。
- Atriwisp 默认启用；没写 `[atriwisps.<name>]`、写了但没写 `enabled`、或写了 `enabled=true` 都会接收。
- 只有显式 `enabled=false` 才会禁用对应 Atriwisp。
- `allowed_tokens` 限制哪些 token 可以代表该 Atriwisp 投递事件；不写则允许任意已认证 token。
- 顶层 `allowed_tools` 是 ATRI-X 内部工具默认白名单，单个 Atriwisp 的 `allowed_tools` 存在时覆盖它。
- 外部工具默认允许；单个 Atriwisp 可以用 `disabled_external_tools` 禁用指定外部工具。
- `default_platforms` 是 Atrinis 策略允许的默认投递平台。
- `allow_superadmins=true` 表示允许投递给目标平台超级管理员。

如果想让 ATRI-X 帮你生成 Atriwisp 监听器，可以在 work 模式下向超级管理员会话提出需求，例如：

```text
@tool:atriwisp_creator 帮我创建一个监听 RSS 更新并通过 Atrinis 投递摘要的 Atriwisp
```

`atriwisp_creator` 会提供协议说明、配置片段、事件模板、代码脚手架和测试命令；真正写文件或运行命令仍会继续使用对应工具。

启动 ATRI-X 后，可以用 curl 测试一个 `direct` 事件：

```bash
curl -sS http://127.0.0.1:32170/atriena/v1/events \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer change-me' \
  -d '{
    "version":"atriena.v1",
    "atriwisp":{"name":"server-watchdog"},
    "source":"minecraft-main",
    "id":"cpu-alert-001",
    "mode":"direct",
    "title":"服务器 CPU 异常",
    "content":"minecraft-main CPU 使用率超过阈值。",
    "targets":{"platforms":["cli"],"superadmins":true}
  }'
```

如果同一个 `atriwisp.name + source + id` 再次发送，Atrinis 会返回 duplicate，不重复分发。

## Atriena 请求示例

```json
{
  "version": "atriena.v1",
  "atriwisp": {
    "name": "server-watchdog",
    "tags": ["server", "prod"]
  },
  "source": "minecraft-main",
  "id": "cpu-alert-001",
  "mode": "llm",
  "title": "服务器 CPU 异常",
  "format": "atriyph",
  "content": "#task investigate_cpu_alert - 检查服务器 CPU 异常并判断是否需要通知",
  "model_slot": "atriwisp2",
  "tool_list_names": ["shell"],
  "tools": [
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
  ],
  "targets": {
    "platforms": ["cli"],
    "superadmins": true
  },
  "meta": {
    "severity": "warning",
    "host": "mc-main-01"
  }
}
```

## Segments（多模态消息段）

Atriena v1 支持通过 `segments` 字段发送图片和文件。`content` 保留为纯文本 fallback，与旧 Atriwisp 完全兼容。

`segments` 为空时行为不变，非空时优先 segments 渲染，content 作为附加文本。

### Segment 字段

| 字段 | 类型 | 必填 | 说明 |
| --- | ---: | :---: | --- |
| `kind` | string | 是 | `text`、`image`、`file`。 |
| `text` | string | text 必填 | 纯文本内容，不落盘。 |
| `url` | string | image/file 必填 | `http://`、`https://` 或 `data:` base64 URI。 |
| `name` | string | 否 | 文件名，用于下载保存和展示。 |
| `mime_type` | string | 否 | MIME 类型提示。 |

### 下载与存储

- Atrinis 接收后自动下载到 `sandbox/atrinis/<atriwisp名>/<事件id>/`。
- 发送到 LLM 时使用原始 URL（多模态模型可直接看图），沙盒保留副本。
- direct 模式同样支持 image/file 输出，平台不支持时自动降级为文字描述。
- 文件大小受 `atrinis.toml` 的 `[segment].max_file_bytes` 限制（默认 100MB）。
- `data:` URI 仅支持 base64 编码，解码后同样受限。
- `file://` 等本地协议禁止。

### 示例

```json
{
  "version": "atriena.v1",
  "atriwisp": {"name": "monitor"},
  "source": "prod-server",
  "id": "cpu-chart-002",
  "mode": "direct",
  "title": "CPU 异常",
  "content": "CPU 飙到 90%",
  "segments": [
    {"kind": "text",  "text": "服务器 CPU 飙到 90%，详见附图。"},
    {"kind": "image", "url": "https://monitor.example.com/chart.png", "name": "cpu_chart.png"},
    {"kind": "file",  "url": "https://logs.example.com/dump.txt", "name": "cpu_dump.txt"}
  ],
  "targets": {"platforms": ["cli"], "superadmins": true}
}
```

### LLM 结果中的 report_segments

后台 LLM 处理事件后，`JSONResult` 的 `report_segments` 可附带图片/文件路径，Atrinis 会在报告发送时一并投递。`url` 必须是当前任务工作目录内的相对路径，不能使用绝对路径、`~` 或 `..`。


```json
{
  "completed": true,
  "need_report": true,
  "report": "分析完成，见截图。",
  "report_segments": [
    {"type": "image", "url": "chart.png"}
  ]

}
```


常用字段：

| 字段 | 必填 | 说明 |
| --- | ---: | --- |
| `version` | 是 | 协议版本，当前为 `atriena.v1`。 |
| `atriwisp.name` | 是 | Atriwisp 名称，也是来源身份之一；仅允许英文字母、数字、`_`、`-`，不允许点号。 |

| `atriwisp.tags` | 否 | Atriwisp 标签，用于日志和统计。 |
| `source` | 是 | 具体事件源，例如服务名、脚本名、RSS 名。 |
| `id` | 是 | source 内唯一事件 ID。 |
| `created_at` | 否 | 外部事件发生时间；缺失时使用接收时间。 |
| `mode` | 是 | `record`、`direct` 或 `llm`。 |
| `title` | 否 | 事件标题，用于通知和后台 Session 标题。 |
| `format` | 否 | `text` 或 `atriyph`，默认 `text`。 |
| `content` | 是 | 事件主体。LLM 模式推荐使用 Atriyph Task Notation（任务表示法）`#task`。 |
| `model_slot` | 否 | Atrinis LLM 模型槽位，仅支持 `atriwisp1`、`atriwisp2`、`atriwisp3`；未填写或对应槽位未配置时回退到 `work`。 |
| `tool_list_names` | 否 | 后台任务预加载的 ATRI-X 内部工具名或 Skill 名；普通工具注入 schema，Skill 注入任务说明并自动注入对应 runner；必须在 Atrinis `allowed_tools` 裁决范围内，`discover_tool` 会被忽略。 |
| `tools` | 否 | Atriwisp 随事件声明的外部工具；默认允许，命中该 Atriwisp 的 `disabled_external_tools` 时拒绝。 |
| `targets` | 否 | Atriwisp 期望投递目标，最终仍由 Atrinis 裁决。 |
| `meta` | 否 | 原始补充数据，只做记录和 prompt 附加。 |


> **注：**：Atriena 基于 JSON，内容必须使用 UTF-8 编码。

HTTP 响应只表示 Atrinis 已接收或拒绝请求，不等待 LLM 完成。


```json
{
  "accepted": true,
  "duplicate": false,
  "event_key": "server-watchdog/minecraft-main/cpu-alert-001",
  "mode": "llm",
  "status": "queued"
}
```

## 投递目标与安全边界

Atriwisp 可以在 `targets` 中声明期望目标，但最终目标由 Atrinis 裁决。

当前建议只使用：

```json
{
  "targets": {
    "platforms": ["cli"],
    "superadmins": true
  }
}
```

`platforms` 可以包含 `"all"`，表示请求投递到 Atrinis 策略允许的全部平台。Atrinis 仍会结合全局配置、Atriwisp 配置和平台可用性计算最终目标。

安全约定：

- token 名只用于日志和审计，不等同于 Atriwisp 身份。
- token 原文不写日志。
- `model_slot` 只能选择 `atriwisp1`、`atriwisp2`、`atriwisp3`，不能指定任意内部模式名。
- Atriwisp 不能绕过 Tool Runtime 和 Security Policy 调用 ATRI-X 内部工具；`tool_list_names` 中的工具名或 Skill 名都会经过 Atrinis `allowed_tools` 裁决。

- Atriwisp 声明的外部 `tools` 默认允许，并通过 ToolRun 以 `atriwisp_<atriwisp>_<tool>` 形式注入为模型可调用函数名；单个 Atriwisp 可用 `disabled_external_tools` 禁用指定工具。
- 外部工具调用由 Atrinis 对声明的 endpoint 发起 HTTP JSON POST；外部工具自身负责实际风险边界，Atrinis 侧按 low 风险处理。

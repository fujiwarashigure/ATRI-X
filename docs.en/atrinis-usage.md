<!-- This file is auto-translated from docs/atrinis-usage.md. Do not edit manually. -->

# Atrinis Configuration and Usage

This article follows [Atrinis listening hub](atrinis.md) and explains how to enable Atrinis, configure Atriwisp policies, and use Atriena to deliver events.

## Quick Start

Atrinis is disabled by default. When enabling it, first declare the configuration file entry in `app.toml`:

```toml
[config_files]
atrinis = "atrinis.toml"
```

Then configure it in `atrinis.toml`:

```toml
enabled = true # Atrinis master switch; if false, the HTTP runtime will not start.
allowed_tools = ["web_search", "web_extract"] # By default, ATRI-X internal tools preloaded by Atriwisp are allowed.

[http]
addr = "127.0.0.1:32170" # It is recommended to bind to a local address first, and use a reverse proxy or intranet forwarding when exposure is needed.
max_body_bytes = 1048576 # Request body limit for a single Atriena request.
queue_size = 128 # Background queue length for LLM mode.
workers = 2 # Number of background workers in LLM mode.

[tokens.home]
token_env = ["ATRINIS_HOME_TOKEN"] # Read from system environment variables or the .env file in the configuration directory.

[delivery_disabled]
targets = [
  # { platform = "telegram" }, # Disable all deliveries for the entire telegram platform.
  # { platform = "telegram", type = "private", id = "123456789" },
  # { platform = "qqonebot", type = "group", id = "987654321" },
]

# Atriwisp is enabled by default; configure it only when you need to limit tokens, override delivery policies, or disable it.
[atriwisps.server-watchdog]
allowed_tokens = ["home"]
allowed_tools = ["shell", "web_search"] # Overrides the top-level allowed_tools when present.
disabled_external_tools = ["danger_tool"] # External tools are allowed by default; only specified tools are disabled here.
disabled_targets = [
  # { platform = "qqonebot", type = "group", id = "987654321" },
]

[atriwisps.spike-checker]
enabled = false # This Atriwisp is disabled only when enabled=false is explicitly set.
```

`.env` example:

```dotenv
ATRINIS_HOME_TOKEN=change-me
```

Configuration description:

- `enabled=false` will not start the Atrinis HTTP runtime.
- `atri-x cli` is CLI-only mode; even with `enabled=true`, the Atrinis HTTP runtime will not start; Please use `atri-x run` or `atri-x service run` when you need to receive Atriena events.
- Currently, `targets=[{"platform":"cli"}]` only applies to scenarios where Atrinis and the CLI are in the same `atri-x run` foreground process, or when the CLI remote server is enabled in service mode; Independent `atri-x cli` processes must connect to the server before they can receive notifications.
- Do not write the token plaintext into the configuration; it is recommended to place it in system environment variables or the configuration directory `.env`.
- `token_env` can contain multiple environment variable names, which will be tried in order.
- Atriwisp is enabled by default; it will receive events if `[atriwisps.<name>]` is not specified, if it is specified but `enabled` is not, or if `enabled=true` is specified.
- Only explicit `enabled=false` will disable the corresponding Atriwisp.
- `allowed_tokens` restricts which tokens can represent the Atriwisp to deliver events; if not specified, any authenticated token is allowed.
- The top-level `allowed_tools` is the default whitelist for ATRI-X internal tools; it is overridden when `allowed_tools` of an individual Atriwisp exists.
- External tools are allowed by default; an individual Atriwisp can use `disabled_external_tools` to disable specific external tools.
- Atrinis allows delivery by default; Only targets explicitly listed in `[delivery_disabled].targets` or a single Atriwisp `disabled_targets` will be prohibited.

If you want ATRI-X to help you generate an Atriwisp listener, you can submit a request to the superadmin Session in work mode, for example:

```text
@tool:atriwisp_creator 帮我创建一个监听 RSS 更新并通过 Atrinis 投递摘要的 Atriwisp
```

`atriwisp_creator` will provide protocol descriptions, configuration snippets, event templates, code scaffolding, and test commands; actually writing files or running commands will still continue to use the corresponding tools.

After starting ATRI-X, you can use curl to test a `direct` event:

```bash
curl -sS http://127.0.0.1:32170/atriena/v2/events \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer change-me' \
  -d '{
    "version":"atriena.v2",
    "atriwisp":{"name":"server-watchdog"},
    "source":"minecraft-main",
    "id":"cpu-alert-001",
    "mode":"direct",
    "title":"服务器 CPU 异常",
    "content":"minecraft-main CPU 使用率超过阈值。",
    "targets":[{"platform":"cli"}]
  }'
```

If the same `atriwisp.name + source + id` is sent again, Atrinis will return duplicate and will not redistribute it.

## Atriena Request Example

```json
{
  "version": "atriena.v2",
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
  "targets": [
    {"platform": "cli"},
    {"platform": "telegram", "type": "private", "id": "123456789"}
  ],
  "meta": {
    "severity": "warning",
    "host": "mc-main-01"
  }
}
```

## Segments (Multimodal Message Segments)

Atriena v2 supports sending images and files via the `segments` field. `content` is reserved as a plain text fallback and is fully compatible with the old Atriwisp.

Behavior remains unchanged when `segments` is empty; when not empty, segments are rendered preferentially, and content serves as additional text.

### Segment Fields

| Field | Type | Required | Description |
| --- | ---: | :---: | --- |
| `kind` | string | Yes | `text`、`image`、`file`。 |
| `text` | string | text (Required) | Plain text content, not persisted to disk. |
| `url` | string | image/file (Required) | `http://`, `https://`, or `data:` base64 URI. |
| `name` | string | No | File name, used for downloading, saving, and displaying. |
| `mime_type` | string | No | MIME type hint. |

### Download and Storage

- Atrinis automatically downloads it to `sandbox/atrinis/<atriwisp名>/<事件id>/` upon receipt.
- The original URL is used when sending to the LLM (multimodal models can view images directly), and a copy is kept in the sandbox.
- Direct mode also supports image/file output; it automatically degrades to a text description if the platform does not support it.
- File size is limited by `[segment].max_file_bytes` of `atrinis.toml` (default 100MB).
- `data:` URIs only support base64 encoding and are similarly restricted after decoding.
- Local protocols such as `file://` are prohibited.

### Example

```json
{
  "version": "atriena.v2",
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
  "targets": [{"platform": "cli"}]
}
```

### report_segments in LLM results

After the background LLM processes the event, the `report_segments` of `JSONResult` can include image/file paths, which Atrinis will deliver together when the report is sent. `url` must be a relative path within the current task working directory; absolute paths, `~`, or `..` cannot be used.


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


Common fields:

| Field | Required | Description |
| --- | ---: | --- |
| `version` | Yes | Protocol version, currently `atriena.v2`. |
| `atriwisp.name` | Yes | Atriwisp name, which is also one of the source identities; only English letters, numbers, `_`, and `-` are allowed; dots are not allowed. |
| `atriwisp.tags` | No | Atriwisp tag, used for logs and statistics. |
| `source` | Yes | Specific event source, such as service name, script name, or RSS name. |
| `id` | Yes | Unique event ID within the source. |
| `created_at` | No | Occurrence time of the external event; the reception time is used if missing. |
| `mode` | Yes | `record`, `direct`, or `llm`. |
| `title` | No | Event title, used for notifications and background Session titles. |
| `format` | No | `text` or `atriyph`, default `text`. |
| `content` | Yes | Event body. For LLM mode, using Atriyph Task Notation `#task` is recommended. |
| `model_slot` | No | Atrinis LLM model slot, only supporting `atriwisp1`, `atriwisp2`, and `atriwisp3`; if left blank or the corresponding slot is not configured, it will fall back to `work`. |
| `tool_list_names` | No | The ATRI-X internal tool name or Skill name preloaded for background tasks; Ordinary tools inject the schema, while Skills inject task descriptions and automatically inject the corresponding runner; Must be within the Atrinis `allowed_tools` adjudication range; `discover_tool` will be ignored. |
| `tools` | No | External tools declared by Atriwisp with events; allowed by default, rejected when hitting the `disabled_external_tools` of that Atriwisp. |
| `targets` | Yes | Atriwisp expects a delivery target array: `{"platform":"telegram"}` indicates sending to the platform superadmin, `type=private/group` with `id` indicates sending to a specified private chat/group chat, and `{"platform":"all"}` indicates all enabled platform superadmins. The final decision is still made by Atrinis. |
| `meta` | No | Original supplementary data, used only for recording and prompt attachment. |


> **Note:** Atriena is based on JSON, and the content must be UTF-8 encoded.

The HTTP response only indicates that Atrinis has received or rejected the request, and does not wait for the LLM to complete.


```json
{
  "accepted": true,
  "duplicate": false,
  "event_key": "server-watchdog/minecraft-main/cpu-alert-001",
  "mode": "llm",
  "status": "queued"
}
```

## CLI Remote Connection

`platform.cli` saves both CLI server and client configurations. `server` is the configuration read when ATRI-X acts as the server, and `clients` is the configuration read when the current command connects to the server as a CLI client.

```toml
[platform.cli]
enabled = true
default_client = "local"
default_url = "ws://127.0.0.1:32172/cli/v1/ws"

[platform.cli.server]
enabled = false
listen = "127.0.0.1:32172"

[platform.cli.server.tokens]
local = ["ATRIX_CLI_LOCAL_TOKEN"]
windows = ["ATRIX_CLI_WINDOWS_TOKEN"]

[platform.cli.clients.local]
token_env = ["ATRIX_CLI_LOCAL_TOKEN"]

[platform.cli.clients.windows]
url = "ws://192.168.1.10:32172/cli/v1/ws"
token_env = ["ATRIX_CLI_WINDOWS_TOKEN"]
```

- When `server.enabled=true`, `atri-x service run` will start a local CLI WebSocket server.
- `server.listen` is the server listening address.
- `default_url` is the default connection address for the client; when connecting to other machines, specify the remote WebSocket address in `clients.<name>.url`.
- `server.tokens` is the list of CLI client ID and token environment variables allowed for server login.
- `clients.<name>` is the client profile; `id` can be omitted, defaulting to `<name>`; `url` can be omitted, defaulting to `default_url`.
- Similar to Provider/Atrinis, the token is read from system environment variables first, and then from the configuration directory `.env`.
- `atri-x cli -c <name>` uses a specified client profile to connect to the server; `-c` can be omitted when there is only one default client.

## Delivery Target and Security Boundary


Atriwisp can declare the expected target in `targets`, but the final target is decided by Atrinis.

`targets` must be an array:

```json
{
  "targets": [
    {"platform": "telegram"},
    {"platform": "telegram", "type": "private", "id": "123456789"},
    {"platform": "qqonebot", "type": "group", "id": "987654321"},
    {"platform": "all"}
  ]
}
```

Semantics:

- Only write `platform`: deliver to the superadmin of that platform.
- `type=private`: deliver to a specified platform private chat.
- `type=group`: deliver to a specified platform group chat.
- `platform=all`: deliver to the superadmins of all enabled platforms; `type` or `id` cannot be written at the same time.

Atrinis allows delivery by default; It is only prohibited when `[delivery_disabled].targets` or a single Atriwisp `disabled_targets` is hit. Setting `{ platform = "telegram" }` in the disable configuration means disabling all deliveries for this platform.

Security Conventions:

- The token name is used only for logs and audit logs, and is not equivalent to the Atriwisp identity.
- The original token text is not written to logs.
- `model_slot` can only choose `atriwisp1`, `atriwisp2`, or `atriwisp3`, and cannot specify an arbitrary internal mode name.
- Atriwisp cannot bypass Tool Runtime and Security Policy to call ATRI-X internal tools; Tool names or Skill names in `tool_list_names` will all undergo Atrinis `allowed_tools` adjudication.

- External `tools` declared by Atriwisp are allowed by default and are injected as model-callable function names in the form of `atriwisp_<atriwisp>_<tool>` via ToolRun; A single Atriwisp can use `disabled_external_tools` to disable specific tools.
- External tool calls are initiated by Atrinis as HTTP JSON POST requests to the declared endpoint; the external tool itself is responsible for the actual risk boundary, and Atrinis handles it as low-risk.

<!-- This file is auto-translated from docs/atrinis-usage.md. Do not edit manually. -->

# Atrinis Configuration and Usage

This article follows [Atrinis listening hub](atrinis.md) and explains how to enable Atrinis, configure Atriwisp policies, and use Atriena to deliver events.

## Quick Start

Atrinis is disabled by default. To enable it, configure it in `app.toml`:

```toml
[atrinis]
enabled = true # Atrinis master switch; if false, the HTTP runtime will not start.

[atrinis.http]
addr = "127.0.0.1:32170" # It is recommended to bind to a local address first, and use a reverse proxy or intranet forwarding when exposure is needed.
max_body_bytes = 1048576 # Request body limit for a single Atriena request.
queue_size = 128 # Background queue length for LLM mode.
workers = 2 # Number of background workers in LLM mode.

[atrinis.tokens.home]
token_env = ["ATRINIS_HOME_TOKEN"] # Read from system environment variables or the .env file in the configuration directory.

[atrinis.delivery]
default_platforms = ["cli"] # Default delivery platforms allowed by Atrinis policies.
allow_superadmins = true # Whether delivery to the target platform's superadmin is allowed.

# Atriwisp is enabled by default; configure it only when you need to limit tokens, override delivery policies, or disable it.
[atrinis.atriwisps.server-watchdog]
allowed_tokens = ["home"]

[atrinis.atriwisps.server-watchdog.delivery]
default_platforms = ["cli"]
allow_superadmins = true

[atrinis.atriwisps.spike-checker]
enabled = false # This Atriwisp is disabled only when enabled=false is explicitly set.
```

`.env` example:

```dotenv
ATRINIS_HOME_TOKEN=change-me
```

Configuration description:

- `[atrinis].enabled=false` will not start the Atrinis HTTP runtime.
- Do not write the token plaintext into the configuration; it is recommended to place it in system environment variables or the configuration directory `.env`.
- `token_env` can contain multiple environment variable names, which will be tried in order.
- Atriwisp is enabled by default; it will receive events if `[atrinis.atriwisps.<name>]` is not specified, if it is specified but `enabled` is not, or if `enabled=true` is specified.
- Only explicit `enabled=false` will disable the corresponding Atriwisp.
- `allowed_tokens` restricts which tokens can represent the Atriwisp to deliver events; if not specified, any authenticated token is allowed.
- `default_platforms` is the default delivery platform allowed by the Atrinis policy.
- `allow_superadmins=true` indicates that delivery to the target platform's superadmin is allowed.

After starting ATRI-X, you can use curl to test a `direct` event:

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

If the same `atriwisp.name + source + id` is sent again, Atrinis will return duplicate and will not redistribute it.

## Atriena Request Example

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
  "tool_list_names": ["shell"],
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

Common fields:

| Field | Required | Description |
| --- | ---: | --- |
| `version` | Yes | Protocol version, currently `atriena.v1`. |
| `atriwisp.name` | Yes | Atriwisp name, which is also one of the source identities. |
| `atriwisp.tags` | No | Atriwisp tag, used for logs and statistics. |
| `source` | Yes | Specific event source, such as service name, script name, or RSS name. |
| `id` | Yes | Unique event ID within the source. |
| `created_at` | No | Occurrence time of the external event; the reception time is used if missing. |
| `mode` | Yes | `record`, `direct`, or `llm`. |
| `title` | No | Event title, used for notifications and background Session titles. |
| `format` | No | `text` or `atriyph`, default `text`. |
| `content` | Yes | Event body. For LLM mode, using Atriyph Task Notation `#task` is recommended. |
| `model_slot` | No | Model slot, used subsequently for `atriwisp1`, `atriwisp2`, and `atriwisp3`. |
| `tool_list_names` | No | ATRI-X tool name preloaded for background tasks; `discover_tool` will be ignored. |
| `tools` | No | Tools declared by Atriwisp along with the event. This is currently a capability under development and is not recommended for reliance. |
| `targets` | No | The delivery target expected by Atriwisp; the final decision is still made by Atrinis. |
| `meta` | No | Original supplementary data, used only for recording and prompt attachment. |

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

## Delivery Target and Security Boundary

Atriwisp can declare the expected target in `targets`, but the final target is decided by Atrinis.

Currently, it is recommended to only use:

```json
{
  "targets": {
    "platforms": ["cli"],
    "superadmins": true
  }
}
```

`platforms` can contain `"all"`, indicating that the request is delivered to all platforms allowed by the Atrinis policy. Atrinis will still calculate the final target based on global configuration, Atriwisp configuration, and platform availability.

Security Conventions:

- The token name is used only for logs and audit logs, and is not equivalent to the Atriwisp identity.
- The original token text is not written to logs.
- Atriwisp cannot send platform messages directly.
- Atriwisp cannot bypass the Tool Runtime and Security Policy to call ATRI-X tools.
- The `tools` declared by Atriwisp is currently still under development and should not be relied upon as a stable interface.

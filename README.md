<!-- This file is auto-translated from README.zh-CN.md. Do not edit manually. -->

# ATRI-X

[中文](README.zh-CN.md) | English

ATRI-X is a lightweight Agent/Chatbot framework written in Go, aiming to minimize operating costs, context costs, and maintenance complexity while preserving extensibility.
It supports general chat, tool calling, Hook extensions, long-term task scheduling, persistent Sessions, and context compaction, making it suitable for scenarios such as personal assistants, platform bots, and orchestratable automation assistants.

## Features

### 1. Lightweight and Efficient

**Ultra-lightweight Go implementation**: ATRI-X's current local startup time is <10ms (N5105, SATA SSD), with resident memory of about 30MB.

**Token-efficient tool discovery**: Research shows that many ordinary users still primarily use LLM-like products as advanced search engines, writing assistants, and listening objects; frequent tool calls are not the norm for all conversations.
Reference: Chatterji et al., *How People Use ChatGPT*, NBER, 2025;Yan et al., *ShareChat: A Dataset of Chatbot Conversations in the Wild*, arXiv:2512.17843, 2025。

ATRI-X does not inject the full schema of all tools by default in every round of conversation, but only exposes `discover_tool` and the names of currently available tools. When the model needs to use a tool, it first discovers the tool details on demand, and then the Agent injects the corresponding schema. Greatly reduces invalid context overhead.

**Chat / Work dual mode**: ATRI-X distinguishes between chat mode and work mode. chat mode completely removes tools, making it more suitable for daily chatting, companionship, lightweight Q&A, and low-cost conversations; Work mode enables tool discovery and tool calling capabilities for complex tasks. The two modes can be configured with models independently, allowing low-cost models to handle casual chat and powerful models to focus on complex tasks.

**Layering of resident memory and long-term memory**: Resident memory only saves short, stable information that truly needs to be injected into every round, and internally distinguishes between 'core' (requiring confirmation for modification) and 'normal' (organizable); Longer and more complex memories are queried by the LLM on demand via `long_memory`. Long-term memory uses Markdown source data and SQLite FTS, balancing transparency and retrieval efficiency.

For personal daily use, token consumption per request:

- work mode: <1000
- chat mode: <500
- Cache hit: >90%

### II. Powerful and Extensible

**Extensible Hook system**: ATRI-X has a built-in Hook Layer, allowing extension logic to be inserted at key event points such as Agent input, LLM request, LLM response, platform sending, and platform connection. Hooks can modify messages, append output intents, call scripts, and more. The exec action of rule Hooks is based on stdio communication, supporting plugins written in any language or script; Combined with [Atriwisp](docs.en/atrinis.md), it can implement powerful functions such as conditional LLM triggering and external event integration.

**Standard Cron and LLM Cron**: ATRI-X features a built-in Cron Runtime and an LLM-orchestratable Cron service. Standard Cron sends fixed content directly according to a schedule; LLM Cron drives model execution using task descriptions, making it suitable for scheduled tasks that require analysis, summarization, or the use of tools.

**Atriyph Task Notation**: Atriyph is used to describe LLM Cron and native skills. The goal is to reduce ambiguity in natural language task descriptions and use a shorter, more stable structure to express inputs, outputs, steps, conditions, and constraints. Compared to arbitrary Markdown, Atriyph is better suited for reusing and passing tasks between LLMs, and is also easier to lint, audit, and process with tools.

**ATRI-X Skills creatable by LLM**: ATRI-X has a built-in `create_atrix_skill` meta-tool, allowing the LLM to crystallize reusable experience into ATRI-X Skills. Automatically validate Atriyph syntax upon creation, with optional Go source code attachment and compilation; The pure Atriyph text or Go source code created is maintained by a unified `read_atrix_skill` / `modify_atrix_skill`; after the source code is modified, it is uniformly formatted and compiled via `finalize_atrix_skill`, and the check results are returned.

**Compatible with external AgentSkills**: In addition to native Go Skills, ATRI-X is also compatible with external AgentSkills that follow the agentskills.io style. Automatically scan `skills/agent/<skill>/SKILL.md` or `SKILL.atriyph` to read the name, description, applicable scenarios, and risk level; Currently, bundled Python scripts can be executed via hidden wrapper tools.


### III. Atrinis Event Perception System

Traditional Agents usually only wait for user input; Cron can only respond to time. Atrinis provides ATRI-X with another trigger method: external events.

Atrinis is the listening hub of ATRI-X, Atriwisp consists of external listeners distributed in various locations, and Atriena is a unified JSON over HTTP event protocol. Working together, these three allow any signal from the external world—such as server alerts, RSS updates, Webhooks, game events, or even external computer information—to be sent to Atrinis, then processed and returned by ATRI-X.

For detailed information, see [Atrinis Listening Hub](docs.en/atrinis.md).

### IV. Flexible Deployment and Enhanced Sessions

**Multi-platform and Rich Output Abstraction**: ATRI-X abstracts the platform and output layers, currently supporting CLI, QQ OneBot, QQ Official, and Telegram, while reserving space for extending to other platforms.

**CLI Client/Server Separation**: Supports using ATRI-X as a client on any computer to connect to the ATRI-X server. **Free Frontend Customization**, allowing you to create any frontend interface you prefer. The following screenshots demonstrate different frontend forms; except for the TUI, all are conceptual HTML mockups and do not represent the final UI.

<p align="center">
  <img src="https://raw.githubusercontent.com/Elfreese/elbot-showcase/main/frontend/assets/frontend_1.png" width="260" />
  <img src="https://raw.githubusercontent.com/Elfreese/elbot-showcase/main/frontend/assets/frontend_2.jpg" width="260" />
  <img src="https://raw.githubusercontent.com/Elfreese/elbot-showcase/main/frontend/assets/tui.png" width="260" />
</p>

For more screenshots, see [elbot-showcase/frontend](https://github.com/Elfreese/elbot-showcase/tree/main/frontend).

**Session, Fork, and Context Compaction**: Built-in persistent Session service, supporting Session recovery, archiving, pinning, Forking, deletion, paginated viewing, and platform isolation. Long conversations automatically trigger context compaction to keep the window controllable; normal conversation can continue after compaction.

### V. Secure and Reliable

**Security Policies and Risk Confirmation**: The tool system has built-in risk levels, role permission checks, and high-risk confirmation processes. Regular users can only discover and invoke low-risk tools; even superadmins must confirm each item when invoking high-risk tools.

**Lightweight Sandbox Isolation**: Background Shell execution is subject to AST-level sandbox constraints. Background tasks have an independent sandbox working directory, reducing the impact of misoperations.

**Comprehensive Logging and Auditing**: Distinguishes between runtime logs, Atriwisp logs, and audit logs, supporting structured fields, log queries, audit queries, and runtime debugging.

## Usage


Common startup methods:

```bash
atri-x              # Automatic mode: Prioritize attempting the default remote CLI client; fall back to full foreground startup when local is unreachable
atri-x run          # Full foreground: Local CLI + Enabled platforms + Cron
atri-x cli [-c 名称] # Remote CLI client: Connect to a resident ATRI-X server
atri-x -c 名称      # Connect to the server directly using a specified CLI client profile
atri-x service run  # Linux/headless service mode: Do not start local CLI; remote CLI server, platforms, and Cron can be enabled
```

Shell completion can be generated via `atri-x completion <shell>`, supporting `bash`, `zsh`, `fish`, `nushell`, `powershell`, and `auto`.

Minimum usage flow:

1. Configure the OpenAI-compatible Provider in `config/providers.toml`.
2. Set the API Key corresponding to `api_key_env` via system environment variables or the configuration directory `.env`.
3. After starting, use the command `/models` to view and then use `/model xx` to select the model. Or manually select the default `chat` / `work` mode and model in `config/state.toml`.
4. Enter `/help` to view commands, or start a conversation directly.

For detailed instructions, see:

- [Quick Start](docs.en/getting-started.md)
- [Configuration Guide](docs.en/configuration.md)
- [Command Cheat Sheet](docs.en/commands.md)
- [Core Concepts](docs.en/concepts.md)
- [Atrinis Listening Hub](docs.en/atrinis.md)
- [Atrinis Configuration and Usage](docs.en/atrinis-usage.md)
- [Frontend API](docs.en/frontend-api.md)

Development plan and task decomposition: [devdocs](devdocs/).

## Development Status

ATRI-X is still under rapid development; interfaces, configurations, and internal implementations may continue to be adjusted. It is currently more suitable for exploration as a personal Agent/bot framework.


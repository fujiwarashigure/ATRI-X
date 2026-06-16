<!-- This file is auto-translated from docs/getting-started.md. Do not edit manually. -->

# Quick Start

This document is used to get ATRI-X up and running and complete your first CLI conversation.

## Environment Requirements

- Go 1.26 or a newer version.
- An OpenAI-compatible LLM service, such as DeepSeek, OpenAI, or other services compatible with `/chat/completions`.
- At least one available API Key.

## Get the Code

```bash
git clone https://github.com/Elflare/elbot/
cd atri-x
```

If you are already in this repository, you can proceed directly to the next step.

## Configure Provider

ATRI-X reads `config/app.toml` by default, and loads `config/providers.toml` and `config/state.toml` via `[config_files]` within it.

The default example already includes DeepSeek and OpenAI Providers:

```toml
[providers.deepseek]
base_url = "https://api.deepseek.com"
api_key_env = "DEEPSEEK_API_KEY"

[providers.openai]
base_url = "https://api.openai.com/v1"
api_key_env = "OPENAI_API_KEY"
models = ["gpt-4o-mini"]
```

It is recommended to place keys in system environment variables or in the `.env` file under the configuration directory, rather than writing them directly into `providers.toml`.

Example `.env`:

```dotenv
DEEPSEEK_API_KEY=your-api-key
OPENAI_API_KEY=your-api-key
```

## Select Default Model

The default runtime model is specified in `config/state.toml`:

```toml
[session]
default_mode = "work"

[mode_models.work]
provider = "deepseek"
model = "deepseek-chat"

[mode_models.chat]
provider = "deepseek"
model = "deepseek-chat"
```

If using other Providers, you need to synchronously modify `provider` and `model`.
`default_mode` is set manually; other settings can be configured using commands in the CLI.

## Start ATRI-X

During development, you can run it directly:

```bash
go run ./cmd/atri-x
```

Specify a configuration file:

```bash
go run ./cmd/atri-x --config config/app.toml
```

Common running modes:

```bash
atri-x              # 自动模式
atri-x run          # 完整前台：CLI + 已启用平台 + Cron
atri-x cli          # 本地 CLI-only：只启动 CLI，不启动平台和 Cron
atri-x service run  # Linux/headless 服务模式：不启动 CLI，启动已启用平台和 Cron
```

In automatic mode, if Linux detects that the current user already has `atri-x service run` running, it will enter local CLI-only mode to avoid duplicate platform connections or duplicate Cron executions; Otherwise, it will enter full foreground mode. Windows does not perform service detection and starts in full foreground mode by default.

`atri-x cli` is an independent local process that uses the same set of configurations and SQLite data, but it will not take over current requests, confirmation states, or the current Session in memory from the service process. When you need to continue a historical Session, you can use `/list` and `/resume`.

Build binary:

```bash
go build -o atri-x ./cmd/atri-x
```

On Windows:

```bash
go build -o atri-x.exe ./cmd/atri-x
```

## Shell Completion

ATRI-X can generate completion scripts for common shells:

```bash
atri-x completion bash
atri-x completion zsh
atri-x completion fish
atri-x completion nushell
atri-x completion powershell
```

You can also use `auto` to guess the shell based on current environment variables:

```bash
atri-x completion auto
```

Example: fish

```bash
atri-x completion fish > ~/.config/fish/completions/atri-x.fish
```

Example: nushell

```bash
atri-x completion nushell > ~/.config/nushell/completions/atri-x.nu
```

Then source this file in your Nushell configuration.

## First Conversation

After starting, simply enter your message. In the default example, the `default_mode` of `config/state.toml` is `work`, which enables tool discovery capabilities.

Common getting started commands:

```text
/help
/status
/models
/model 1
/chat
/work
```

- `/help` View available commands.
- `/status` View current Session, model, context, and request status.
- `/models` View the list of available models.
- `/model <编号或名称>` Switch models.
- `/chat` Switch to low-cost chat mode.
- `/work` Switch to work mode where tools can be used.

For more commands, see [Command Quick Reference](commands.md).

## Configuration Directory and Data Directory

Default configuration lookup order:

1. The path specified by `--config`.
2. `ATRIX_CONFIG_FILE` environment variable.
3. Platform configuration directory, such as Windows `%APPDATA%/ATRI-X/app.toml` or Linux XDG configuration directory.
4. Source code directory `config/app.toml`.

Runtime data is stored in the platform data directory by default, such as SQLite, logs, sandbox, and artifacts. For detailed rules, see [Configuration Instructions](configuration.md).

## FAQ

### API Key missing error during startup

Check:

- Whether `api_key_env` in `providers.toml` is written correctly.
- Whether the system environment variables or the configuration directory `.env` contain the corresponding Key.
- Whether the current shell can read these environment variables.

### Model unavailable or incorrect name

Check:

- Whether `provider` in `state.toml` exists in `providers.toml`.
- Whether the name `model` is supported by the Provider.
- After starting, you can use `/models --fresh` or `/models --refresh` to refresh the model list.

### Do not want to enable tools by default

Change the default mode in `config/state.toml` to chat:

```toml
[session]
default_mode = "chat"
```

Enter `/work` when tools are needed.

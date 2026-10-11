# 冒烟与稳定性测试记录

本文记录本机对 fork 做的冒烟/稳定性测试：跑了什么、实测结果、以及本机 Docker 的两个坑。
**只写实测到的事实**，没有跑到的项明确标出。

## 1. 结论速览

| 测试 | 结果 |
| --- | --- |
| 仓库自带部署脚本测试（`deploy/tests/*.sh`，7 个） | **7/7 通过** |
| Go 测试稳定性（受影响 11 个包 × 3 轮，`-shuffle=on`） | 16 个包全绿；**仅 2 个测试确定性失败**（Windows 无 `sh`，见 §2） |
| 同样 17 个包在 **Linux 容器**内跑 | **全部 `ok`，0 失败**（含 `internal/agent`） |
| 生产镜像构建（`deploy/Dockerfile` → `runtime` target） | **成功**，镜像内 `atri-x --version` = `0.6.8` |
| 容器运行冒烟 | **通过**：`/live`、`/ready`、`/healthz`（带 token）、`/metrics`（带 token）均 200；无 token 时 `/healthz` 为 401；数据目录自动生成；SIGTERM 退出码 0 |
| 进程级冒烟：`state.toml [model_snapshots]` 分片 | **通过**：真实进程启动后手工追加该分片，15 秒轮询内热加载生效并记录 `sections=model_snapshots`（见 §6） |

## 2. 那两个失败只是"本机没有 sh"

`TestEmoticonHookSendsSeparateOutputAndCleansPersistedContent` 与
`TestToolCallAssistantEmoticonSendsBeforeFinalResponse` 报 `exec: "sh": executable file
not found in %PATH%`。3 轮随机顺序运行中 **3/3 稳定失败**（不是 flake）。

判定为环境问题而非代码缺陷的依据：用 `deploy/Dockerfile` 的 `go-runtime` 阶段（真
Linux + 自带 `sh`）在容器内跑同一个包：

```
ok  atri-x/internal/agent  4.140s
```

在 Linux 上这两个测试**通过**。Windows 的 Go 走 Windows 的 PATH，即使从 WSL 里启动
Windows 版 `go.exe` 也找不到 WSL 的 `/usr/bin/sh`（已实测确认），所以只能在 Linux 里验。

## 3. 稳定性测试数值

3 轮，每轮 `go test -count=1 -vet=off -shuffle=on -p 1`，覆盖 `logging`、`session`、
`config`、`storage/sqlite`、`cron`、`atrinis`、`app`、`delivery`、`tool`、`hook/...`、`agent`：

- 16 个包每轮全部 `ok`；
- 每轮仅 `internal/agent` FAIL，且失败集合恒定为上述 2 个测试（3/3，确定性）；
- 单轮耗时 46–58 秒；
- **没有出现任何偶发（flake）失败**——包括之前在完整套件里偶发的 `TempDir RemoveAll
  cleanup: directory is not empty` 也没有复现。

同一组包在 Linux 容器内一次性全绿（`internal/agent` `ok 4.322s`）。

## 4. 容器冒烟细节

镜像：`atri-x:smoke-0.6.8`（`--build-arg VERSION=0.6.8 GOPROXY=https://goproxy.cn,direct
APT_MIRROR=mirrors.aliyun.com`）。

实测到的端点行为（宿主端口映射到容器 `32171`）：

| 请求 | 结果 |
| --- | --- |
| `GET /live` | `200 {"status":"live","version":"0.6.8",...}` |
| `GET /ready` | `200 {"status":"ready","ready":true,"checks":{...:"ok"}}` |
| `GET /healthz`（无 token） | `401` |
| `GET /healthz`（`X-Atrix-Ops-Token`） | `200 {"status":"ok","live":true,"ready":true,"degraded":false,...}` |
| `GET /metrics`（`X-Atrix-Ops-Token`） | `200` |

首次启动自动生成数据目录：`app.toml`、`services.toml`、`state.toml`、`atrinis.toml`、
`tool_tags.toml`、`memories.toml`、`SOUL.md`、`angel_memory.db`、`self_learning.db`、
`plugins/`、`skills/`、`long_memory/`。

优雅停止：`docker stop` 后 `exit code = 0`、`OOMKilled = false`。

> 注意：不带 `ATRIX_OPS_TOKEN` 时 `/healthz` 期望返回 **404**（未注册，安全默认），
> 与带 token 时的 200 是两种不同行为；`deploy/README.md` 已说明该默认。Elwis 的
> `32170/healthz` 是另一回事（仅 Atrinis 启用时），不要混用。

## 5. 本机 Docker 的两个坑（实测）

### 5.1 从 M: 盘挂载数据卷会静默卡死

仓库位于 `M:\`。用 `-v "M:\...\.smoke\data:/data"` 启动时，容器**永远停在
`Created`**，`docker inspect` 的 `.State.Error` 为空、没有日志、容器从不进入
`running`；`docker run -d` 却返回了容器 ID，看起来像"启动很慢"。同一个镜像不挂载
时立刻 `running`，从 `C:\` 挂载也正常。

- 影响：`deploy/docker-compose.yml` 用 `./data:/data`，因此在 `M:\` 下直接
  `docker compose up -d` 会踩到这个坑。
- 规避（本次冒烟采用）：把数据卷放在 `C:` 下，或把仓库移到 `C:`，或在 Docker Desktop
  的 File Sharing 里共享 `M:`。
- 这是**本机 Docker Desktop 配置问题**，不是 ATRI-X 缺陷；未去改 Docker Desktop 设置。

### 5.2 反复创建/删除容器后 daemon 会变慢

连续多轮 `docker run` + `docker rm -f` 之后，新容器也会停在 `Created`（同样的静默
表现）。此时 `docker ps` 本身也开始变慢。实测**把卡住的容器删掉后，普通容器又能正常
启动**，因此不是镜像问题；如果再次出现，优先怀疑 daemon 需要重启，而不是应用回归。

## 6. 进程级冒烟：`state.toml` 新增分片与热加载（后续追加）

命名模型快照给 `state.toml` 增加了 `[model_snapshots]` 分片，光有单元测试不足以证明它接进了
**真实启动与热加载链路**，因此补了一次本机进程级冒烟（无 Docker，直接跑构建产物）：

```
go build -o <space-free-dir>\atri-x.exe ./cmd/atri-x
set APPDATA=<space-free-dir>\appdata          # 让默认配置目录可写（否则默认目录 mkdir 被拒）
set ATRIX_HEALTH_ADDR=127.0.0.1:33281
set ATRIX_OPS_TOKEN=smoke-token
atri-x.exe service run                          # 不传 --config，走平台默认配置目录并自动生成
```

实测结果：

| 项目 | 结果 |
| --- | --- |
| 首次启动自动生成配置 | `app.toml`、`services.toml`、`state.toml`、`atrinis.toml`、`tool_tags.toml`、`memories.toml`、`SOUL.md`、`angel_memory.db`、`self_learning.db`、`data/*.db`、`data/logs/*`、`plugins/`、`skills/` 全部生成 |
| `GET /live` / `/ready` / `/metrics`（带 `X-Atrix-Ops-Token`） | 全部 `200`；`/ready` 的 4 项检查全 `ok` |
| 启动耗时 | stderr 打印 `atri-x startup completed in 103.3ms` |
| 手工往 `state.toml` 追加 `[model_snapshots.smoke]` 后 | 15 秒轮询内被热加载，审计日志出现 `event=runtime_state_reloaded module=agent sections=model_snapshots trigger=watch result=succeeded` —— 说明新分片真的进了 `applyRuntimeState` / `runtimeStateDigest`，不只是能解析 |
| 热加载后文件内容 | `[model_snapshots.smoke]` 仍在，进程全程 ready，无 stderr 报错 |

两点本机事实（不是产品缺陷）：

- `--config <不存在的文件>` 会直接失败（`read config ... cannot find the file specified`），自动生成默认配置只发生在**不传 `--config`** 时；
- 默认配置目录在 `%APPDATA%\ATRI-X`，本机 dpx 环境的 `%APPDATA%` 目录 mkdir 被拒（`Access is denied`），因此冒烟把 `APPDATA` 指到工作目录下的可写目录。仓库路径带空格时 `Start-Process` 的 `-ArgumentList` 会被拆开，冒烟目录放在无空格路径下。

### 6.1 平台连接信号（后续追加）

平台连接事实从"连接回调直接转给 Agent（`Agent.NotifyPlatformConnected`）"改成"适配器发布全局信号
`platform.PlatformConnected` + hook 侧订阅后按平台排队执行"（`67508075`）之后，同样补了一次进程级
冒烟（无 Docker，直接跑构建产物）：

```
go build -o <临时目录>\atri-x.exe ./cmd/atri-x
set APPDATA=<临时目录>\appdata
set ATRIX_HEALTH_ADDR=127.0.0.1:33291
set ATRIX_OPS_TOKEN=smoke-token
atri-x.exe service run
```

| 项目 | 结果 |
| --- | --- |
| 启动 | stderr 只有 `atri-x startup completed in 110.2586ms`，进程存活 |
| `GET /live` | `200` |
| `GET /ready` | `200`，`chat_history_sqlite_dir` / `process` / `scheduler` / `sessions_sqlite_dir` 全 `ok` |
| `GET /metrics` | `200`，`goroutines=14`，字段正常 |
| stderr 全程 | 没有信号订阅或关闭相关报错 |

**本次没覆盖到的（诚实标注）**：`service run` 用新生成的默认配置启动，`/metrics` 报
`checks.platforms = "none configured"`，因此**没有真实平台连接发生**。这条链路只由单元测试覆盖：
`internal/app/platforms_signal_test.go` 验证连接回调确实发布信号（并验证适配器不报名字时用适配器名），
`internal/agent/platform_signals_test.go` 验证信号驱动 hook、发布方不等 hook 业务完成、同平台串行、
关闭后不再接纳、`StartPlatformEvents` 幂等且关闭后不可重启。真实平台端到端需要外部服务与密钥。

**本机跑冒烟时的一个坑（实测）**：`atri-x service run` 会持有服务标记用的命名互斥体，进程没退干净时
`go test ./internal/app/` 会以 `atri-x service already appears to be running`
（`service_marker_windows_test.go`）失败——这不是代码缺陷，跑完冒烟先停进程再跑测试。

### 6.2 全量改动回归（后续追加）

项2（`/model` 兼容性裁决）、项5（通知按平台排队）与 §9 的查询层改动落地后，再跑了一次同样的进程级冒烟，确认启动链路没有被这些改动影响：

| 项目 | 结果 |
| --- | --- |
| 启动 | stderr 只有 `atri-x startup completed in 122.3965ms` |
| `GET /live` | `200` |
| `GET /ready` | `200`，4 项检查全 `ok` |
| 进程 | 存活，无 panic |

**没覆盖到的（诚实标注）**：`CloseNoticeQueues` / `ClosePlatformEvents` 的"先停止接纳再排空"只由单元测试覆盖——`atri-x service run` 没有优雅停机入口（`Stop-Process` 是硬杀，不经 30 秒关闭预算），所以进程级没有验证"关闭时排空在途通知"这件事；`internal/app` 的 Runner 测试覆盖了 cleanups 的执行顺序与预算。

### 6.3 平台失败隔离、service 保活与跨平台超管通知（后续追加）

项2（平台失败隔离 + service 保活 + 超管通知 `fa3d31e`）改的是进程生命周期与 health 语义，因此专门做了一次**能真正触发平台失败**的进程级冒烟：把 CLI WebSocket 服务端指向一个已被占用的端口，让唯一的平台运行时以 `bind: address already in use` 立刻退出；同时启用一个只会重连退避、不会退出的 Telegram 适配器（bogus token）作为"另一个仍在运行"的平台，`security.superadmins.telegram = ["999"]`。

```
go build -o <临时目录>\atri-x-smoke\atri-x.exe ./cmd/atri-x
# app.toml: [platform.cli.server] enabled = true / listen = "127.0.0.1:33172"（先被一个 TcpListener 占住）
#           [platform.telegram] enabled = true / bot_token = bogus / api_timeout_seconds = 1
#           [security.superadmins] telegram = ["999"]
set APPDATA=<临时目录>\atri-x-smoke\appdata
set ATRIX_HEALTH_ADDR=127.0.0.1:33180
set ATRIX_OPS_TOKEN=smoke-token
atri-x.exe service run
```

| 项目 | 结果 |
| --- | --- |
| 平台失败 | `level=WARN msg="platform stopped with error" platform=cli error="listen tcp 127.0.0.1:33172: bind: Only one usage of each socket address ..."` |
| 进程 | **存活**（12 秒后 `alive=True`）——改动前这里会 `return nil` 让进程以 exit 0 静默结束 |
| `GET /live` | `200`，`uptime_seconds=11`（进程确实没退） |
| `GET /ready` | `200`，`ready=true`，4 项检查全 `ok`（平台断开不影响 ready，与文档一致） |
| `GET /healthz`（带 `X-Atrix-Ops-Token`） | `200`，`"degraded":true`；`platforms` 里 `cli` 为 `connected=false` + `last_disconnected_at` + `last_error` = 上面那条 bind 错误，`telegram` 为 `not connected` |
| 超管通知 | 失败后 2 秒：`level=WARN msg="notice output failed" platform=telegram error="Post \"https://api.telegram.org/bot<redacted>/sendMessage\": context deadline exceeded"` —— 目标平台与收件人确实被解析并发起了真实发送（失败原因是假 token / 无外网） |

**结论**：`MarkPlatformDisconnected`（此前全仓库只有测试调用）现在真的被调用，`/healthz` 会反映平台断开；service 模式在全部运行时退出后不再跟着退出；跨平台超管通知走到了真实发送路径。**没覆盖到的**：没有可用的真实平台，因此没有验证"通知成功送达"（只验证到发起发送）；`StopAppOnExit` 在非 service 模式下的退出（`atri-x run` + `/exit`）也只由单元测试覆盖。

## 7. 未覆盖的部分（诚实标注）

- 本轮（§6.3 这批改动）跑过完整 `go test -count=1 -vet=off -p 1 ./...`：全仓只有本机既有失败——
  `internal/agent` 的 2 个 `TestEmoticonHook*`（`exec: "sh"` 缺失）与 1 例随机 `state.toml` 临时目录
  清理竞态（本轮命中 `TestApplyModelSnapshotMergesExternalEditFirst`，单独跑通过）、
  `internal/tool/builtin` 的 7 个 shell/PATH 相关用例、`internal/character` 与 `internal/request`
  的 `[build failed]`（写测试二进制时 `Access is denied`，见 §6 的说明）。没有**新**失败。
- 没有用真实平台（OneBot / Telegram / QQ 官方）做端到端消息冒烟：这些需要外部服务与
  密钥，本次没有条件。
- 没有跑 `-race`：本机 Go 报 `-race requires cgo`，本机未启用 cgo。
- 没有跑 `deploy/tests` 之外的运维脚本（`deploy/*.sh` 的备份/升级/回滚主脚本），
  只跑了它们对应的测试脚本。
- 没有验证 QQ OneBot 的 3000 rune 合并转发、媒体入库到 S3 等需要真实依赖的路径。

# Bitter Lessons(踩坑教训沉淀)

本文件记录**已经定位、修复并通过验证**,且对未来开发有长期警示价值的教训。
未解决的问题继续留在 `handoff.md` 和 issue 中;只有修复完成、有测试或验收证据、
并且教训本身具有跨任务警示价值的事件才收录到这里。

每条教训按统一结构记录:现象、根因、修复、防范。新条目编号递增,日期为
修复验证日期。引用文件、测试名或分支作为证据,便于后来者复核。

## 索引

| 编号   | 日期       | 模块                | 一句话教训                                                                      |
| ------ | ---------- | ------------------- | ------------------------------------------------------------------------------- |
| BL-001 | 2026-08-29 | box / local-process | 镜像系统 API 的结构体,字段顺序必须逐字对照官方文档;平台代码必须在目标平台上测试 |
| BL-002 | 2026-08-29 | box / local-process | 重写进程监督链路时,输出捕获等"看不见的行为"最容易静默丢失                       |
| BL-003 | 2026-08-29 | box / local-process | 同一份资源限额在不同平台是不同语义:hard 与 advisory 必须区分对待                |
| BL-004 | 2026-08-29 | 工具链              | Windows 上交叉编译 Go 必须显式 `CGO_ENABLED=0`                                  |

---

## BL-001 镜像系统 API 的结构体,字段顺序必须逐字对照官方文档

- **日期/模块**: 2026-08-29,`box/runtimes/local-process`(分支
  `feat/box-local-process-runtime`)
- **现象**: Windows 上所有任务启动即失败:任务记录为 `failed`、退出码为空
  (网关侧映射为 0),六个进程级测试全部失败。失败发生在 0.1 秒内,任务进程
  根本没有运行。
- **根因**: `process_windows.go` 中镜像 Win32 结构体
  `JOBOBJECT_CPU_RATE_CONTROL_INFORMATION` 时,把 `CpuRate` 放在了
  `ControlFlags` 前面。MSDN 规定 `ControlFlags` 是第一个字段。内核按偏移读取
  第一个字得到 `ControlFlags = 625`(含非法标志位),`SetInformationJobObject`
  返回 `ERROR_INVALID_PARAMETER`("The parameter is incorrect"),CPU 限额设置
  失败,`launchTask` 中止启动。作者在 macOS 上开发(macOS 无强制限额路径),
  因此从未暴露。
- **修复**: 调整字段顺序为 `ControlFlags`、`CpuRate`,并加注释锁定顺序不可
  变更。修复后 `TestProbeRunsSupervisedLifecycle` 等用例通过。
- **防范**:
  - 凡是用 Go 结构体镜像系统 API 且经 `unsafe` 传内核的代码,字段名、顺序、
    类型必须逐字对照官方文档,并加注释注明"顺序不可变";
  - 平台特定代码(`//go:build windows` / `unix`)必须在目标平台上至少运行一次
    测试,仅交叉编译通过不能证明行为正确。

## BL-002 重写进程监督链路时,输出捕获等"看不见的行为"最容易静默丢失

- **日期/模块**: 2026-08-29,`box/runtimes/local-process`
- **现象**: 任务正常启动、退出码正确,但 stdout/stderr 永远为空,
  `TestRunStreamsOutputAndTerminalState`、`TestRunReportsNonZeroExit` 失败。
- **根因**: runner 重写过程中,`startTaskProcess` 丢失了对子进程输出的接线:
  `exec.Cmd` 的 `Stdout`/`Stderr` 为 nil 时子进程输出连接到 `os.DevNull`,
  runner 拥有的 `task-stdout.log` / `task-stderr.log` 无人写入,网关的
  `follow`/`emitOutput` 按字节偏移流式转发时永远读到零字节。作者在分支
  handoff 中已正确怀疑到这一点——handoff 是有效的调试线索来源。
- **修复**: `launchTask` 打开两个任务日志文件并传入两个平台的
  `startTaskProcess`,在启动前赋给 `command.Stdout`/`command.Stderr`;
  runner 在进程启动后立即关闭自身句柄(子进程已继承)。
- **防范**:
  - 重写执行链路(supervise/launch/stream)时,先列一份"行为清单":输出去向、
    退出码传播、终态记录、句柄与进程清理,重写后逐项对拍;
  - 退出码正确但无输出的组合,优先怀疑捕获接线而不是任务本身。

## BL-003 同一份资源限额在不同平台是不同语义:hard 与 advisory 必须区分对待

- **日期/模块**: 2026-08-29,`box/runtimes/local-process`
- **现象**: 修复 BL-001 后任务能够启动,但 Go 测试二进制充当的任务进程以
  `0xc00000fd`(STATUS_STACK_OVERFLOW)崩溃,无任何业务输出。
- **根因**: 测试规格使用 `MemoryBytes: 1MB`。该限额在 macOS 上是 advisory
  (无强制机制),在 Linux CI 上因 cgroup 不可写而退化为 advisory,而在
  Windows 上 Job Object 的 `JobMemoryLimit` 是内核硬限制:Go 运行时在
  CREATE_SUSPENDED 恢复运行后的提交内存超过 1MB,连线程栈都无法提交,
  进程立即崩溃。同一份限额代码,三个平台三种行为。
- **修复**: 测试规格(`testSpec`)的内存上限调整为 256MB——它只是内核上限,
  不产生实际预留,但足以容纳测试二进制自身的运行时开销。修复后
  `TestRunTimeoutTerminatesProcessTree`、`TestRunCancelTerminatesProcessTree`
  等进程树终止用例全部通过。
- **防范**:
  - 涉及资源限额的测试,必须在"限额真正强制生效"的平台上验证,而不是只在
    advisory 平台上验证;
  - 生产语义上,小于任务最小可行内存的限额应当被提前拒绝
    (`LIMITS_NOT_ENFORCEABLE` 一族),而不是让任务以晦涩的内核错误码崩溃。

## BL-004 Windows 上交叉编译 Go 必须显式 `CGO_ENABLED=0`

- **日期/模块**: 2026-08-29,box 模块交叉编译检查
- **现象**: 在 Windows 上执行 `GOOS=linux go build ./...` 失败:`runtime/cgo`
  的 Linux 源码(`grp.h`、`sys/mman.h`、`sigaction` 等)被 MinGW gcc 以
  Windows 头文件编译,产生大量 fatal error。
- **根因**: Git Bash 环境的 PATH 上有 MinGW gcc,cgo 因此默认启用;交叉编译时
  Go 用宿主机 gcc 编译目标平台的 cgo 源码,必然失败。
- **修复/操作**: 交叉编译检查使用
  `CGO_ENABLED=0 GOOS=linux go build ./...`(darwin 同理)。box 分支 handoff
  所称 "cross-compiles clean" 即为此方式。
- **防范**: 脚本化交叉编译检查时应显式设置 `CGO_ENABLED=0`,不依赖执行环境
  恰好没有 C 编译器。

---

## 已沉淀在别处的环境类教训

以下教训已经写入权威位置,此处仅作索引,避免内容分叉:

- Windows WinNAT 保留 TCP 5355-5454,导致 PostgreSQL 默认端口 5432 不可用,
  本地栈使用 15432 —— 见 `AGENTS.md` 第 5 节与 `.env` 的
  `POSTGRES_HOST_PORT`。
- 本工作站 `proxy.golang.org` 不可达,Go 模块代理使用
  `GOPROXY=https://goproxy.cn,direct` —— 见 `.env` 与
  `scripts/testenv.mjs` 的环境隔离逻辑。

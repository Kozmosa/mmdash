# ADR 0005: local-process Runtime 的平台支持范围收敛为 Linux

Status: accepted for Stage 8 `local-process` delivery（issue #47，PR #54）

## 背景

issue #47 引入 `local-process`（裸机进程）Runtime，解决宿主平台不支持嵌套
虚拟化、`local-docker` 无法工作的 Box 部署环境。实现与评审完成后，平台支持
范围需要明确：

- 这类环境的绝大多数是 **Linux 云 VM 与容器**（无嵌套虚拟化的 VM、无特权
  或无 docker socket 的容器），Windows 占比可以忽略；
- 交付中遗留的低权限账户执行 TODO 暴露了平台成本差异：Linux 只需设计
  `setuid/setgid` 路径，而 Windows 需要令牌/登录会话模型
  （`CreateProcessAsUser` 一族），复杂度显著更高；
- Windows 已有两条一等公民替代路径：Docker Desktop（`local-docker`）与
  WSL 内启动 Box（即 Linux 路径）。

## 决策

- `local-process` 的**产品支持目标是 Linux**，尤其是无嵌套虚拟化的
  VM/容器场景。
- **Windows 不作为支持目标**。Windows 用户路径：安装 Docker Desktop 走
  `local-docker`，或在 WSL 中启动 Box 走 Linux 路径。即便存在
  "Windows 云 VM 无嵌套虚拟化、装不了 Docker Desktop"的小众场景，也明确
  列为不支持（请改用 Linux 主机）。
- 已交付的 Windows 平台路径（Job Object + `CREATE_SUSPENDED` +
  kill-on-job-close，见 `box/runtimes/local-process/process_windows.go`）
  **保留为"开发/测试环境可用"的最佳努力实现**：不删除、不回退，但不构成
  支持承诺，修复优先级以 Linux 为准。

## 理由

1. **场景域判定**：需求动机（无嵌套虚拟化 → Docker 不可用）天然落在
   Linux 域，与真实用户分布一致。
2. **替代路径充分**：Windows 的两种替代（Docker Desktop / WSL）都是一等
   公民路径，不因本决策出现能力空档。
3. **投入收敛**：低权限执行 TODO 只剩 Linux `setuid/setgid` 一条设计路径；
   测试矩阵与验收覆盖收敛到 Linux；Linux 侧的 cgroup v2 委派 + 进程组与
   目标环境天然契合，fail-closed 探测语义在目标场景下行为可预期。

## 备选方案

- **全平台支持（否决）**：为小众 Windows 场景长期背负令牌模型、账户准备
  与跨平台测试矩阵成本，与场景占比不匹配。
- **删除 Windows 代码（否决）**：Windows 路径已实现并在开发机通过全量测试，
  保留为零成本的最佳努力路径，也便于未来若决策反转时恢复支持。

## 后果

- 后续低权限账户执行（`config.LocalProcess.User`）只在 Linux 上设计实现。
- 文档、handoff 与支持口径统一以 Linux 为准；Windows 相关叙述需注明
  "非支持目标、最佳努力"。
- 若未来出现"本地轻量 VM"（Firecracker/Hyper-V 等）需求，按 Runtime
  adapter 架构新增 Runtime，而不是给 `local-process` 增加隔离开关。
- CI/验收只需覆盖 Linux 目标路径；Windows 上的开发自测不受影响。

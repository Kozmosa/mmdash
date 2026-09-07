# TODO: Progress 自动评估提速（三项优化，已实施）

> 实施状态：三项代码 TODO 已完成；末尾保留了无法由本次代码变更解决的宿主环境门禁说明。

> 交接对象：实现 agent。开工前必读仓库根目录 `AGENTS.md`（工作方法、验证策略、
> commit 规范）。本文所有结论都在 2026-09-07 的 `main` 分支上核实过，行号会有
> 漂移，以函数名/符号为准。前置：本任务建立在 commit `6275fa2`（无人值守评估
> 容错）之上，不要改动该提交引入的审批/轮询行为。

## 背景速览

自动进度评估慢的主因不是单次取证太多，而是三件叠加的事：

1. 所有评估共用一个持久 Hermes Session，上下文随评估次数线性增长；定时评估
   间隔超过 provider 缓存 TTL 时，每次都全价重读全部历史。
2. 每次评估的 instructions 携带约 4-5K tokens 的稳定规则，且含每次都变的
   evaluation ID，破坏前缀缓存。
3. 证据工作流的四个领域查询（code/model/experiment/article）串行执行，每轮
   往返都带全量上下文。

准确性约束（不可破坏）：证据必须当次从 Data Hub 现读，上次评估输出只能作为
对比基线；评估器只读； contracts（OpenAPI/event/schema）本三项均不应变化。

---

## 任务 2（先做）：核实并补强「无变化短路」

### 目标
项目证据状态没有变化时，不发起新的 Hermes 评估。

### 现状（已核实，不要重复造轮子）
- `input_version` = `canonicalInputVersion(input)`（`backend/internal/progress/tracking.go:578`，
  对 assembled input 的 canonical JSON 做 sha256）。input 含 evidence_catalog
  （含 revision 与 object_type_counts）、`progress.state_revision`、settings、
  project_id，因此 `input_version` 相同 ⟺ 证据状态相同。
- `FinalizeRequest`（`backend/internal/progress/tracking_postgres.go:557`）第 571 行
  的预查**已经**把同 `input_version` 的新请求 merge 进已有评估：
  `status IN ('queued','running') OR (status='succeeded' AND NOT $3)`，其中 `$3` =
  `claim.Force`。merge = 新请求标记 `merged` + `merged_into_evaluation_id`，
  不创建新 evaluation、不建 job、不调 Hermes。
- `Force` 目前只来自 `progress.recalculate` API（`backend/internal/progress/module.go:413`
  → `Recalculate(..., body.Force)`），即人工强制重算永远完整重跑（产品语义，
  保留，勿改）。

### 待办
1. **核实 `evidence_catalog.revision` 是 Project-scoped 的**。追它的生成来源
   （datahub 投影 revision）。如果它实际是全局/跨项目递增的，同一项目无变化时
   input_version 也会变，merge 永远不生效——这是本任务最可能存在的真 bug，
   若属实则把它改为 Project-scoped（或仅将 Project 相关部分纳入 input_version
   的输入），并补测试。
2. **核实 Force 的取值路径**：确认 event/cron/manual（非 recalculate）触发的
   `RequestClaim.Force` 恒为 false；`progress.recalculate` 传 body.Force。
   若发现其他调用点传 true，逐个确认语义。
3. **补集成测试**（progress 包已有真实 PostgreSQL 集成测试模式，沿用其门控）：
   - 同 input_version 的第二次 event 请求 merge 进 succeeded 评估，不创建 job，
     Hermes adapter 不被调用（可用 mock/断言 job 表无新行）；
   - `Force=true` 时不 merge、新建 evaluation 并正常排队；
   - 任一 revision 变化（evidence 或 state）后不再 merge。
4. **文档**：在 `docs/development/progress.md` 的 evaluator 生命周期段落补一段
   「无变化请求 merge 进最近成功评估；recalculate 可强制重跑」。

### 验收
- 上述测试全绿；`cd backend && go test ./internal/progress/...` 通过。
- 不新增迁移、不改 contracts。

---

## 任务 3：prompt 重排（缓存友好化）

### 目标
让每次评估的提示词最大化命中 provider 前缀缓存，并减少串行往返。

### 现状
- `progressEvaluationSystemPrompt`（`backend/internal/agent/progress_automation.go`
  顶部常量）：会话级 system prompt，创建会话后 Hermes 不可修改。
- `progressEvaluationInstructions(projectID, evaluationID)`：每次 run 附带的
  instructions，含完整证据工作流 + 每次都变的 `evaluationID` 和 `projectID`。
- `progressEvaluationPromptVersion = "v2"`：参与确定性 remote session ID
  （`progressSessionRemoteID`）。**Hermes 不能改已建会话的 system prompt，改
  prompt 必须 bump 版本号 → 生成全新确定性会话**（既有机制，勿绕开）。

### 待办
1. **内容迁移**：把 instructions 中不依赖单次 run 的稳定内容（MANDATORY MCP
   EVIDENCE WORKFLOW、EVIDENCE RULES、READABLE FEEDBACK AND ACTIONS、OUTPUT
   CONTRACT、UNATTENDED RUN CONTRACT）迁入 `progressEvaluationSystemPrompt`。
   工作流中写死的 `project %s` 改为引用 run input seed 里的 `project.project_id`
   字段（input seed 必含该字段），使 system prompt 完全 Project 无关、字节级稳定。
2. **instructions 瘦身**：迁完后 instructions 只剩一行量级的本次任务说明（例如
   「Evaluate the current Progress evaluation for this Session's Project following
   the Session workflow」），**去掉 evaluationID 与 projectID**，保证同一会话内
   每次 run 的 instructions 字节级一致。input seed（run `input`）保持现状不变。
3. **并行读取**：在工作流第 5 步明确允许/要求把四个领域的 `data.list` 合并到
   同一 assistant turn 并行发起（Hermes 支持并行 tool calls）；第 1、2 步的
   `project.get` + `progress.get` 同理。保持「先 list 后 read、每域最多两次
   read」的证据纪律不变。
4. **版本 bump**：以上内容落到 system prompt 后必须把
   `progressEvaluationPromptVersion` v2 → v3（会话代机制见任务 1；若任务 1 同批
   实施，直接采用任务 1 的 ID 方案，避免两次连续轮转）。
5. **测试迁移**（现有断言会失败，属预期，迁移而非删除）：
   - `TestProgressEvaluationInstructionsDefineEvidenceAndReadableFeedbackRubric`
     的 fragments 断言改为针对新的 system prompt 构建函数；
   - `TestEvaluateProgressUsesDedicatedEvaluationProvenance` 里对
     `createSessionRequests[0].SystemPrompt` 的子串断言同步更新；
   - 新增断言：instructions 在两次评估间字节级一致（固定 evaluationID 也不影响）。
6. **文档**：`docs/development/progress.md` 说明 prompt 分层（会话级规则 / run 级
   任务行）与 v3 bump 会让既有项目在下一次评估时新建一个 Progress Session。

### 验收
- `cd backend && go test ./internal/agent/...` 全绿；rubric 内容一条不少（只是
  搬家）。
- 人工比对：同一项目两次评估的 StartRun payload 中 instructions 完全相同。

---

## 任务 1：会话轮转（基于任务 3 的 v3 ID）

### 目标
单个 Progress Session 的上下文规模有硬上限，超过即换新会话；连贯性由已持久化的
上次评估输出（agent 经 `progress.get` 自取，工作流第 2 步）承担，不靠旧会话记录。

### 现状
- `progressSessionRemoteID(projectID, instanceID)` = SHA1UUID of
  `"mmdash:progress:v2:<projectID>:<instanceID>"` → 每个 项目×实例 永远一个会话。
- `ensureProgressSession`（同文件）：按 `session_type='progress' AND status='active'
  AND remote_session_id == 计算值` 匹配本地行；不存在则
  `getOrCreateProgressSession`（Get → Create，冲突后重读的收养逻辑已存在，
  并发安全依赖 `agent_sessions(agent_instance_id, remote_session_id)` 唯一索引
  + `ErrConflict` 收养路径——轮转必须复用这套机制，不得另起炉灶）。
- Hermes 会话统计可从 `adapter.GetSession` 取得：`agent.Session` 含
  `MessageCount/ToolCallCount/InputTokens/OutputTokens/APICallCount`
  （`backend/internal/agent/adapter.go`）。

### 设计（按此实现，遇阻先记录再调整）
1. **ID 方案**：`progressSessionRemoteID(projectID, instanceID, generation)`，输入
   追加 `:g<generation>`；与任务 3 合并实施时版本前缀用 `v3`。
2. **generation 推导（确定性，跨 worker 一致）**：generation = 该 项目×实例 名下
   已存在的 progress 会话行数（本地 `Store.ListSessions` 过滤
   `SessionType==SessionProgress`，包含 ended）。行只增不删 → 计数单调；两个并发
   worker 得到相同 generation → 相同新 ID → 唯一索引/ErrConflict 收养兜底。
3. **触发点**：`ensureProgressSession` 找到/收养 active 会话后，调用
   `adapter.GetSession(remoteID)` 读取统计；超过任一阈值 → 轮转：
   - 阈值常量起步：`MessageCount >= 120` 或 `InputTokens+OutputTokens >= 300_000`
     （写成常量并注释依据；如需配置再走 cmd config，本任务不强制）。
   - 轮转动作：旧会话本地置 `status='ended'`（复用 Store 里既有的会话结束路径，
     没有 Store 级方法则新增一个最小方法+迁移不需要，仅 UPDATE）、远端
     `adapter.UpdateSession(oldRemoteID, UpdateSessionRequest{EndReason:
     &"rotated"})`（失败仅可观测，不阻塞新会话创建）；然后按 generation+1 走
     既有 `getOrCreateProgressSession` 创建新会话。
   - `GetSession` 失败（网络/超时）→ 放弃本轮轮转判断，沿用旧会话（宁可用旧
     会话也不因轮转检查挂掉评估）。
   - 单次评估内至多轮转一次；轮转决策只发生在 StartRun 之前。
4. **不做的事**：不迁移旧会话内容、不注入历史摘要进新会话（progress.get 已覆盖）、
   不做中途轮转、不改评估输入输出契约。

### 测试
- 超阈值 → 新建 generation+1 会话、旧会话 ended、评估正常完成（fake adapter，
  沿用 `service_test.go` 的 `agentServiceTestAdapter`，需为 `GetSession` 补统计
  字段与 `UpdateSession` 记录）。
- 阈值内 → 复用旧会话，不产生新会话。
- `GetSession` 报错 → 复用旧会话且评估成功。
- 同状态并发两次 `EvaluateProgress` → 两边落到同一个新会话（收养路径）。
- 确定性：相同会话行状态下，两次计算出的 remote ID 一致。

### 验收
- `cd backend && go test ./internal/agent/... ./internal/progress/...` 全绿；
- 完整仓库门禁 `pnpm check` 通过（注意本机需 `GOPROXY=https://goproxy.cn,direct`，
  见 `.env`）；Worker 侧无改动。

---

## 实施顺序与依赖

1. 任务 2（独立，最小风险）→ 2. 任务 3（版本 bump v2→v3）→ 3. 任务 1（generation
   叠加在 v3 之上）。任务 3 与 1 同分支实施时，把 ID 方案一次性定为
   `mmdash:progress:v3:<projectID>:<instanceID>:g<generation>`，只轮转一次。

## 全局验收清单

- [ ] `cd backend && go test ./internal/...` 全绿（含既有集成测试门控）
- [ ] `pnpm check` 全绿（Python/TS 侧无改动也须过）
- [x] `docs/development/progress.md`、`handoff.md` 已更新
- [ ] commit 主题符合 conventional commits（`feat(progress): ...` /
      `fix(progress): ...`），提交内无任何 agent 协作者元数据
- [x] contracts/OpenAPI/events 零变更；migration 零新增（除非任务 2 核实出
      revision 作用域 bug 且确需存储层修正）

## 完成记录（2026-09-07）

- [x] 任务 2：确认 evidence revision 由 Project-scoped Data Hub 投影生成；补充 event 无变化 merge、无新 Job、Force 重跑及 evidence/state revision 变化集成覆盖。
- [x] 任务 3：稳定规则迁入 Session system prompt，run instructions 缩为无 ID 的固定任务行，补充并行读取要求，并将 prompt 版本从 v2 提升至 v3。
- [x] 任务 1：按 Project × Agent × generation 生成确定性 Session ID，加入 120 消息/300,000 token 轮转、远端 best-effort 结束、统计读取失败复用和冲突收养路径。
- [x] `cd backend && go test ./internal/agent/... ./internal/progress/...` 通过。
- [x] 并发轮转用例通过，包括 `go test -race ./internal/agent -run TestEvaluateProgressConcurrentRotationAdoptsOneNewSession`。
- [x] `pnpm commit:check "feat(progress): optimize automatic evaluations"` 通过；按请求未创建 commit，工作树保持未提交。
- [x] `pnpm contracts:check`、`pnpm api:check`、Go 构建、Worker 构建、Python lint、TypeScript 测试及 Next webpack production build 通过；直接使用 Pixi Caddy 校验也通过。

### 外部环境门禁

- [ ] `cd backend && go test ./internal/...`：既有 Repo/Repo GitCLI 测试在当前主机访问仓库根目录时返回 `Access is denied`，Progress/Agent 相关测试通过。
- [ ] `pnpm check`：同一环境下 Repo 根目录权限，以及 Box Sandbox/E2B 运行时不可用导致既有测试失败；本次变更没有修改这些模块。
- [ ] commit 检查：本次未创建 commit，因此没有新增提交主题或协作者元数据可验证。

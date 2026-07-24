# Competition Test Pack

## Story

测试故事使用一套轻量但成套的“城市应急物资配送”竞赛素材，目标是模拟一支单队在比赛中的完整使用流程：

1. 上传题目
2. 梳理任务
3. 撰写模型文档
4. 做基础 AI 分析
5. 运行小规模实验
6. 回看实验记录

## Assets

### Problem Brief

- 文件名：`urban-relief-brief.txt`
- 题目主题：城市应急物资配送
- 核心目标：
  - 在 6 小时内完成 4 个社区的应急物资配送
  - 平衡总时长、迟到惩罚和车辆负载
  - 输出调度方案与敏感性分析

### Model Draft

- 文档标题：`城市应急配送模型`
- 推荐结构：
  - 摘要
  - 问题重述
  - 基本假设
  - 符号说明
  - 目标函数
  - 约束条件
  - 实验计划
- 关键公式：
  - `min Z = \\sum_i c_i x_i + \\lambda \\sum_j d_j y_j`
  - `\\sum_k x_{ik} = 1`

### Experiment Repo

- `solver_ok.py`
  - 读取环境变量参数
  - 输出 `alpha_env / beta_env / alpha_static / beta_static`
- `solver_fail.py`
  - 输出 `intentional failure`
  - 非零退出
- `solver_mismatch.py`
  - 保留静态参数，模拟“提参与执行语义错位”

### Timeline Milestones

- 题意澄清会
- 模型定稿
- 实验复核

## Observation Dimensions

- 连续性：前一步产物是否自然引出下一步
- 易懂性：当前页是否说明“我现在该做什么”
- 恢复性：错误后是否容易继续
- 结果感：历史记录和分析结果是否像“竞赛产出”而非底层文件
- 协作感：TODO 与 timeline 是否帮助推进故事

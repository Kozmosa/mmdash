# 主页题目上传功能修复设计

## 背景

Issue #2: 在主页选择 PDF 上传后，页面提示上传完成，刷新页面后仍显示"选择文件"。

## 根因分析

后端的上传接口 `POST /home/{project_id}/upload` 和列表接口 `GET /home/{project_id}/problems` 均正常工作，但前端存在两个缺失：

1. 前端从未调用列表接口获取已上传文件
2. 前端没有状态和 UI 来渲染已上传文件列表

这导致用户上传后只能看到 toast 提示，刷新页面后文件"消失"。

## 设计决策

采用**方案 C：完整文件管理**，即在修复显示问题的同时增加删除能力。

## 架构与数据流

```
用户点击上传 → frontend POST /home/{pid}/upload
                → backend 保存到 uploads/ + 写入 problem_files 表
                → frontend 成功后调用 fetchProblemFiles(pid) 刷新列表

用户点击删除 → frontend DELETE /home/{pid}/problems/{fid}
                → backend 删除本地文件 + 删除 DB 记录
                → frontend 成功后刷新列表

页面加载/切换项目 → frontend GET /home/{pid}/problems
                     → backend 返回该项目的文件列表
                     → frontend 渲染列表
```

## 后端变更

### 新增接口

**`DELETE /api/home/{project_id}/problems/{problem_id}`**

权限模型与现有接口一致：
1. 校验 project 存在 → `404 Project not found`
2. 校验当前用户是 team member → `403 Not a team member`
3. 查询 `ProblemFile` → 不存在返回 `404`
4. 删除磁盘文件（`os.remove`）
5. 删除数据库记录（`db.delete` + `db.commit`）
6. 返回 `204 No Content`

### 文件变更

- `backend/app/api/home.py` — 新增删除接口
- `backend/tests/integration/test_home.py` — 新增删除相关测试用例

## 前端变更

### 类型与状态

- 新增 `ProblemFile` 接口：`{ id: string; filename: string; file_type: string; uploaded_at: string }`
- 新增 `problemFiles` state
- 新增 `loadingProblems` state

### 数据获取

- 新增 `fetchProblemFiles(projectId)` 函数，调用 `GET /home/${projectId}/problems`
- 在 `selectedProject` 变化时，与 todos/progress 并列通过 `Promise.all` 获取
- 上传成功后调用 `fetchProblemFiles` 刷新列表
- 删除成功后调用 `fetchProblemFiles` 刷新列表

### 删除功能

- 新增 `deleteProblemFile(problemId)` 函数，调用 DELETE 接口
- 删除前通过 `confirm()` 或 toast 确认
- 每个文件项右侧显示删除按钮（Trash2 图标）

### UI 渲染

在「题目上传」Card 的 CardContent 中，上传 input 下方新增文件列表区域：

- 列表项展示：文件名 + 文件类型 Badge（PDF/文本）+ 格式化上传时间
- 空状态：显示「暂无已上传文件」提示
- 加载状态：显示 Skeleton 占位
- 文件列表仅在 `selectedProject` 存在时渲染

### 文件变更

- `frontend/app/(main)/home/page.tsx` — 新增状态、数据获取、删除逻辑和 UI

## 测试覆盖

### 后端集成测试

新增 `TestDeleteProblem` 测试类：
- `test_delete_problem_success` — 正常删除，确认文件和 DB 记录均被清除
- `test_delete_problem_not_found` — 删除不存在的 problem → 404
- `test_delete_problem_not_member` — 删除非成员项目的文件 → 403
- `test_delete_problem_project_not_found` — project 不存在 → 404

## 风险与边界

- 磁盘文件删除失败（如文件已被手动删除）：不应阻塞 DB 记录删除，但需记录或静默处理
- 并发删除：无特殊处理，依赖数据库事务
- 文件类型标识：继续使用现有逻辑（`.pdf` 后缀判断）

# DsChat (hschat) 设计规格

> 核心议题：**continue 状态机**、**可打断/可重入的流式架构**、以及**对话可切走、可刷新的原理**。

## 0. 一句话概括

DsChat 是一个**本地 agent 调试器**：一个 Go 单体进程，内嵌 Web UI（`web/` + `assets/` 经 `go:embed`），以 JSON 文件持久化对话（
`chats/*.json`），通过 MCP（tools-only 子集：stdio / streamable HTTP）与内置工具（Sandbox / AskUser / Coding）驱动 OpenAI 兼容的
LLM 流式接口。另有无头 CLI 模式（`main.go -prompt`）。

设计哲学（见 `main.go` 注释）：**essentials only**——MCP 只实现 tools、SSE 不支持 streamable resumability、JS 不是真容器；但*
*可打断、可重入**是硬约束，砍不掉。

---

## 1. 数据模型（`internal/model`）

### 1.1 Chat

```go
type Chat struct {
Title       string // 唯一标识；文件名 = sanitize(title) + ".json"
RootDir     string // 可选；空 = 用 config 默认根目录；注入 ctx 供沙盒工具使用
Messages    []Message
ContextSize int // 最近一次请求的 prompt tokens，usage 事件到内存、随下次保存落盘
}
```

### 1.2 Message

| 字段                   | 含义                                                      |
|----------------------|---------------------------------------------------------|
| `Role`               | `system` / `user` / `assistant` / `tool`                |
| `Content`            | 正文                                                      |
| `ReasoningContent`   | 思维链（DeepSeek 等）                                         |
| `ToolCalls`          | assistant 发起的工具调用（`[{id, function:{name, arguments}}]`） |
| `ToolCallID`, `Name` | tool 消息回指其来源 tool_call                                  |
| `SendToServer`       | **关键标志**：false 的消息留在历史里但不发给 LLM                         |
| `Approved`           | assistant 消息级"手动批准"标记（见 §3.4）                           |

### 1.3 结构化不变量（invariants）

`cont.ValidateChat` 定义了"合法对话"的判定，也是各错误类型的词汇表：

- **分组（group）**：一条带 tool_calls 的 assistant 消息 + 紧随其后的若干 tool 消息。
- 非末尾分组：tool 消息数 **必须等于** assistant 的 tool_calls 数（`group_size_mismatch`）。
- 每条 tool 消息的 `tool_call_id` 必须命中所在分组 assistant 的 tool_calls（否则 `orphan_tool`），且 `name` 必须一致（
  `schema_mismatch`）。
- `tool_call_id` 在分组内、以及跨 assistant 消息间均不可重复（`duplicate_id`）。
- 引用的工具必须存在（`invalid_tool_call`）。
- 末尾分组允许不完整——那正是"待续跑"状态。

---

## 2. 存储与配置（`internal/storage`）

- **`chats/<sanitized-title>.json`**：一个对话一个文件，`MarshalIndent` 全量写。删除 = 移入 `chats/recycler/`（回收站，可挽回）。列表按
  mtime 倒序。
- **`config.json`**（`model.MCPConfig`）：端口（默认 5234）、沙盒根目录列表、MCP 服务器、**工具审批表**（`approved_tools` /
  `manually_approved_tools`，格式 `"MCP名::工具名"`）、模型 provider 列表与当前选择、默认 system prompt、
  `enable_coding_tools`。
- **`coding.json`**（`model.CodingConfig`）：CLI 别名工具表（`shell_tools`）、`raw_shell`（真 shell 直通，启用后**禁用沙盒路径检查
  **）、黑名单。
- **`states.log`**：全量结构化日志（continue 引擎、stream 会话、server 请求都往里打，是本调试器的"自我调试"手段）。

---

## 3. Continue 状态机（`internal/continue/engine.go`）

这是全项目的心脏。**核心思想：对话数组本身即状态。不维护显式状态枚举，而是"看最后一条消息的 role 决定下一步做什么"。**
这让任意历史（包括被用户编辑得奇形怪状的历史）都有定义良好的续跑语义。

### 3.1 入口

```go
engine.Continue(ctx, chat, input, autoContinue, emit, interrupted)
```

- `input != ""`：先 append 一条 `user` 消息、发 `user_added` 事件、**立即落盘**（save 边界）。
- 然后进入 `doContinue` 路由，跑完为止。

### 3.2 路由（`doContinue`）

```
              ┌─────────────────────────────────────────┐
              │   看 chat.Messages 最后一条的 role       │
              └─────────────────────────────────────────┘
        ┌───────────┬──────────────┬─────────────┬──────────┐
   user/system   assistant        tool        其他/被中断/空
        │            │               │            │
   streamLLM   continueAssistant  continueTool  直接返回
        │            │               │
        └──── 完成后若 autoContinue 且末条不是 user → 递归 doContinue
```

- **`user` / `system`** → 发起 LLM 流式请求（`streamDeepSeek`）。
- **`assistant`** → 有 tool_calls 则走审批/执行；无 tool_calls 则**所有模式都自然停机**——唯一的例外是 sudo 下的**手动**
  continue（视作"接着写"继续 stream）；auto_continue 递归永远不会进入这个分支，否则每个结束块都会触发一次无中生有的续写，滚出无限多个连续结束块。手动续写若产生
  tool_calls，则回到正常 agent 循环。
- **`tool`** → 回溯到所属 assistant 分组，**断点续跑**：收集已执行的 tool_call_id，执行下一个未执行的；全部执行完则再次
  stream。
- 任何分支入口都先查 `interrupted()`，**中断是协作式的、在每个决策点生效**。

### 3.3 LLM 流式段（`streamDeepSeek`）

1. 先 append 一条**空 assistant 占位消息**，后续 delta 直接累加进这条消息——内存中的 chat 永远是"当前真实状态"。
2. 事件：`delta`（正文）/ `reasoning_delta` / `tool_call`（按 id 聚合，重复 id 覆盖）/ `usage`（只更新内存 `ContextSize`，落盘留给下个
   save 边界）/ `done`。
3. 结束处理：
    - **被中断**：保留已生成的部分消息，发 `assistant_done`，**不报错**。
    - **真实错误**：**回滚**掉占位 assistant（`messages[:assistantIdx]`），发 `error{deepseek_error}`。
    - **空响应**（无 content/reasoning/tool_calls）：同样移除占位，避免留下空消息。
4. 正常结束 → `assistant_done` → **save 落盘**。

### 3.4 工具执行与审批（`continueAssistant` / `continueTool`）

审批是**两级**的（`mcp.Manager.IsToolApproved` 返回 `(approved, manuallyApproved)`）：

- `approved`：自动执行。
- `manually_approved`：**默认需要人工介入**——assistant 消息停机，等用户在前端点"批准"（`PUT .../approve` 翻转
  `Message.Approved`），再点继续。消息级 `Approved=true` 视为整组放行。
- 未批准且无 manual：报 `unapproved_state` 错误（理论不可达，防御用）。
- CLI 无头模式下 `ApproveAll=true`，manual 升级为自动批准。

特殊工具 **`ask_user`**：assistant 调用它时**直接停机返回**，不执行、不计入待审批。由前端弹窗收集答案，以一条 `tool` 消息（
`name=ask_user`）的形式 `POST` 插入对话末尾，然后再次 continue。readonly 模式下这是**唯一**被允许的插入（
`isReadonlyAskUserInsert` 严格校验：必须插在末尾、id 命中末尾 assistant 的 ask_user 调用、未重复回答）。`ask_user` 永远不能被设为
approved。

**无效 tool_call 处理**（`findInvalidToolCalls`：空 id/空名/工具不存在/组内 id 重复）：

- 默认：发 `error{invalid_tool_calls, ids}` 停机，前端把对应 tool_call 高亮 5 秒。
- `ContinueOnInvalid=true`（CLI 模式）：给每个无效调用补一条 `"tool not found"` 的 tool 消息，**让模型自己看着办**，继续
  stream。
- 参数校验（`validateArgs`，对照工具 inputSchema 查 JSON 合法性/required/类型/未知字段）同理：默认报错停机；
  `ContinueOnInvalid` 下把错误文本当工具结果喂回去。

### 3.5 消息编辑语义（writable / sudo 模式）

`DeleteMessage` / `EditMessage` / `InsertMessage` 都带 `mode` 参数：

- **sudo**：纯数组操作，不级联、不校验。出了事 `GET /api/validate/{title}` 自己看。
- **writable**：维护结构不变量——
    - 删 assistant：级联删其 tool 分组；若后继也是 assistant，**思维链拼接**给后继（DeepSeek 要求 assistant 带
      reasoning_content）。
    - 删 tool：从来源 assistant 摘除对应 tool_call；若因此清空 tool_calls，整条 assistant 也按上法删除。
    - 编辑 assistant：新增的 tool_call 自动补一条占位 tool 消息（`content="error"`），保持分组完整。
    - 插入：tool 消息不能插组首/不能插非 tool 消息之前；id 在组内唯一；id 不在 assistant 的 tool_calls 里则自动补登。
- **readonly**：只允许删**最后一条**消息（"撤销一步"）+ 上述 ask_user 回答插入。

### 3.6 auto_continue

勾选后，每段 stream / 工具执行结束，只要末条消息不是 user 且未被中断，就递归 `doContinue`（递归入口带 `auto=true`
标记）——形成"模型→工具→模型→…"的 agent 循环，直到模型给出不带 tool_calls 的最终回答。这个回答在**所有模式（含 sudo）下都是停机点
**：sudo 的"接着写"是手动逃生舱，auto 递归不进去。

---

## 4. 可打断、可重入的流式层（`internal/engine/stream.go`）

状态机只管"怎么跑"，这一层管"**谁在跑、谁在看**"。这是"可切走、可刷新"的服务端基石。

### 4.1 Session：每对话一份运行态

```go
type Session struct {
running, interrupt bool
gen       int64 // 运行世代号：每次 StartInference +1
events    []ContinueEvent // 本次 run 的事件日志（仅内存）
savedPos  int             // 事件日志中已落盘的前缀长度
cancel    context.CancelFunc
notify    chan struct{}      // 每次状态变化 close+换新，唤醒所有等待者
}
```

**核心不变量：对话的实时状态 = 磁盘文件 + events[savedPos:]**。

- `save()` 只在**消息边界**执行（user_added 后、tool_result 后、assistant_done 后、run 结束时），保存成功才把 `savedPos`
  推进到当时的事件数。因为 save 跑在推理 goroutine 上，事件序列在 save 中途不会变，所以 `savedPos` 精确对齐。
- **error 事件永不落盘**——这是刻意的，由订阅协议补偿（见 §4.3）。
- 每个 Session 一把锁；全局 `StreamEngine` 一把锁；锁序固定 `e.mu → sess.mu`，状态广播在释放会话锁后进行。

### 4.2 中断与互斥

- 同一对话**同时只有一个 run**：`StartInference` 在锁内检查 `running`，忙则 409。
- `RequestInterrupt`：置 `interrupt` 标志 + `cancel()` ctx。HTTP 请求层立刻断流，状态机在各决策点协作式退出。前端**不乐观切换按钮
  **——有些工具（CLI）可能忽略中断，SSE 的 `idle` 才是权威停机信号。
- 会话被删/改名时 `DropSession`（仅当不在跑）。删除、改名、换 rootdir、编辑/删除/插入消息、approve 等一切写操作都以"非推理中"
  为前提（409）。

### 4.3 重入订阅协议（`GET /api/chat/stream?title=X`，SSE）

任意客户端（新开的页、刷新的页、第二个标签页）连上后：

1. **`sync` 事件** `{gen, saved_pos, running, errors?}`——建立基线。`errors` 补发本次 run 的 error 事件：因为它们不落盘，
   `saved_pos` 可能已经越过它们（比如瞬间失败的 run），不重发的话订阅者永远不知道失败原因。
2. **回放** `events[saved_pos:]` 中所有未持久化事件。
3. **实时**事件流。
4. **`idle`** `{gen}`——run 结束（或本来就在闲置，立即发一次；每个 gen 恰好一次）。

新一轮 run（gen 变化）时序列从第 1 步重来。`EventSource` 断线自动重连，服务端每次重连都重发 `sync`——**错过的一切自愈**。

前端渲染不变量（`web/js/continue.js` 头部注释原文）：

> history DOM 反映磁盘（saved_pos 之前）；streaming 层（`.stream-live` 元素）从事件日志的 saved_pos 起重建。任何 history
> 重载都会重定基线，只回放 seq ≥ saved_pos 的事件——**没有任何东西会被渲染两次，也没有消息会被劈成两半**。

配套机制：

- 相邻 delta 合并（replay 省钱）；`assistant_done` 虽无 DOM 也记录，作为屏障防止两条 assistant 的 delta 跨保存边界被误合并。
- error 事件**不参与 seq 对齐**，任何时候到达都直接渲染（toast + 消息流里的 error div + tool_call 高亮）。

### 4.4 全局状态推送（`GET /api/chats/status`，SSE）

`event: status` 携带**全量** running 集合快照，任何对话 running 变化即推。用途：

- 侧栏**所有**对话的"● 生成中"标记（单对话 stream 只覆盖当前打开的，管不了后台）；
- 兼作**存活探针**：重连期间显示"后端离线"标记（1.5s 宽限防抖）；
- 重连成功顺手刷新全局 mode（后端重启会回 readonly）。

---

## 5. 前端：切走与刷新的原理（`web/js/`）

刷新/切走不丢东西，靠三件事叠加：

1. **服务端权威**：磁盘 + 会话事件日志（§4）。前端不持有任何"唯一的"运行状态，按钮文字、运行标记全部以 SSE 为准。
2. **重定基线渲染**：
    - 切对话（`ChatList.select`）= 只换 SSE 订阅（`ContinueModule.switchChat`），**后台跑的对话继续跑**。
    - 收到 `sync` → 清空 streaming 层 → `ChatList.loadMessages()` 拉历史（带 `saved_pos`）→ `onHistoryLoaded(savedPos)`
      冲刷缓冲事件，丢弃 seq < savedPos 的（已在磁盘上、已被 history 渲染），回放其余的。
    - `idle` → 磁盘成为唯一权威 → 重拉 history 替换 streaming 层。
    - history 未渲染完时到达的事件进 `pending` 缓冲，错误进 `pendingErrors`（等 tool-call DOM 存在后再高亮）。
3. **标签页隔离**：当前对话存 `sessionStorage`（多标签页各看各的），旧版 `localStorage` key 只做一次性迁移兜底。

其余前端模块：`messages.js`（历史渲染 + 导出 HTML）、`editor.js`（消息编辑 UI）、`validate.js`（调 `/api/validate`）、`tools.js`
（工具审批面板）、`status.js`（§4.4）、`app.js`（mode 切换、偏好、root dir 选择、provider/model 切换、**noob mode**——readonly
下隐藏技术细节、跑起来禁用中断）。

---

## 6. 模式与权限

| 模式                  | 对话编辑              | 工具执行                                                          |
|---------------------|-------------------|---------------------------------------------------------------|
| `readonly`（默认，重启回落） | 仅删末条 + 答 ask_user | 照常（审批体系独立于模式）                                                 |
| `writable`          | 结构化编辑（§3.5）       | 同上                                                            |
| `sudo`              | 裸数组操作，无校验         | assistant 无 tool_calls 时，手动 continue 可"接着写"；auto_continue 不触发 |

审批三态（每工具，`config.json` 持久化）：`approved`（自动）/ `manually_approved`（逐次人工）/ `unapproved`（对模型不可见）。**只有
approved + manual 的工具会进 `GetAllowedTools()` 发给模型**。`reconcileTools` 在加载时清理：ask_user 不可 approved、manual
与 approved 互斥（manual 优先）、已消失工具若其 MCP 仍连接则除名。

---

## 7. 工具系统（`internal/mcp` + `internal/builtin`）

统一抽象：`builtin.Provider` ↔ `mcp.Client`，经 `AdaptClient` 对齐。工具全名 `"MCP名::工具名"`；**模型只见裸名**，审批匹配对裸名做
`::tool` 后缀匹配。

- **外部 MCP**：`stdio`（起子进程）/ `streamable`（HTTP）。tools-only。
- **Sandbox**（内置）：
  `tree / search_name / search_content_plaintext / search_content_advanced / read_content / replace_content / create_dir / create_file / rm / move`
  。全部经 `getSafePath` 限制在 root dir 内；删/覆盖走回收站（`trash`）；>1MB 拒读；GB18030 解码 + BOM 剥离 + 换行归一；
  `ext_blacklist` 挡二进制。
- **AskUser**（内置）：`ask_user`，后端 CallTool 永远报错——它必须由前端处理（§3.4）。
- **Coding**（内置，`enable_coding_tools` 开关）：`coding.json` 定义的 shell 别名工具 + 可选 `raw_shell` 真
  shell（启用即关闭沙盒路径检查）。

---

## 8. LLM 客户端（`internal/llm`）

- OpenAI 兼容 chat/completions 流式；`thinking: enabled`、`reasoning_effort: high`、`max_new_tokens: 128K`、`include_usage`。
- 429 立即重试 ≤5 次。
- **无总超时**：`http.Client.Timeout` 会覆盖流式 body 的读取，长生成（长 CoT 模型一轮几分钟起步）会被中途掐断。只保留
  `ResponseHeaderTimeout`；流的中途存活交给 ctx（用户中断）。
- **截断即错误**：`parseSSE` 检查 `scanner.Err()`，连接断开/超长行（>4MB）一律返回 error 走引擎的回滚 + error
  事件路径——绝不把半截流当成正常 `done`（否则截断的回答会伪装成一个"结束块"落盘）。
- 只发 `SendToServer=true` 的消息；tool 消息即使空 content 也保留 content 字段。
- **DeepSeek 特化**：assistant 消息补空 `reasoning_content: " "`（只改请求副本，不落盘——别的 provider 不喜欢这字段）。
- **system prompt 偷换**：首条 system 恰为默认 "You are a helpful assistant." 时，若存在 `system.txt` 则静默替换（进程级缓存）。

---

## 9. HTTP API 一览（`internal/server`）

| 路由                                                     | 用途                                                   |
|--------------------------------------------------------|------------------------------------------------------|
| `GET/PUT /api/mode`                                    | 全局模式（readonly/writable/sudo）                         |
| `GET/PUT /api/config`                                  | root_dirs / provider / model                         |
| `GET /api/chats` · `POST /api/chats`                   | 列表（带 running 标记）· 新建（可带 root_dir，注入 default_prompt）  |
| `GET/DELETE /api/chats/{title}`                        | 读（带 `saved_pos`/`running`，与会话锁一致快照）· 删（→回收站，推理中 409） |
| `POST .../dupe` · `PUT .../rename` · `PUT .../rootdir` | 复制 · 改名 · 换根目录（均拒推理中）                                |
| `GET /api/chats/status`                                | 全局 running SSE（§4.4）                                 |
| `GET /api/validate/{title}`                            | 结构校验（§1.3）                                           |
| `POST /api/chat/continue`                              | 启动 run `{title, input, auto_continue}`（忙则 409）       |
| `GET /api/chat/stream?title=`                          | 重入订阅 SSE（§4.3）                                       |
| `POST /api/chat/interrupt`                             | 协作式中断 `{title}`                                      |
| `GET/PUT /api/mcp/tools` · `POST /api/mcp/reload`      | 工具审批表 · 重连所有 MCP                                     |
| `DELETE/PUT/POST /api/chat/{title}/message/{index}`    | 删 / 改 / 插（`POST` 在 index 处插入）                        |
| `PUT .../message/{index}/approve`                      | 翻转 assistant 消息的 Approved                            |

---

## 10. CLI 无头模式（`internal/cli`）

`hschat -prompt "..." [-title T]`：跳过 HTTP，直接 new 引擎跑一轮 `auto_continue=true` 的 Continue。差异：`SkipAskUser`（不注册
ask_user）、`ApproveAll`（manual→自动）、`ContinueOnInvalid`（无效调用喂回模型）、`interrupted` 恒 false。对话照样落在 `chats/`
里，可回 Web UI 查看/续跑。

---

## 11. 关键设计取舍备忘

1. **对话即状态**：没有显式状态机枚举，靠末条消息 role 路由——任意手工编辑后的历史都有定义良好的"继续"语义。
2. **savedPos 双轨**：磁盘 + 内存事件日志的切分点，是可刷新/可切走/多标签的全部秘密；save 只在消息边界发生，所以事件要么全存要么全不存。
3. **error 不落盘 + sync 补发**：磁盘历史保持"干净可继续"，失败原因走带外通道，绝不丢失。
4. **中断是协作式 + SSE 权威**：前端不猜，等 `idle`。
5. **编辑保持不变量**：writable 模式的所有级联/补登都是为了让"编辑后的历史仍然能 continue"；sudo 则把责任交还给人。
6. **复杂性守恒**（`main.go` 碎碎念）：这些机制中的每一个，都是"可打断可重入"这一硬约束逼出来的，砍不掉。

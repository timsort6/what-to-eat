# food-agent

一个基于 **Eino (Agent)** + **RAG (PostgreSQL/pgvector)** + **MCP** 的智能美食推荐系统。

目录结构遵循 Go 项目常用的 Clean Architecture 风格：
- `cmd/`：入口（server + tools）
- `internal/`：业务代码（handler/service/repository/agent 等）
- `pkg/`：可复用组件（config/db/cache/logger/amap sdk 等）

## 架构说明与 Eino ReAct Agent

本项目采用 Eino v0.7.13 提供的 ReAct Agent 机制，将用户多轮餐饮需求与高德 MCP 地点搜索工具编排在一起。核心设计原则：

1. **分阶段编排**：搜索阶段为服务端手写循环（`internal/agent/runner_stream.go`），模型每轮可在白名单内选择周边搜索或文本搜索，每轮最多 `max_tool_calls` 个工具调用（默认 6）；超出的批次整批不执行任何外部工具，而是为每个 tool_call_id 回一条中文错误结果（“本轮请求了N个工具调用，超过每轮上限M个，未执行……”）让模型合并后重试，该轮照常占用一次模型调用与步数预算，轮次用尽则照常进入决策与正文；未知工具名、空/重复调用 ID 仍整体拒绝；仅决策阶段使用 Eino ReAct Agent + `ToolReturnDirectly`（`internal/agent/runner_decision.go`，由 `finalize_recommendation` 直接返回结构化决策）；最后一次模型调用流式生成正文，并支持结构化地点卡片展示。
2. **服务端白名单约束**：高德搜索工具在服务端严格限定为 `amap.poi2_around` 与 `amap.poi2_text` 两个子集。配置中只允许选择白名单内的工具，拒绝未声明的外部工具调用。
3. **上下文可信注入**：用户经纬度与位置权限直接从已认证请求上下文中提取，禁止模型伪造用户身份或覆盖位置。
4. **多维度执行预算**：包含逻辑步骤数上限、模型调用次数上限、工具调用次数上限以及请求级超时控制，杜绝死循环或资源耗尽。
5. **SSE 生命周期保障**：流式推送过程中，每次交互成功仅发送单次 `done` 事件。若请求断开、取消或报错，绝不发送包含成功标志的 `done`。

## 配置说明

主配置文件支持在 `agent` 节点下配置 Agent 运行策略（示例参考 `configs/config.yaml_example`）。注意：仓库中的 `configs/config.yaml_example` 预算仍为旧值 `max_steps: 8` / `max_model_calls: 4`，照抄会显式降为最多 2 轮搜索（外部地图上限仍按该文件的 `max_tool_calls: 3` 计为 2×3=6 次）；默认值以代码中的 10/5 为准（`internal/options/options.go` 中 `NewServerRunOptions`），下方片段按代码默认值书写：

```yaml
agent:
  prompt_file: prompts/food-agent.md
  allowed_tools:
    - amap.poi2_around
    - amap.poi2_text
  max_steps: 10
  max_model_calls: 5
  max_tool_calls: 6
  timeout_seconds: 90
```

配置字段说明（内部默认值见 `internal/options/options.go` 中 `NewServerRunOptions`）：
- `prompt_file`：提示词文件路径。相对路径将基于当前加载的主配置文件所在目录自动解析（如配置为 `prompts/food-agent.md`）。启动时会严格检查文件是否存在且非空。
- `allowed_tools`：允许 Agent 使用的工具列表，默认仅开放 `amap.poi2_around` 与 `amap.poi2_text`。配置未知工具将在服务初始化时直接拒绝。内部决策工具 `finalize_recommendation` 由服务端注册，不属于此白名单，也不暴露为外部 MCP 工具。
- `max_steps`：单请求逻辑步骤上限，默认 10。
- `max_model_calls`：单请求模型调用上限，默认 5。
- `max_tool_calls`：每个搜索轮次允许的外部地图工具调用次数，默认 6，直接作为每轮上限（`internal/agent/runner_budget.go` 中 `newCallBudget`，已去掉原 `min(3, …)` 硬限）；须 ≥1 且不大于 `max_steps`，否则启动校验失败。超量批次的处理见上文“分阶段编排”，并记一条 INFO 日志 `agent.tool_batch_rejected`（`round`、`requested`、`limit`、`tools` 工具名列表，不含参数），被拒批次不计入 `agent.run` 的 `tool_attempted`。`agent.run` 的 `calls` 中工具记录的 `tool` 为配置名（`amap.poi2_around`/`amap.poi2_text`，由模型侧名 `amap_poi2_*` 经 `runner_stream.go` 中 `toolMap` 映射回配置名）；`agent.tool_batch_rejected` 的 `tools` 是模型请求的模型侧名。提示词中的上限由配置自动注入：`configs/prompts/food-agent.md` 写 `{{max_tool_calls}}`，`internal/agent/runner.go` 中 `NewRunner` 将其替换为生效的 `max_tool_calls`（与每轮预算同一个值）；不含占位符的旧提示词原样使用。
- `timeout_seconds`：单请求共享超时（秒），默认 90（原为 30；2026-10-24 真实推理模型实测：决策约 18–21 秒，端到端约 35 秒），覆盖搜索、决策与正文全部阶段。

配置覆盖优先级遵循：命令行 Flag > 环境变量 > 配置文件 > 内部默认值。

### 模型配置（`llm` 节点）
- `model`：检索与最终回答使用的模型；未配置 `decision_model` 时决策阶段也用它。
- `decision_model`（可选）：只用于决策阶段（`finalize_recommendation`）的模型，与 `model` 共用 `base_url`/`api_key`；为空则所有阶段都用 `model`。也可用 flag `--llm.decision_model` 或环境变量 `FOOD_AGENT_AI_DECISION_MODEL`。
- `timeout_seconds`：单次模型 HTTP 请求超时（含流式读取），透传给 eino-ext openai 的 `ChatModelConfig.Timeout`（`pkg/client/llm/client.go` 中 `chatModelConfig`）。实际取 max(`llm.timeout_seconds`, `agent.timeout_seconds`)（`internal/options/options.go` 中 `EffectiveLLM`），默认即 90，避免单次请求先于 agent 总预算超时。
- `max_retries`：当前**不生效**。eino-ext openai v0.1.7 的 `ChatModelConfig` 没有重试字段，服务端也不自动重试；字段仅为配置兼容保留。
- 模型 ID 以服务商 `GET {base_url}/models` 返回为准。

单模型（所有阶段同一个快速模型）：

```yaml
llm:
  base_url: <OpenAI 兼容地址>
  api_key: <密钥>
  model: deepseek-flash
  timeout_seconds: 60
```

分阶段（检索/回答用 chat，决策用 reasoner）：

```yaml
llm:
  base_url: <OpenAI 兼容地址>
  api_key: <密钥>
  model: deepseek-chat
  decision_model: deepseek-reasoner
  timeout_seconds: 60
```

### 调用预算（当前实现）
- 搜索 S 轮占 2S 个逻辑步骤；决策模型、决策工具、direct_return 共 3 个逻辑步骤；正文模型 1 个逻辑步骤。合计 2S+4 步、S+2 次模型调用。
- `S = max(0, min(3, max_model_calls-2, floor((max_steps-4)/2)))`（`internal/agent/runner.go` 中 `searchRounds`）。
- 默认 5/10、`max_tool_calls=6`：3 轮搜索 + 1 次决策 + 1 次正文；外部地图最多 3×6=18 次（每轮 6 个），内部决策工具 1 次单独计数，不占外部 18 次。超量被拒的轮次同样占用 1 次模型调用与 2 个逻辑步骤、0 次外部地图。
- 旧配置 4/8：最多 2 轮搜索、外部地图最多 2×`max_tool_calls` 次（默认 6 时 12 次；`config.yaml_example` 的 3 时 6 次）。低配置不会向上补额；`max_model_calls<2` 或 `max_steps<4` 时无法容纳决策与正文，返回受控预算错误，不发成功 `done`。
- 所有阶段共享同一预算与 `agent.timeout_seconds`（默认 90 秒）上下文；无自动重试。预算是上限，模型可提前决策。

## HTTP 接口（本轮新增/变更）

### `GET /api/v1/locations/search`（需认证）
- 参数仅允许 `query`（去空白后 1–80 字符）与 `city`（选填，≤100 字符），各出现一次；其他参数、重复参数、空查询或超限返回 400，不静默截断。
- 固定调用 `amap.poi2_text` 第一页最多 10 条，不限定餐饮分类；无有效候选时仅一次 `amap.geocode` 回退。每个 HTTP 请求上游最多 2 次，共享 5 秒超时（`internal/apis/locations/service.go` 中 `Service.Search`）。
- 只返回白名单 `{id,name,address,location,city}`，坐标须有限且在经纬度范围内。无结果为 200 空列表；上游失败 502、超时 504、取消 408，不冒充无结果。
- 不接受工具名、URL 或任意 MCP 参数；复用应用现有 MCP 连接与凭证，不扩聊天工具白名单。

### `POST /api/v1/addresses`、`PUT /api/v1/addresses/:id`（既有接口）
- 前端“保存为地址”复用 POST 接口。新建与更新均做长度校验，字段超长返回 400：`label` ≤64、`name` ≤128、`address`/`formatted_address` ≤255 字符（`internal/apis/addresses/service.go` 中 `validateLengths`、`handler.go` 中 `mapServiceErr`）。

### 聊天请求新增字段
- `preferences`（可选）：`target_count` 1–10（默认 3）、`radius_meters` 1–50000（默认 3000）、`meal_style` = `any|quick|regular|leisure`（默认 `any`）、`max_price_cny` 1–10000 或 null（默认不限）、`allow_unknown_price`（默认 true）、`food_requirement` ≤100 字符或 null。拒绝未知/重复字段与非法 null。
- `selection`（可选）：`{option_id, previous_preferences, changes}`；必须同时提交完整 `preferences`，`changes` 只能改上述六项，服务端校验差异后重新检索，不信任旧候选 ID。
- 旧字段兼容：无 `preferences` 时，`radius`（默认 5000）/`limit`（默认 10）映射为半径/数量；越界（radius 1–50000、limit 1–10）或与 `preferences` 同时出现且矛盾返回 400（`internal/apis/agentchat/types.go` 中 `ChatRequest.NormalizePreferences`）。

### SSE 事件
- 顺序：`decision`（一次，非终结）→ 若干 `delta`（真实流式 Markdown）→ `done`（一次，正文成功结束后）。
- `done` 新增 `status`（`ready|needs_choice`）、`preferences`、`decision`，并保留 `answer_text`、`places`、`places_count`、`location`、`address_id` 旧字段；旧客户端可忽略 `decision` 事件。
- 流开始后的失败为 **HTTP 200 + `event: error`**，不发 `done`；监控不能只看 HTTP 状态码，需统计 `error` 事件。取消/断开时既不发 `done` 也不发 `error`。
- `needs_choice` 时 `places` 为空，协商选项在 `decision` 中，最多 3 个。

### 决策中的品类理解与冲突澄清
- 决策可携带两个有界可选字段：`food_interpretation`（`requirement` ≤100 字符、`quote`、`ambiguous`）与 `conflict`（`field`、`proposed`、`quote`），定义见 `internal/agent/decision_tool.go`、`recommendation_types.go`。
- 两者的 `quote` 必须逐字摘自最新一条用户消息；服务端在 `recommendation_policy.go` 的 `interpret` 中校验：仅当请求 `food_requirement` 为空时才可采纳品类理解，采纳后的偏好须通过既有校验；`ambiguous=true` 或存在 `conflict` 时必须返回带选项的 `needs_choice`，且 `conflict.proposed` 须与某个选项的 `changes` 一致并确实改变偏好。任一不满足即判为无效决策，不发成功 `done`。
- 补充规则：
  - 采纳的 `requirement` 须是 `quote` 的子串（规范化后）；且在最新用户原话中首次出现处，同一子句内紧邻的前 4 个字里不得有“不要/不吃/不想/没有/除了/别吃/别要”等否定词（“有没有”视为疑问，不算否定；不认单字“别”，以免误中“特别”），否则不得按非含糊采纳（应返回 `ambiguous=true` 的澄清型 `needs_choice`，否则判为无效决策）。这是固定词表的简单规则，不识别双重否定或后置否定。
  - `radius_meters`、`target_count`、`max_price_cny`、`food_requirement` 冲突的 `quote` 必须含 `proposed` 的取值（半径须带距离单位并按米换算相等；只认阿拉伯数字）。
  - 末条用户消息为空白时，`agentchat` 在开流前直接返回 400，不调用模型与地图。
- 这些校验只约束结构与引用来源，不保证模型对品类或冲突的判断正确。

### 三态评估以服务端为准（2026-10-24）
- `finalize_recommendation` 的契约不再包含 `assessments`；模型只给选择与意图。旧模型若仍提交 `assessments`，服务端照常严格解码（未知字段仍拒绝）但忽略其内容，整体替换为服务端按（采纳品类后的）条件计算的三态（`recommendation_policy.go` 中 `normalizeDecision`）。
- `selected_ids`：不在本次候选池或重复（含 POI ID 与候选键互为别名）、数量超过 `target_count` 仍拒绝；服务端判为不可选的候选被剔除；剔除后为空则回退为服务端按直线距离由近到远选择，仍为空则转为 `needs_choice`（`preferences_conflict`/`no_candidates`）；不足目标数时补 `insufficient_count`。
- `reason_codes`（含方案内的）只保留服务端可证实的值。quote/否定/取值校验、`options.changes` 白名单与“无变化”检查、`observed_ids` 必须可选、64KB 上限保持不变。形态与改前一致：缺 `selected_ids`/`reason_codes` 键、枚举外原因码、`needs_choice` 带选中项均拒绝；唯一放宽是 `ready` 配空 `selected_ids`，按上述回退处理。

### 未知商业字段与模型估计
- 餐饮 text/around 由服务端固定 `show_fields=business`，不按候选追加详情查询。价格：币种与口径明确为人民币人均时按已核实比较（证据 `business.cost/currency/cost_basis`）；高德 `business.cost`（或旧形状顶层 `cost`）缺币种或口径时，按“高德参考人均（人民币）”参与 `max_price_cny` 比较，证据只有 `business.cost`，前端卡片显示“参考人均 ¥X”，不称为已核实；明确的其他币种/口径、非数字或空 cost 仍为“未知”，`done.places` 不输出未知价格的 cost；未知不自动淘汰（`allow_unknown_price=false` 除外），也不得称为满足。
- 品类（`food_tools_parse.go` 中 `assessFood`，小规则表，未列出的组合一律未知）：品类拆成“地域前缀（川渝/黔/湘/粤/潮/滇 及其写法；四川/川/蜀/重庆/渝 同属川渝一组）+核心菜品”。type 各级或 tag 各项（去空白、全角转半角）含完整品类，或含核心菜品且地域一致 → 满足（如“川味火锅”“重庆火锅”满足“四川火锅”，“川味火锅/四川火锅”满足“重庆火锅”）；含核心菜品或同类替代品（核心为火锅时：冒菜/麻辣烫/串串/钵钵鸡/酸汤锅）但地域明确不同 → 不符（如 tag“贵州火锅”）；只有替代品而任何项都不含核心菜品 → 不符（如 type 四川菜(川菜)+tag 冒菜）；type 某一级恰为另一菜品族（火锅/冒菜/烤肉/寿司/披萨/面馆）→ 不符；既满足又冲突为未知。高德菜系末级（川菜/北京菜/日本料理/意式等）对菜品要求为未知；tag 中的普通菜名（烤肉、毛肚等）既不满足也不冲突。名称不参与判断。营业时间不当作用餐/出餐耗时。正文中的概括或估计须邻近标注“模型估计/待核实”，不得覆盖来源事实。

### 高德 MCP 的两种用途
- IDE（OpenCode）中连接的官方高德 MCP 仅用于开发阶段核对接口能力与少量公开样本，不是应用连接，也未读取或复制其凭证。
- 应用运行时继续使用本仓库既有 MCP 适配器（`internal/mcp`、`pkg/mcpserver/amap`）与原有配置，未切换远端 MCP 或 provider。

## 前端客户端配合

前端项目位于独立代码库 `D:\github\food-agent-client`（Vue 3 + TypeScript + Vite）。
- **多轮会话**：仅在后端返回完整且有效的成功 `done` 事件后，前端才会将该轮对话存入历史记录，并在下一轮请求中以 user/assistant 消息序列递交。
- **取消与流丢弃**：当用户在推荐过程中点击取消或重置时，前端通过 AbortController 中断请求并递增代际标识，彻底丢弃未完成流的延迟回调，防止污染新会话。

## 验证与已知状态

### 本轮（地址协商与 Markdown，2026-10-24，离线）
- 全部为离线夹具/httptest 与本地真实浏览器，**不是**线上质量证据。回归结果来自 task18 回执 `task18-regression-20261024/task-18-final-checks.json`（Go 1.25.5，`GOTOOLCHAIN=local`、`GOPROXY=off`；前端 Node v22.20.0）：
  - `go test -race -shuffle=on -count=1 -timeout=180s -mod=readonly ./...`：退出 0；`go build -mod=readonly ./...`：退出 0。
  - 相关 7 包 `go vet`（agent、agentchat、addresses、locations、options、pkg/config、mcp）：退出 0。
  - 全仓 `go vet -mod=readonly ./...`：**退出 1**，唯一报告为既有的 `pkg/utils/ops.go:311` IPv6 地址格式警告（与本轮无关，未修改）；因此不是“全检查绿”。
  - `gopls check`（CLI v0.24.0，task18 回执中 56 个新增/修改 Go 文件）：退出 0；未验证 LSP/MCP 连接。
  - 前端 `npm test`、`vue-tsc --noEmit`、`vite build`、带浏览器的 `npm test`、`test:markdown-browser`、`test:acceptance`、`test:acceptance-matrix`（`TASK14_SKIP_PUBLIC=1`）：均退出 0。
- 未验证：真实付费模型与真实高德查询未运行；高德公开 URI 仅确认跳转返回 200，页面未渲染；真机、原生 200% 缩放与读屏未验证；离线夹具 ≠ 线上模型/地图质量。
- 部署状态未更新：本轮未部署、未重启任何服务、未修改真实配置。

### 历史记录（2026-10-22，旧预算 8/4 版本，仅存档）
以下为上一阶段记录，其中“模型调用上限 4 / 步数 8 / 收尾编号 F”描述的是旧实现，当前默认与公式以上文“调用预算”为准；PID 与部署信息不代表当前状态。

- **前端验证**（`D:\github\food-agent-client`）：
  - 运行 `npm test`：16 项单元测试全部通过（当前阶段重新运行通过）。
  - 运行 `npm run build`：生产环境打包顺利完成（当前阶段重新运行通过）。
- **Go 后端验证**（Go 1.25.5 工具链环境验证）：
  - 环境配置：Go 1.25.5，配合 `GOTOOLCHAIN=local`、`GOPROXY=off`、`GOSUMDB=off` 及隔离 GOCACHE。
  - `go test -race -shuffle=on -count=1 -timeout=90s -mod=readonly ./...`：全仓竞态与乱序测试全部通过（无跳过）。
  - 重点 5 个包运行 `go vet -mod=readonly ./internal/agent ./internal/apis/agentchat ./internal/options ./pkg/config ./internal/mcp`：全部通过。
  - 全仓执行 `go vet -mod=readonly ./...`：报告已知且与本次无关的预存警告（`pkg/utils/ops.go:311` 中 `fmt.Sprintf("%s:%d",ip,port)` 传给 `net.DialTimeout`），本次改动范围外的代码保持原样，不隐瞒此警告。
  - CLI `gopls check` 在 6 个改动的 Go 文件上退出码为 0（明确代表 gopls 命令行工具检查，LSP MCP 连接未作验证）。
  - `go build -mod=readonly -o C:/Users/tian/AppData/Local/Temp/opencode/budget-final-qa-20261022/final-sol-03.exe ./cmd/server`：成功生成新二进制文件，SHA256 为 `341AA4C8F2F6B4A13F85CA70038011F42BBE75DB4EBBF17AD8B9C80E47618F03`。
  - **调用预算与终轮保留机制说明**（旧实现的行号引用已失效，代码已重构；当前实现见上文“调用预算”与 `internal/agent/runner_budget.go`、`runner_stream.go`）：
    单请求模型调用上限为 4 次；收尾模型调用编号为 F = min(MaxModelCalls, ceil(MaxSteps/2))（即单次交互允许的最大模型调用总次数）；此前最多允许 F-1 轮搜索；在当前配置（8 步、4 次模型调用、每轮 3 次工具）下，收尾模型调用编号为 4，此前最多进行 3 轮搜索模型调用，第 4 轮保留用于最终回答且严格禁止调用工具；每轮搜索工具配额在模型准入时重置（而非模型成功后重置）；单请求内实际准入的工具执行总数上限为 9 次（<= 9），而非 12 次或单请求 3 次；禁止工具调用的最终回答若失败或为空，直接返回错误，不伪造 EOF 或进行隐式重试。
- **SSE 超时配置**：
  - `cmd/server/app/console_server.go` 中 `initHTTPServer` 已配置 `ReadTimeout: 30s` 与 `WriteTimeout: 0`，保障长连接流式 SSE 数据顺利推送而不被服务端写超时截断。
- **运行时已知状态**（更新至 2026-10-22）：
  - 数据库连接正常。新后端服务已部署为 PID 20344（运行临时可执行文件 `C:/Users/tian/AppData/Local/Temp/opencode/budget-final-qa-20261022/final-sol-03.exe`，监听 `http://127.0.0.1:8080`），替换了此前的 PID 31400 及其 MCP 子进程；前端 PID 30164 保持运行（监听 `http://127.0.0.1:5173`）。
  - 生效配置保持不变：最大步数 8，模型调用上限 4，每模型轮次工具 3，超时 30 秒。
  - 5 个健康检查端点（`/api/v1/ping`、`/api/v1/healthz`、`/api/v1/readyz` 及前端代理端口等）在测试前后持续为 HTTP 200。未修改任何真实配置、供应商或预算覆盖项。
  - 真实端到端 QA（3 次付费请求，0 次重试，3 次网络请求）全部通过（PASS）：
    - Case 1（首轮早餐粥店搜索）：HTTP 200 SSE，192 delta，1 done，0 error，DOM 渲染 39 张地点卡片，首个可见 8808ms，EOF 9896ms，模型调用 4 次，工具调用 5 次。输入框清空后提交按钮正常置灰。
    - Case 2（吐字中取消）：HTTP 200 SSE，13 delta，0 done，0 error，首个可见 7744ms，在 7849ms 点击取消，成功中止流，静默观察 3000ms 无迟到更新，控件恢复可用（提交按钮重新启用），模型调用 4 次，工具调用 6 次，最终模型轮次被取消，产出 0 张卡片（符合取消预期）。
    - Case 3（取消后新会话推荐（单条新user消息））：HTTP 200 SSE，181 delta，1 done，0 error，DOM 渲染 41 张地点卡片，首个可见 7925ms，EOF 8426ms，模型调用 4 次，工具调用 6 次。输入框清空后提交按钮正常置灰。
  - 限制与边界说明：
    - 链路追踪无请求 ID，依赖时间窗口和序号关联，不能排除外部并发。
    - 截图保护隐私已脱敏，业务成功依据真实 SSE 计数与未脱敏 DOM 卡片断言。
    - 桌面视口 1440（Case 1 与 Case 3 截屏宽度 1425，排除滚动条；Case 2 截屏宽度 1440），移动端视口与截屏宽度均为 390。
    - 本次测试不包含 1920 分辨率、暗色模式或减少动画的完整视觉矩阵，不包含对餐馆价格与实时营业状态等客观事实的核查。
    - 多轮会话上下文传递沿用此前历史验证，本次 3 个场景为全新独立真实验证。

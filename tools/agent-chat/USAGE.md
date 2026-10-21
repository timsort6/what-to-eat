# Agent Chat Demo 工具使用说明（中文）

本工具（`tools/agent-chat`）是一个**客户端**测试程序，用于通过 HTTP 调用 food-agent 的 Agent Chat（M3）接口，并用 **SSE（`text/event-stream`）**接收流式输出。

它用于快速验证（端到端）：
- JWT 鉴权是否正常
- 服务端是否已正确初始化 AMap MCP（依赖在服务端）
- LLM 是否能流式输出（`delta`）
- 最终 `done` 事件是否包含 `answer_text` + `places[]`（常用字段 + `raw`）

说明：当前后端接口**仅支持流式**（SSE），不提供 `stream=false` 的非流式模式。

## 快速开始

1) 启动服务端（并确保 MCP/LLM/DB 配置都已就绪）：

```bash
make run-server
```

2) 注册并登录，获取 JWT token：

```bash
go run ./tools/agent-chat register --username=test_user --password='Abcd1234!'
TOKEN="$(go run ./tools/agent-chat login --username=test_user --password='Abcd1234!')"
export FOOD_AGENT_TOKEN="${TOKEN}"
```

3) 用经纬度发起 chat（终端会实时打印模型输出）：

```bash
go run ./tools/agent-chat chat --prompt="附近有什么好吃的？想吃粤菜，人均 100 左右" --location="121.1,31.1"
```

## 进阶用法

### 1) 打印原始 SSE 事件（排障用）

```bash
go run ./tools/agent-chat chat --prompt="推荐 3 家火锅" --location="121.1,31.1" --raw-events
```

### 2) 使用 `address_id` 替代 `location`

如果你想用 `address_id` 测试，需要先通过地址 CRUD API 创建一个常用地址（见 `docs/demo_m0_m1.md`），然后：

```bash
go run ./tools/agent-chat chat --prompt="推荐 3 家适合约会的餐厅" --address-id=1 --limit=10
```

### 3) 使用多轮对话 messages 文件

创建一个 JSON 文件，例如 `tmp/messages.json`：

```json
[
  {"role":"user","content":"我在公司附近，想吃日料"},
  {"role":"assistant","content":"你更偏好寿司还是拉面？"},
  {"role":"user","content":"寿司，预算 150 以内"}
]
```

然后：

```bash
go run ./tools/agent-chat chat --messages-file=tmp/messages.json --location="121.1,31.1"
```

## 常见问题（Troubleshooting）

1) 报错 `missing token`
- 通过 `--token` 显式传入，或设置环境变量 `FOOD_AGENT_TOKEN`
- 或者传 `--username/--password`，工具会尝试自动登录获取 token

2) 终端没有任何流式输出
- 看服务端日志：LLM 配置 / MCP key / DB 是否正常
- 加 `--raw-events` 看服务端是否在推送 `error` 事件

3) `chat failed status=400`
- 请求必须至少包含一个：`location` 或 `address_id`
- `messages` 不能为空（使用 `--prompt` 或 `--messages-file`）

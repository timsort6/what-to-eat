# tools/agent-chat

一个用于测试 food-agent Web Server 的 **Agent Chat（M3）** 接口的命令行工具。

它会通过 HTTP 调用并解析 **SSE（text/event-stream）**：

- `POST /api/v1/agent/chat`（服务端会持续推送 `delta`，最后推送 `done`）

用于快速验证：
- 鉴权是否正常（register/login/token）
- agent/chat 是否能流式输出文本
- `done` 事件是否包含 `answer_text` + `places[]`（常用字段 + raw）

## 快速开始

最常用：直接用经纬度发起 chat（会在终端实时打印输出）：

```bash
go run ./tools/agent-chat chat --prompt="附近有什么好吃的？" --location="121.1,31.1"
```

如果你已经有 JWT token（推荐放到环境变量）：

```bash
export FOOD_AGENT_TOKEN="<jwt>"
go run ./tools/agent-chat chat --prompt="推荐 3 家适合约会的餐厅" --location="121.1,31.1"
```

如果你还没有 token，可以先登录获取：

```bash
go run ./tools/agent-chat login --username=test_user --password='Abcd1234!'
```

## 参数说明

全局参数：
- `--base-url`：服务地址（默认 `http://127.0.0.1:8080`）
- `--token`：JWT token（也可用环境变量 `FOOD_AGENT_TOKEN`）
- `--username/--password`：可选；用于 `chat` 自动登录获取 token
- `--raw-events`：打印原始 SSE event payload（排障用）

chat 参数：
- `--prompt`：单条 user 消息（或用 `--messages-file` 传多轮对话 JSON）
- `--location`：`lng,lat`（推荐）或 `--address-id`
- `--keywords/--types/--radius/--sortrule/--region`：POI2 搜索参数（可选）
- `--limit`：请求 places 数量（1-25）
- `--places-limit`：done 事件里最多打印前 N 条 places（0 表示不打印）

## 详细用法

见 `tools/agent-chat/USAGE.md`。

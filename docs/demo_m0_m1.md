# M0/M1/M2/M3 启动与测试（Demo 指南）

本文用于指导你在本地启动并测试当前已完成的功能：

- M0：Web Server **强制依赖** AMap MCP Server（stdio 子进程）
- M1：注册 / 登录 / 鉴权（JWT）
- M2：常用地址（收藏地址）CRUD（鉴权）
- M3：Agent Chat（LLM + AMap MCP）**SSE 流式输出**（鉴权）

## 0. 你需要准备的组件

### 必需

1) Go（建议与 `go.mod` 一致的版本：Go 1.25）

2) 数据库（二选一）

- PostgreSQL（默认配置使用 pgsql）
- MySQL（如果你把 `data.database.type` 改成 mysql）

3) AMap MCP Server 配置文件（独立）

- `configs/mcp-amap.yaml` 里必须填 `amap.key`

4) LLM 配置（M3 必需）

你需要在 `configs/config.yaml` 里配置 `llm`（或通过环境变量注入）：
- `llm.base_url`（OpenAI-compatible）
- `llm.api_key`（如果网关需要）
- `llm.model`（必须填一个可用模型名）

### 可选

4) Redis

当前 web server 支持“有 Redis 就用 Redis，否则用内存缓存”：
- 如果 `data.redis.addr` 为空，则禁用 redis，自动使用内存缓存

## 1. 配置文件准备

### 1.1 准备 mcp-server 配置（必须）

编辑 `configs/mcp-amap.yaml`：

```yaml
amap:
  key: "你的高德 key"
  base_url: "https://restapi.amap.com/v3"
  timeout_seconds: 5
```

### 1.2 准备 web server 配置（必须）

建议以 `configs/config.yaml_example` 为模板复制一份：

```bash
cp configs/config.yaml_example configs/config.yaml
```

然后重点检查下面这些字段：

1) JWT 配置（M1）

- `auth.jwt_secret`：开发环境可以随便填，但 **不能留空**
- 当 `server.mode=release` 时，如果 `auth.jwt_secret` 仍是默认值 `change-me`，web server 会启动失败（安全策略）

2) MCP 子进程配置（M0）

web server **强制依赖** AMap MCP Server，因此：
- `mcp.servers.amap.enabled` 必须为 `true`
- `mcp.servers.amap.transport` 必须为 `stdio`
- `mcp.servers.amap.command/args/config_file` 必须正确

建议开发态配置如下（由 web server 拉起子进程 `go run ./cmd/mcp-server`）：

```yaml
mcp:
  servers:
    amap:
      enabled: true
      transport: stdio
      command: "go"
      args: ["run","./cmd/mcp-server"]
      work_dir: "/mnt/d/github/food-agent"
      config_file: "configs/mcp-amap.yaml"
      env: {}
```

3) LLM 配置（M3）

示例（按你的 OpenAI-compatible 网关替换即可）：

```yaml
llm:
  base_url: "https://api.openai.com/v1"
  api_key: "<your-key>"
  model: "gpt-4o-mini"
  language: "zh"
  timeout_seconds: 60
  max_retries: 2
```

你也可以用环境变量注入（注意 env 名称是 `FOOD_AGENT_AI_*`）：

```bash
export FOOD_AGENT_AI_BASE_URL="https://api.openai.com/v1"
export FOOD_AGENT_AI_API_KEY="<your-key>"
export FOOD_AGENT_AI_MODEL="gpt-4o-mini"
```

## 2. 启动数据库（示例）

下面给出一个 PostgreSQL 的 docker 示例（你也可以用本机安装的 PG）：

```bash
docker run --name food-agent-pg \
  -e POSTGRES_PASSWORD=postgres \
  -e POSTGRES_DB=food_agent \
  -p 5432:5432 \
  -d postgres:16
```

对应 `configs/config.yaml` 的默认 pgsql 配置大致为：
- host: `127.0.0.1`
- port: `5432`
- user: `postgres`
- password: `postgres`
- dbname: `food_agent`

启动后 web server 会自动执行 `AutoMigrate` 创建/更新表结构。

## 3. 启动服务

### 3.1 用 Makefile 启动（推荐）

```bash
make run-server
```

它等价于：

```bash
go run ./cmd/server --config configs/config.yaml
```

### 3.2 单独启动 mcp-server（可选，用于排障）

```bash
make run-mcp
```

或：

```bash
go run ./cmd/mcp-server --config configs/mcp-amap.yaml
```

注意：mcp-server 是 stdio server，单独运行时会阻塞等待 stdin 消息，这属于正常现象。

## 4. 用 curl 测试 M0/M1 API

### 4.1 探活接口（M0）

```bash
curl http://127.0.0.1:8080/api/v1/ping
```

### 4.2 注册（M1）

用户名规则：仅允许英文/数字/下划线，长度 3~32。  
密码规则：必须达到 `pwdutil.Moderate`（建议用 `Abcd1234!` 这类）。

```bash
curl -X POST http://127.0.0.1:8080/api/v1/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"username":"test_user","password":"Abcd1234!"}'
```

### 4.3 登录（M1）

```bash
curl -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"test_user","password":"Abcd1234!"}'
```

返回的 `data.token` 是 JWT。

你可以把 token 保存到环境变量（需要 `jq`）：

```bash
TOKEN="$(curl -sS -X POST http://127.0.0.1:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"username":"test_user","password":"Abcd1234!"}' | jq -r '.data.token')"
```

### 4.4 获取当前用户信息（M1）

```bash
curl http://127.0.0.1:8080/api/v1/me \
  -H 'Authorization: Bearer <token>'
```

不带 token 会返回 401。

## 5. 用 curl 测试 M2（常用地址）

### 5.1 创建常用地址（M2）

只要前端/你自己能确定最终地点，后端就只负责存储。

最小请求（必须有 `label` + `location`）：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/addresses \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{"label":"家","location":"121.1,31.1"}'
```

带 raw POI（推荐，便于后续 chat 上下文与前端展示）：

```bash
curl -X POST http://127.0.0.1:8080/api/v1/addresses \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -d '{
    "label":"公司",
    "location":"121.2,31.2",
    "formatted_address":"上海市徐汇区 ...",
    "amap_id":"B0FF...",
    "name":"某某大厦",
    "amap_poi":{"id":"B0FF...","name":"某某大厦","location":"121.2,31.2"}
  }'
```

说明：
- 用户的第一个地址会被强制设为默认地址
- `PUT` 仅支持设默认（`is_default=true`），不支持显式取消默认
- 删除默认地址时会自动迁移默认到 **最小 id**

### 5.2 查询常用地址列表（M2）

```bash
curl http://127.0.0.1:8080/api/v1/addresses \
  -H "Authorization: Bearer ${TOKEN}"
```

## 6. 用 curl 测试 M3（Agent Chat，SSE）

该接口只支持 SSE 流式输出：
- `event:delta`：模型输出增量（用于前端实时渲染）
- `event:done`：最终结果（包含完整 `answer_text` + `places[]`）

### 6.1 直接用 location 发起 chat（推荐用于测试）

```bash
curl -N -X POST http://127.0.0.1:8080/api/v1/agent/chat \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d '{
    "messages":[{"role":"user","content":"附近有什么好吃的？想吃粤菜，人均 100 左右"}],
    "location":"121.1,31.1",
    "radius":5000,
    "sortrule":"distance",
    "limit":10
  }'
```

### 6.2 使用 address_id 发起 chat

先从 `/api/v1/addresses` 找到一个 `id`，然后：

```bash
curl -N -X POST http://127.0.0.1:8080/api/v1/agent/chat \
  -H "Authorization: Bearer ${TOKEN}" \
  -H 'Content-Type: application/json' \
  -H 'Accept: text/event-stream' \
  -d '{
    "messages":[{"role":"user","content":"帮我推荐 3 家适合约会的餐厅"}],
    "address_id": 1,
    "radius":3000,
    "limit":10
  }'
```

注意：请求中必须至少提供 `address_id` 或 `location`，否则返回 400。

## 7. Apifox 测试建议

你可以导入本项目根目录下的 OpenAPI 文件：

- `food-agent.openapi.yaml`

然后在 Apifox 里手动设置环境变量（例如 token），即可进行接口联调。

## 8. 常见问题排查

1) Web server 启动失败，提示 MCP 相关错误
- 检查 `mcp.servers.amap.command/args/work_dir/config_file`
- 检查 `configs/mcp-amap.yaml` 是否填了 `amap.key`

2) Web server 启动失败，提示 auth.jwt_secret 默认值
- 如果 `server.mode=release`，请把 `auth.jwt_secret` 改成非默认值

3) 注册返回 “password too weak”
- 换一个更强的密码（至少包含大小写、数字、特殊字符更稳）

4) M3 chat 输出为空/报错
- 检查 `llm.base_url/api_key/model` 是否已配置且可用
- 检查服务端日志里是否有 LLM 网关错误（鉴权/模型名不存在等）

# MCP AMap Demo 工具使用说明（中文）

本工具（`tools/mcp-amap`）是一个**客户端**测试程序，用于快速验证 AMap MCP server（stdio）是否可用。它会：

1. 拉起 `cmd/mcp-server` 作为 **stdio 子进程**
2. 通过 `internal/mcp.StdioClient` 连接（stdin/stdout 上的 JSON-RPC）
3. 调用 MCP methods/tools 并打印结果

适合用于快速确认：
- MCP server 是否能用你的配置正确启动
- tools 是否可发现（`tools/list`）
- 各 tool 的返回 `status/data` 是否符合预期

## 快速开始

1) 在 `configs/mcp-amap.yaml` 中填写你的高德 Key：

```yaml
amap:
  key: "YOUR_AMAP_KEY"
```

2) 运行交互模式（REPL）：

```bash
go run ./tools/mcp-amap
```

3) 或者用 one-shot 命令直接调用：

```bash
go run ./tools/mcp-amap tools-list
go run ./tools/mcp-amap geocode --address="四川省成都市锦城大道666号奥克斯广场" --city="成都"
```

## 全局参数（子进程如何启动）

- `--config`（默认：`configs/mcp-amap.yaml`）
  - 可以是**文件路径**或**目录**
  - 如果传目录，会解析为 `<dir>/mcp-amap.yaml`
- `--server-command`（默认：`go`）
- `--server-args`（默认：`run ./cmd/mcp-server`）
- `--work-dir`（默认：`.`）

工具在底层会用两种方式把 config 路径传给子进程：
- env：`FOOD_AGENT_MCP_CONFIG=<resolved_config_path>`
- args：如果你在 `--server-args` 里没有显式提供 `--config`，工具会自动追加 `--config <resolved_config_path>`

这种“冗余传参”是为了让启动方式更稳健（不同启动方式都能拿到 config）。

其他常用参数：
- `--call-timeout-seconds`（默认：15）
- `--raw`：打印完整 JSON 返回
- `--limit`：摘要输出时最多打印前 N 条 POI（默认：3）

## 命令映射（命令实际调用了什么）

### 1) `tools-list`

- MCP method：`tools/list`
- 用途：发现能力 + 验证连通性

命令：

```bash
go run ./tools/mcp-amap tools-list
```

期望输出（至少应该出现这些 tool 名称）：
- `amap.geocode`
- `amap.regeo`
- `amap.poi2_text`
- `amap.poi2_around`
- `amap.search_around` (alias)

如果加 `--raw`，你还会看到每个 tool 的 `inputSchema`。

### 2) `geocode`

- MCP tool：`amap.geocode`
- 对应后端 API：AMap v3 地理编码（`/v3/geocode/geo`）

命令：

```bash
go run ./tools/mcp-amap geocode --address="四川省成都市锦城大道666号奥克斯广场" --city="成都"
```

关键入参：
- `--address` 必填
- `--city` 可选

如何判断“正常”：
- `status` 应为 `"1"`
- `infocode` 常见 `"10000"` 表示 OK
- `geocodes[0].location` 形如 `"lng,lat"`

### 3) `regeo`

- MCP tool：`amap.regeo`
- 对应后端 API：AMap v3 逆地理编码（`/v3/geocode/regeo`）

命令：

```bash
go run ./tools/mcp-amap regeo --location="104.062059,30.576459" --radius=1000 --extensions=base
```

关键入参：
- `--location` 必填，格式 `lng,lat`
- `--radius` 可选
- `--extensions` 可选（`base` 或 `all`）

如何判断“正常”：
- `status` 应为 `"1"`
- `infocode` 常见 `"10000"`

### 4) `poi2-text`

- MCP tool：`amap.poi2_text`
- 对应后端 API：AMap v5 POI2 文本搜索（`/v5/place/text`）

命令：

```bash
go run ./tools/mcp-amap poi2-text --keywords="美食" --region="成都" --page-size=10 --page-num=1
```

关键入参：
- `--keywords` 或 `--types` 至少要设置一个
- `--region` 可选
- `--city-limit` 可选（仅当你设置了该 flag 时才会下发）

如何判断“正常”：
- `status` 应为 `"1"`
- `count` 是字符串数字
- 摘要模式会打印前 N 条 POI（`name/location/distance/...`）
- 用 `--raw` 查看完整 POI 对象

### 5) `poi2-around`

- MCP tool：`amap.poi2_around`
- 对应后端 API：AMap v5 POI2 周边搜索（`/v5/place/around`）

命令：

```bash
go run ./tools/mcp-amap poi2-around --location="104.062059,30.576459" --keywords="川菜" --radius=2000 --sortrule=distance
```

关键入参：
- `--location` 必填
- `--radius` 可选
- `--sortrule` 可选（`distance` / `weight`）

如何判断“正常”：
- `status` 应为 `"1"`
- 摘要模式会打印前 N 条 POI

## 常见问题（Troubleshooting）

1) 子进程启动失败
- 检查 `--server-command/--server-args/--work-dir`
- 如果你使用了相对路径，把 `--work-dir` 设为仓库根目录（repo root）

2) 找不到配置 / key 缺失
- 确保 `--config` 最终能解析到一个文件，且包含 `amap.key`
- 你可以传目录，让工具自动解析为 `<dir>/mcp-amap.yaml`

3) tool 返回 `status != 1`
- 加 `--raw` 打印完整返回
- 检查 `info` / `infocode`（高德会给出原因，例如 key 无效、配额不足、参数不合法等）

4) 超时（Timeout）
- 增大 `--call-timeout-seconds`

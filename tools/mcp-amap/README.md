# tools/mcp-amap

一个用于测试 **AMap MCP Server（stdio）** 的交互式命令行工具：

- 它会拉起 `cmd/mcp-server` 作为子进程（stdio）
- 通过 `internal/mcp.StdioClient`（stdin/stdout JSON-RPC）调用 MCP tools

## 前置条件

- 在 `configs/mcp-amap.yaml` 中填写你的高德 Key：
  - `amap.key: "..."`。

## 运行

交互模式（REPL）：

```bash
go run ./tools/mcp-amap
```

单次调用（one-shot）：

```bash
go run ./tools/mcp-amap geocode --address="四川省成都市锦城大道666号奥克斯广场" --city="成都"
```

列出可用 tools：

```bash
go run ./tools/mcp-amap tools-list
```

## 常用参数

- `--config`：配置文件路径或目录（传目录时解析为 `<dir>/mcp-amap.yaml`）
- `--server-command`：默认 `go`
- `--server-args`：默认 `run ./cmd/mcp-server`
- `--work-dir`：默认 `.`
- `--raw`：打印 MCP server 返回的完整 JSON 文本
- `--limit`：摘要输出时最多打印多少条 POI

## 详细文档

- 见 `tools/mcp-amap/USAGE.md`（命令映射 + 排障）

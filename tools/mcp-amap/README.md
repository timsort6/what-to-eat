# tools/mcp-amap

An interactive CLI to test the AMap MCP server (stdio) by spawning `cmd/mcp-server`
as a subprocess and calling MCP tools via `internal/mcp.StdioClient`.

## Prerequisites

- Fill `configs/mcp-amap.yaml` with your AMap key:
  - `amap.key: "..."`.

## Run

Interactive:

```bash
go run ./tools/mcp-amap
```

One-shot:

```bash
go run ./tools/mcp-amap geocode --address="四川省成都市锦城大道666号奥克斯广场" --city="成都"
```

List tools:

```bash
go run ./tools/mcp-amap tools-list
```

## Flags

- `--config` config file path OR a directory (dir => `<dir>/mcp-amap.yaml`)
- `--server-command` defaults to `go`
- `--server-args` defaults to `run ./cmd/mcp-server`
- `--work-dir` defaults to `.`
- `--raw` prints the full JSON text returned by the MCP server
- `--limit` how many POIs to print in summary output

## Docs

- See `tools/mcp-amap/USAGE.md` for detailed command mapping and troubleshooting.

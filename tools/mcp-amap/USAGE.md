# MCP AMap Demo Tool Usage

This tool (`tools/mcp-amap`) is a **client-side** test program. It:

1. Spawns `cmd/mcp-server` as a **stdio** subprocess.
2. Connects to it via `internal/mcp.StdioClient` (JSON-RPC over stdin/stdout).
3. Calls MCP methods/tools and prints the results.

It is meant for quickly verifying:
- the MCP server starts correctly with your config
- the tool surface is discoverable (`tools/list`)
- each tool returns expected status and data

## Quick Start

1) Fill your AMap key in `configs/mcp-amap.yaml`:

```yaml
amap:
  key: "YOUR_AMAP_KEY"
```

2) Run interactive mode:

```bash
go run ./tools/mcp-amap
```

3) Or run one-shot commands:

```bash
go run ./tools/mcp-amap tools-list
go run ./tools/mcp-amap geocode --address="四川省成都市锦城大道666号奥克斯广场" --city="成都"
```

## Global Flags (How The Subprocess Is Started)

- `--config` (default: `configs/mcp-amap.yaml`)
  - Can be a **file path** OR a **directory**
  - If a directory is given, it resolves to `<dir>/mcp-amap.yaml`
- `--server-command` (default: `go`)
- `--server-args` (default: `run ./cmd/mcp-server`)
- `--work-dir` (default: `.`)

Under the hood, the tool passes the config path to the subprocess in two ways:
- env: `FOOD_AGENT_MCP_CONFIG=<resolved_config_path>`
- args: it appends `--config <resolved_config_path>` if you didn't already provide it in `--server-args`

This redundancy makes the startup robust across different launch styles.

Other helpful flags:
- `--call-timeout-seconds` (default: 15)
- `--raw` print full JSON result text
- `--limit` print first N POIs in summary mode (default: 3)

## Command Mapping (What Calls What)

### 1) `tools-list`

- MCP method: `tools/list`
- Purpose: capability discovery and connectivity verification

Command:

```bash
go run ./tools/mcp-amap tools-list
```

Expected output (at least these tool names should appear):
- `amap.geocode`
- `amap.regeo`
- `amap.poi2_text`
- `amap.poi2_around`
- `amap.search_around` (alias)

If you run with `--raw`, you should also see each tool's `inputSchema`.

### 2) `geocode`

- MCP tool: `amap.geocode`
- Backend API: AMap v3 geocode (`/v3/geocode/geo`)

Command:

```bash
go run ./tools/mcp-amap geocode --address="四川省成都市锦城大道666号奥克斯广场" --city="成都"
```

Key inputs:
- `--address` required
- `--city` optional

How to judge "normal":
- `status` should be `"1"`
- `infocode` often `"10000"` for OK
- `geocodes[0].location` should look like `"lng,lat"`

### 3) `regeo`

- MCP tool: `amap.regeo`
- Backend API: AMap v3 reverse geocode (`/v3/geocode/regeo`)

Command:

```bash
go run ./tools/mcp-amap regeo --location="104.062059,30.576459" --radius=1000 --extensions=base
```

Key inputs:
- `--location` required, format `lng,lat`
- `--radius` optional
- `--extensions` optional (`base` or `all`)

How to judge "normal":
- `status` should be `"1"`
- `infocode` often `"10000"`

### 4) `poi2-text`

- MCP tool: `amap.poi2_text`
- Backend API: AMap v5 POI2 text (`/v5/place/text`)

Command:

```bash
go run ./tools/mcp-amap poi2-text --keywords="美食" --region="成都" --page-size=10 --page-num=1
```

Key inputs:
- `--keywords` or `--types` must be set (at least one)
- `--region` optional
- `--city-limit` optional (only sent if you set the flag)

How to judge "normal":
- `status` should be `"1"`
- `count` is a string number
- summary mode prints first N POIs with `name/location/distance/...`
- use `--raw` to inspect full POI objects

### 5) `poi2-around`

- MCP tool: `amap.poi2_around`
- Backend API: AMap v5 POI2 around (`/v5/place/around`)

Command:

```bash
go run ./tools/mcp-amap poi2-around --location="104.062059,30.576459" --keywords="川菜" --radius=2000 --sortrule=distance
```

Key inputs:
- `--location` required
- `--radius` optional
- `--sortrule` optional (`distance` / `weight`)

How to judge "normal":
- `status` should be `"1"`
- summary mode prints first N POIs

## Troubleshooting

1) Subprocess fails to start
- Check `--server-command/--server-args/--work-dir`
- If using relative paths, set `--work-dir` to the repo root.

2) Config not found / key missing
- Ensure `--config` resolves to a file and contains `amap.key`
- You can pass a directory and let the tool resolve to `<dir>/mcp-amap.yaml`

3) Tool returns `status != 1`
- Print raw output: add `--raw`
- Inspect `info` / `infocode` (AMap will tell you why, e.g. invalid key, quota, invalid parameters)

4) Timeout
- Increase `--call-timeout-seconds`


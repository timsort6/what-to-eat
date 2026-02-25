# food-agent-mcp-server (AMap) Design Notes

This folder contains the `food-agent-mcp-server` binary: an MCP server that exposes AMap
WebService APIs as MCP tools.

This doc records the rationale and the minimal design so future work (HTTP JSON-RPC / SSE)
can reuse the same core.

## What Changed (This Iteration)

- The AMap MCP server now uses an **independent config file** (default: `configs/mcp-amap.yaml`).
  - `cmd/mcp-server` resolves config path by priority:
    - flag `--config`
    - env `FOOD_AGENT_MCP_CONFIG`
    - default `configs/mcp-amap.yaml`
- The main web server config now has an explicit field:
  - `mcp.servers.amap.config_file`
  - When `mcp.servers.amap.enabled=true`, the web server starts the MCP subprocess and passes:
    - `FOOD_AGENT_MCP_CONFIG=<config_file>`
- `mcp.servers.amap.work_dir` is now functional:
  - the stdio subprocess uses it as working directory (`exec.Cmd.Dir`).

## Package Layout (Reusable Core)

The MCP server implementation is split into reusable packages under `pkg/mcpserver`:

- `pkg/mcpserver/jsonrpc`
  - JSON-RPC 2.0 message types and standard error codes.
- `pkg/mcpserver/stdio`
  - Content-Length framing + stdio serve loop.
  - Transport-only; no AMap dependency.
- `pkg/mcpserver/amap`
  - AMap MCP method routing (`initialize`, `tools/list`, `tools/call`) and tool implementations.
  - Depends on `food-agent/pkg/amap` SDK via a small `Backend` interface so it is unit-testable.

`cmd/mcp-server` is just the composition root:
- loads options/config
- initializes logger
- builds AMap SDK client
- wires it into `pkg/mcpserver/amap.Server`
- runs `pkg/mcpserver/stdio.Serve(os.Stdin, os.Stdout, server.Handle)`

## Tool Surface

Currently exposed tools:

- `amap.geocode` (v3)
- `amap.regeo` (v3)
- `amap.poi2_text` (v5)
- `amap.poi2_around` (v5)
- `amap.search_around` (alias of `amap.poi2_around`, kept for compatibility)

All tools return a single MCP content item: `{type:"text", text:"<pretty json>"}`.

## Why Independent Config File

Goal: treat the AMap MCP server as a microservice-like component.

- The MCP server can own secrets (AMap key) without forcing the main web server to also hold them.
- Web server only needs "how to start/connect to the MCP server", not vendor credentials.

If later the MCP server is deployed remotely, the config stays server-owned; the web server
only stores connection settings.

## Future: HTTP JSON-RPC Transport

Target: keep `pkg/mcpserver/amap.Server` unchanged; only add a new transport that converts
HTTP requests into `jsonrpc.Request` and writes back `jsonrpc.Response`.

Suggested layout:

- `pkg/mcpserver/httpjsonrpc`
  - `ServeHTTP(server *amap.Server)` returning `http.Handler`
  - endpoint: `POST /rpc` body = JSON-RPC request
  - response: JSON-RPC response

Notes:
- JSON-RPC over HTTP typically uses plain JSON body without Content-Length framing.
- IDs can be string/number/null; keep `json.RawMessage` to echo IDs back verbatim.

## Future: SSE Transport (Remote MCP Server)

MCP has remote transports in the ecosystem (SSE is one common pattern). To support it:

1) Keep the core logic (tools + method routing) in `pkg/mcpserver/amap.Server`.
2) Implement an SSE transport that:
   - receives requests (often via `POST` or a bidirectional channel abstraction)
   - emits responses/events over an SSE stream

Implementation strategy:
- Create `pkg/mcpserver/transport` interfaces:
  - `type Responder interface { Send(jsonrpc.Response) error }`
  - `type RequestStream interface { Recv() (jsonrpc.Request, error) }`
- Implement adapters:
  - stdio: RequestStream from framing + Responder to `writeMessage`
  - http: RequestStream per request, Responder writes HTTP response
  - sse: RequestStream from some upstream channel, Responder emits SSE events

This keeps the same `Server.Handle(ctx, req)` flow.

## Implementation Prompts / Checklists (For Future Work)

When you add HTTP JSON-RPC:

- Add `mcp.servers.amap.transport=http` and `mcp.client.amap.url` (or similar).
- Keep stdio as default for local dev.
- Ensure tool output is still JSON text (or add optional structured output).
- Add tests:
  - HTTP handler: valid request -> valid response
  - unknown method/tool -> correct JSON-RPC error code

When you add SSE:

- Decide which SSE flavor you want:
  - pure server->client events, plus client POST for requests
  - or full duplex via a session id + two endpoints
- Define a stable message envelope:
  - `event: message`
  - `data: <jsonrpc.Response>`
- Implement reconnect strategy and session cleanup.


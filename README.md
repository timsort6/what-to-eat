# food-agent

一个基于 **Eino (Agent)** + **RAG (PostgreSQL/pgvector)** + **MCP** 的智能美食推荐系统。

目录结构遵循 Go 项目常用的 Clean Architecture 风格：
- `cmd/`：入口（server + tools）
- `internal/`：业务代码（handler/service/repository/agent 等）
- `pkg/`：可复用组件（config/db/cache/logger/amap sdk 等）

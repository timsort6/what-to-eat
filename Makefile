DIST ?= dist

SERVER_BIN := $(DIST)/food-agent-server
MCP_BIN := $(DIST)/food-agent-mcp-server

.PHONY: help
help: ## Show help
	@grep -E '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) | awk 'BEGIN {FS=":.*## "}; {printf "%-22s %s\n", $$1, $$2}'

.PHONY: fmt
fmt: ## Run gofmt on repo go files
	@echo "+ $@"
	@gofmt -w $$(find . -type f -name '*.go' \
		-not -path './.gocache/*' \
		-not -path './.gomodcache/*' \
		-not -path './.gotmp/*' \
		-not -path './tmp/*')

.PHONY: test
test: ## Run unit tests
	@echo "+ $@"
	@go test ./...

.PHONY: build
build: ## Build server & mcp-server binaries
	@echo "+ $@"
	@mkdir -p $(DIST)
	@go build -v -o $(SERVER_BIN) ./cmd/server
	@go build -v -o $(MCP_BIN) ./cmd/mcp-server

.PHONY: run-server
run-server: ## Run web server (go run)
	@echo "+ $@"
	@go run ./cmd/server --config configs/config.yaml

.PHONY: run-mcp
run-mcp: ## Run mcp-server (go run)
	@echo "+ $@"
	@go run ./cmd/mcp-server --config configs/mcp-amap.yaml

.PHONY: clean
clean: ## Remove build artifacts
	@echo "+ $@"
	@rm -rf $(DIST)


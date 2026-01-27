package main

import (
	"os"

	"food-agent/cmd/mcp-server/app"
)

func main() {
	if err := app.Execute(); err != nil {
		os.Exit(1)
	}
}


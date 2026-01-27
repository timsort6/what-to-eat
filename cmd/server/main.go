package main

import (
	"food-agent/cmd/server/app"
	"os"
)

func main() {
	if err := app.Execute(); err != nil {
		os.Exit(1)
	}
}

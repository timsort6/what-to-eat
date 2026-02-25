package app

import (
	"os"

	"food-agent/internal/options"
	"food-agent/pkg/client/amap"
	"food-agent/pkg/logger"
	mcpamap "food-agent/pkg/mcpserver/amap"
	"food-agent/pkg/mcpserver/stdio"

	"github.com/spf13/cobra"
)

func Run(cmd *cobra.Command, opts *options.MCPServerRunOptions) error {
	if err := opts.Complete(cmd, opts.ConfigFile); err != nil {
		return err
	}

	// Initialize zap logger for this process (stderr/stdout/file per config).
	logger.Init(opts.Log)

	// Build SDK client.
	cli, err := amap.NewFromOptions(opts.Amap)
	if err != nil {
		return err
	}

	backend := mcpamap.NewSDKBackend(cli)
	server := mcpamap.NewServer(backend)

	// stdio MCP server loop.
	return stdio.Serve(cmd.Context(), os.Stdin, os.Stdout, server.Handle)
}

package app

import (
	"food-agent/internal/options"
	"github.com/spf13/cobra"
)

func Execute() error {
	return NewCommand().Execute()
}

func NewCommand() *cobra.Command {
	opts := options.NewMCPServerRunOptions()

	cmd := &cobra.Command{
		Use:   "food-agent-mcp-server",
		Short: "Food Agent MCP Server (stdio)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return Run(cmd, opts)
		},
		SilenceUsage: true,
	}

	opts.AddFlags(cmd.Flags())

	return cmd
}

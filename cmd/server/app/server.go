package app

import (
	"food-agent/internal/options"
	"food-agent/pkg/server"

	"github.com/spf13/cobra"
)

func Run(cmd *cobra.Command, opts *options.ServerRunOptions) error {
	// cmd/server: default config file is configs/config.yaml
	if err := opts.Complete(cmd, opts.ConfigFile); err != nil {
		return err
	}

	stopCh := server.SetupSignalHandler()
	s, err := NewConsoleServer(opts, stopCh)
	if err != nil {
		return err
	}
	if err := s.Prepare(cmd.Context()); err != nil {
		return err
	}
	return s.Run()
}

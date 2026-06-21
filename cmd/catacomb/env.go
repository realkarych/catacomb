package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

func newEnvCmd() *cobra.Command {
	var discoveryPath string
	cmd := &cobra.Command{
		Use:   "env",
		Short: "Print OTLP environment variables for connecting to the running daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if discoveryPath == "" {
				discoveryPath = daemon.DiscoveryPath()
			}
			d, err := daemon.ReadDiscovery(discoveryPath)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "CLAUDE_CODE_ENABLE_TELEMETRY=1\n")
			fmt.Fprintf(cmd.OutOrStdout(), "OTEL_TRACES_EXPORTER=otlp\n")
			fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf\n")
			fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_ENDPOINT=http://%s\n", d.Addr)
			fmt.Fprintf(cmd.OutOrStdout(), "OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer %s\n", d.Token)
			return nil
		},
	}
	cmd.Flags().StringVar(&discoveryPath, "discovery", "", "discovery file path (default: resolved CATACOMB_DISCOVERY)")
	return cmd
}

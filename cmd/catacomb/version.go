package main

import "github.com/spf13/cobra"

var Version = "dev"

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cmd.Printf("catacomb %s\n", Version)
			return nil
		},
	}
}

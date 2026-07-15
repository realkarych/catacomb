package main

import (
	"runtime/debug"

	"github.com/spf13/cobra"
)

var Version = "dev"

func versionFromBuild(current string, read func() (*debug.BuildInfo, bool)) string {
	if current != "dev" {
		return current
	}
	info, ok := read()
	if !ok {
		return current
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return current
}

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

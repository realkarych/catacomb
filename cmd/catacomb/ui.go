package main

import (
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

func browserCommand(goos, rawURL string) *exec.Cmd {
	switch goos {
	case "darwin":
		return exec.Command("open", rawURL)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		return exec.Command("xdg-open", rawURL)
	}
}

var startCmd = func(c *exec.Cmd) error { return c.Start() }

var openBrowser = func(rawURL string) error {
	return startCmd(browserCommand(runtime.GOOS, rawURL))
}

func newUICmd() *cobra.Command {
	var noOpen bool
	cmd := &cobra.Command{
		Use:   "ui",
		Short: "Open the catacomb web UI in the default browser",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runUI(
				daemon.DiscoveryPath(),
				noOpen,
				cmd.OutOrStdout(),
			)
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the URL without opening a browser")
	return cmd
}

func runUI(discoveryPath string, noOpen bool, out io.Writer) error {
	disc, err := daemon.ReadDiscovery(discoveryPath)
	if err != nil {
		return err
	}
	u := &url.URL{
		Scheme:   "http",
		Host:     disc.Addr,
		Path:     "/",
		RawQuery: url.Values{"token": {disc.Token}}.Encode(),
	}
	rawURL := u.String()
	if _, err := fmt.Fprintln(out, rawURL); err != nil {
		return err
	}
	if noOpen {
		return nil
	}
	return openBrowser(rawURL)
}

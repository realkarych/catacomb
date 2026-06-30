package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

func newHookCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "hook <type>",
		Short: "Forward a Claude Code hook event to the catacomb daemon",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			forward(cmd.ErrOrStderr(), clientDiscoveryPath(), args[0], cmd.InOrStdin())
			return nil
		},
	}
}

func forward(warn io.Writer, discoveryPath, hookType string, stdin io.Reader) {
	payload, err := io.ReadAll(stdin)
	if err != nil {
		fmt.Fprintf(warn, "catacomb hook: read stdin: %v\n", err)
		return
	}
	d, err := daemon.ReadDiscovery(discoveryPath)
	if err != nil {
		fmt.Fprintf(warn, "catacomb hook: discovery: %v\n", err)
		return
	}
	req, err := http.NewRequest(http.MethodPost, "http://"+d.Addr+"/hook/"+url.PathEscape(hookType), bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintf(warn, "catacomb hook: request: %v\n", err)
		return
	}
	req.Header.Set("Authorization", "Bearer "+d.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(warn, "catacomb hook: forward to %s: %v\n", d.Addr, err)
		return
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		fmt.Fprintf(warn, "catacomb hook: forward to %s: status %d\n", d.Addr, resp.StatusCode)
	}
}

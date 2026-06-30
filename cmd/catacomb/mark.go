package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

type markArgs struct {
	discoveryPath string
	sessionID     string
	name          string
	boundary      string
	occurrence    int
	hasOccurrence bool
	stateRef      string
}

func newMarkCmd() *cobra.Command {
	var a markArgs
	cmd := &cobra.Command{
		Use:   "mark",
		Short: "Record a phase boundary marker in a running session",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.discoveryPath = clientDiscoveryPath()
			a.hasOccurrence = cmd.Flags().Changed("occurrence")
			return runMark(a)
		},
	}
	cmd.Flags().StringVar(&a.sessionID, "session", "", "session ID to mark")
	cmd.Flags().StringVar(&a.name, "name", "", "marker name (phase label)")
	cmd.Flags().StringVar(&a.boundary, "boundary", "", "boundary type: start or end")
	cmd.Flags().IntVar(&a.occurrence, "occurrence", 0, "explicit occurrence ordinal (optional)")
	cmd.Flags().StringVar(&a.stateRef, "state-ref", "", "opaque state reference (optional)")
	_ = cmd.MarkFlagRequired("session")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("boundary")
	return cmd
}

func runMark(a markArgs) error {
	if a.name == "" {
		return fmt.Errorf("catacomb mark: name must not be empty")
	}
	if a.boundary != "start" && a.boundary != "end" {
		return fmt.Errorf("catacomb mark: boundary must be start or end, got %q", a.boundary)
	}
	disc, err := daemon.ReadDiscovery(a.discoveryPath)
	if err != nil {
		return fmt.Errorf("catacomb mark: discovery: %w", err)
	}
	body := map[string]any{
		"session_id": a.sessionID,
		"name":       a.name,
		"boundary":   a.boundary,
	}
	if a.hasOccurrence {
		body["occurrence"] = a.occurrence
	}
	if a.stateRef != "" {
		body["state_ref"] = a.stateRef
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, "http://"+disc.Addr+"/v1/mark", bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("catacomb mark: request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+disc.Token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("catacomb mark: post to %s: %w", disc.Addr, err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("catacomb mark: status %d", resp.StatusCode)
	}
	return nil
}

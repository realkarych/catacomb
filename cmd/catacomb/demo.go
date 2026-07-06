package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

const demoSessionID = "demo-0001"

var demoHTTPClient = &http.Client{Timeout: 5 * time.Second}

type demoDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	transcript    []byte
	httpClient    *http.Client
}

func newDemoCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "demo",
		Short: "Ingest a bundled synthetic transcript into the running daemon",
		Long: `Ingest a bundled synthetic transcript into the running daemon so you can see
a populated graph without a live session.`,
		Example: `  catacomb demo`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps := demoDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: clientDiscoveryPath(),
				transcript:    demoTranscript,
				httpClient:    demoHTTPClient,
			}
			return runDemo(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
}

func runDemo(ctx context.Context, out io.Writer, deps demoDeps) error {
	disc, err := deps.readDiscovery(deps.discoveryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoDaemon
		}
		return err
	}
	endpoint := "http://" + disc.Addr + "/v1/transcript"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(deps.transcript))
	if err != nil {
		return fmt.Errorf("demo: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+disc.Token)
	req.Header.Set("Content-Type", "application/x-ndjson")

	resp, err := deps.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("demo: post transcript: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("demo: daemon returned %d", resp.StatusCode)
	}

	_, err = fmt.Fprintf(out, "Demo session %s ingested.\n", demoSessionID)
	return err
}

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

var (
	statusHTTPClient = &http.Client{}
	statusNowFn      = time.Now
)

type statusDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	discoveryPath string
	httpClient    *http.Client
	now           func() time.Time
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Print daemon addr, pid, uptime, and session/node counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps := statusDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: daemon.DiscoveryPath(),
				httpClient:    statusHTTPClient,
				now:           statusNowFn,
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
}

func runStatus(ctx context.Context, out io.Writer, deps statusDeps) error {
	disc, err := deps.readDiscovery(deps.discoveryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoDaemon
		}
		return err
	}

	now := deps.now()
	uptime := "unknown"
	tokenAge := "unknown"
	if disc.StartedAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, disc.StartedAt); parseErr == nil {
			d := now.Sub(t).Truncate(time.Second)
			uptime = d.String()
			tokenAge = d.String()
		}
	}

	sessions, nodes, fetchErr := fetchSessionCounts(ctx, disc, deps.httpClient)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "addr\t%s\n", disc.Addr)
	_, _ = fmt.Fprintf(w, "pid\t%d\n", disc.Pid)
	_, _ = fmt.Fprintf(w, "uptime\t%s\n", uptime)
	_, _ = fmt.Fprintf(w, "token age\t%s\n", tokenAge)
	_, _ = fmt.Fprintf(w, "observing\t%s\n", observingLabel(disc.TranscriptDir))
	if fetchErr != nil {
		if errors.Is(fetchErr, ErrDaemonRestarted) {
			_ = w.Flush()
			return ErrDaemonRestarted
		}
		_, _ = fmt.Fprintf(w, "sessions\tunavailable\n")
		_, _ = fmt.Fprintf(w, "nodes\tunavailable\n")
		_ = w.Flush()
		return nil
	}
	_, _ = fmt.Fprintf(w, "sessions\t%d\n", sessions)
	_, _ = fmt.Fprintf(w, "nodes\t%d\n", nodes)
	return w.Flush()
}

func observingLabel(dir string) string {
	if dir == "" {
		return "history off (enable: catacomb up --history)"
	}
	return dir
}

func fetchSessionCounts(ctx context.Context, disc daemon.Discovery, client *http.Client) (sessions, nodes int, err error) {
	u := &url.URL{
		Scheme:   "http",
		Host:     disc.Addr,
		Path:     "/v1/sessions",
		RawQuery: url.Values{"token": {disc.Token}}.Encode(),
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, fmt.Errorf("%w: %w", ErrDaemonUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusUnauthorized {
		return 0, 0, ErrDaemonRestarted
	}
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("status: sessions endpoint returned %d", resp.StatusCode)
	}

	var summaries []daemon.SessionSummary
	if decErr := json.NewDecoder(resp.Body).Decode(&summaries); decErr != nil {
		return 0, 0, fmt.Errorf("status: decode sessions: %w", decErr)
	}
	totalNodes := 0
	for _, s := range summaries {
		totalNodes += s.NodeCount
	}
	return len(summaries), totalNodes, nil
}

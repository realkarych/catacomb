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
	"strings"
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
	asJSON        bool
}

type statusReport struct {
	Addr           string   `json:"addr"`
	Pid            int      `json:"pid"`
	Uptime         string   `json:"uptime"`
	TokenAge       string   `json:"token_age"`
	ConfigPath     string   `json:"config_path,omitempty"`
	ObservingDir   string   `json:"observing_dir,omitempty"`
	StoreBackend   string   `json:"store_backend,omitempty"`
	SinkTypes      []string `json:"sink_types,omitempty"`
	SourcesEnabled []string `json:"sources_enabled,omitempty"`
	ReaperWindow   string   `json:"reaper_window,omitempty"`
	MaxShards      int      `json:"max_shards,omitempty"`
	Sessions       int      `json:"sessions"`
	Nodes          int      `json:"nodes"`
	Healthy        bool     `json:"healthy"`
}

func newStatusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Print daemon addr, pid, uptime, and session/node counts",
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps := statusDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: clientDiscoveryPath(),
				httpClient:    statusHTTPClient,
				now:           statusNowFn,
				asJSON:        asJSON,
			}
			return runStatus(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVarP(&asJSON, "json", "j", false, "output machine-readable JSON")
	return cmd
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
	healthy := fetchErr == nil

	rep := statusReport{
		Addr:           disc.Addr,
		Pid:            disc.Pid,
		Uptime:         uptime,
		TokenAge:       tokenAge,
		ConfigPath:     disc.ConfigPath,
		ObservingDir:   disc.TranscriptDir,
		StoreBackend:   disc.StoreBackend,
		SinkTypes:      disc.SinkTypes,
		SourcesEnabled: disc.SourcesEnabled,
		ReaperWindow:   disc.ReaperWindow,
		MaxShards:      disc.MaxShards,
		Sessions:       sessions,
		Nodes:          nodes,
		Healthy:        healthy,
	}

	if deps.asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(rep); encErr != nil {
			return encErr
		}
		if fetchErr != nil && errors.Is(fetchErr, ErrDaemonRestarted) {
			return ErrDaemonRestarted
		}
		return nil
	}

	if fetchErr != nil && errors.Is(fetchErr, ErrDaemonRestarted) {
		return ErrDaemonRestarted
	}

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintf(w, "addr\t%s\n", rep.Addr)
	_, _ = fmt.Fprintf(w, "pid\t%d\n", rep.Pid)
	_, _ = fmt.Fprintf(w, "uptime\t%s\n", rep.Uptime)
	_, _ = fmt.Fprintf(w, "token age\t%s\n", rep.TokenAge)
	_, _ = fmt.Fprintf(w, "observing\t%s\n", observingLabel(disc.TranscriptDir))
	if rep.ConfigPath != "" {
		_, _ = fmt.Fprintf(w, "config\t%s\n", rep.ConfigPath)
	}
	if rep.StoreBackend != "" {
		_, _ = fmt.Fprintf(w, "store\t%s\n", rep.StoreBackend)
	}
	if len(rep.SinkTypes) > 0 {
		_, _ = fmt.Fprintf(w, "sinks\t%s\n", strings.Join(rep.SinkTypes, " "))
	}
	if len(rep.SourcesEnabled) > 0 {
		_, _ = fmt.Fprintf(w, "sources\t%s\n", strings.Join(rep.SourcesEnabled, " "))
	}
	if rep.ReaperWindow != "" {
		_, _ = fmt.Fprintf(w, "reaper\t%s\n", rep.ReaperWindow)
	}
	if rep.MaxShards > 0 {
		_, _ = fmt.Fprintf(w, "shards\t%d\n", rep.MaxShards)
	}
	if fetchErr != nil {
		_, _ = fmt.Fprintf(w, "sessions\tunavailable\n")
		_, _ = fmt.Fprintf(w, "nodes\tunavailable\n")
		return w.Flush()
	}
	_, _ = fmt.Fprintf(w, "sessions\t%d\n", rep.Sessions)
	_, _ = fmt.Fprintf(w, "nodes\t%d\n", rep.Nodes)
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

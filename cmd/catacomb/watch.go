package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

func newWatchCmd() *cobra.Command {
	var runFilter string
	var nodeTypes []string
	var tiers []string
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Stream live graph deltas from the catacomb daemon (SSE)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runWatch(
				cmd.Context(),
				clientDiscoveryPath(),
				runFilter, nodeTypes, tiers,
				http.DefaultClient,
				cmd.OutOrStdout(),
			)
		},
	}
	cmd.Flags().StringVar(&runFilter, "run", "", "filter to a specific run ID (empty = all)")
	cmd.Flags().StringArrayVar(&nodeTypes, "type", nil, "filter to node types (repeatable)")
	cmd.Flags().StringArrayVar(&tiers, "tier", nil, "filter to tiers (repeatable)")
	return cmd
}

func runWatch(
	ctx context.Context,
	discoveryPath string,
	runFilter string,
	nodeTypes []string,
	tiers []string,
	httpClient *http.Client,
	out io.Writer,
) error {
	disc, err := daemon.ReadDiscovery(discoveryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNoDaemon
		}
		return err
	}
	u := &url.URL{
		Scheme: "http",
		Host:   disc.Addr,
		Path:   "/v1/subscribe",
	}
	q := url.Values{}
	if runFilter != "" {
		q.Set("run", runFilter)
	}
	for _, t := range nodeTypes {
		q.Add("type", t)
	}
	for _, tier := range tiers {
		q.Add("tier", tier)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+disc.Token)

	resp, err := httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("catacomb watch: server returned %d", resp.StatusCode)
	}

	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		_, _ = fmt.Fprintln(out, strings.TrimPrefix(line, "data: "))
	}
	if ctx.Err() != nil {
		return nil
	}
	return sc.Err()
}

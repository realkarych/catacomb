package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

type upDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	startDaemon   func() error
	installHooks  func() error
	pollHealthz   func(ctx context.Context, addr string) error
	sessionCount  func(ctx context.Context, disc daemon.Discovery) (int, error)
	openBrowser   func(string) error
	replayDemo    func(ctx context.Context, disc daemon.Discovery) error
	after         func(time.Duration) <-chan time.Time
	discoveryPath string
	waitSeconds   int
	noOpen        bool
	noDemo        bool
}

func newUpCmd() *cobra.Command {
	var noOpen, noDemo, global bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the daemon (if needed), install hooks, and open the UI",
		RunE: func(cmd *cobra.Command, _ []string) error {
			discPath := daemon.DiscoveryPath()
			deps := upDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: discPath,
				startDaemon:   buildStartDaemon(discPath),
				installHooks:  buildInstallHooks(discPath, global),
				pollHealthz:   prodPollHealthz,
				sessionCount:  prodSessionCount,
				openBrowser:   openBrowser,
				replayDemo:    prodReplayDemo,
				after:         time.After,
				waitSeconds:   5,
				noOpen:        noOpen,
				noDemo:        noDemo,
			}
			return runUp(cmd.Context(), cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVar(&noOpen, "no-open", false, "print the URL without opening a browser")
	cmd.Flags().BoolVar(&noDemo, "no-demo", false, "skip the demo fallback if no session appears")
	cmd.Flags().BoolVar(&global, "global", false, "install hooks for all projects (~/.claude/settings.json) instead of the current directory")
	return cmd
}

func buildStartDaemon(discPath string) func() error {
	return func() error {
		exe, err := osExecutable()
		if err != nil {
			return fmt.Errorf("up: resolve executable: %w", err)
		}
		logPath := discPath + ".log"
		if mkErr := os.MkdirAll(filepath.Dir(logPath), 0o700); mkErr != nil {
			return fmt.Errorf("up: create run dir: %w", mkErr)
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("up: open daemon log: %w", err)
		}
		c := execCommand(exe, "daemon")
		c.Stdout = f
		c.Stderr = f
		if err := startCmd(c); err != nil {
			_ = f.Close()
			return fmt.Errorf("up: start daemon: %w", err)
		}
		_ = f.Close()
		return nil
	}
}

func buildInstallHooks(discPath string, global bool) func() error {
	return func() error {
		exe, err := osExecutable()
		if err != nil {
			return fmt.Errorf("up: resolve executable for hooks: %w", err)
		}
		path, err := settingsPath(false, global)
		if err != nil {
			return fmt.Errorf("up: resolve settings path: %w", err)
		}
		return installHooks(path, discPath, exe, false)
	}
}

var upPollHealthz = func(ctx context.Context, addr string) error {
	target := "http://" + addr + "/healthz"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDaemonUnreachable, err)
	}
	resp, err := statusHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrDaemonUnreachable, err)
	}
	_ = resp.Body.Close()
	return nil
}

func prodPollHealthz(ctx context.Context, addr string) error {
	return upPollHealthz(ctx, addr)
}

func prodSessionCount(ctx context.Context, disc daemon.Discovery) (int, error) {
	n, _, err := fetchSessionCounts(ctx, disc, statusHTTPClient)
	return n, err
}

func prodReplayDemo(ctx context.Context, disc daemon.Discovery) error {
	deps := demoDeps{
		readDiscovery: func(_ string) (daemon.Discovery, error) { return disc, nil },
		discoveryPath: "",
		transcript:    demoTranscript,
		httpClient:    demoHTTPClient,
	}
	return runDemo(ctx, io.Discard, deps)
}

func runUp(ctx context.Context, out io.Writer, deps upDeps) error {
	disc, err := deps.readDiscovery(deps.discoveryPath)
	needStart := err != nil && errors.Is(err, os.ErrNotExist)
	if err != nil && !needStart {
		return err
	}

	if needStart {
		if startErr := deps.startDaemon(); startErr != nil {
			return startErr
		}
		ready := false
		for attempt := 0; attempt < deps.waitSeconds; attempt++ {
			disc, err = deps.readDiscovery(deps.discoveryPath)
			if err == nil {
				if hzErr := deps.pollHealthz(ctx, disc.Addr); hzErr == nil {
					ready = true
					break
				}
			}
			<-deps.after(time.Second)
		}
		if !ready {
			return fmt.Errorf("%w: daemon did not become ready", ErrDaemonUnreachable)
		}
	} else {
		if pollErr := deps.pollHealthz(ctx, disc.Addr); pollErr != nil {
			return pollErr
		}
	}

	if err := deps.installHooks(); err != nil {
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

	if !deps.noOpen {
		if err := deps.openBrowser(rawURL); err != nil {
			return err
		}
	}

	if deps.noDemo {
		return nil
	}

	n, _ := deps.sessionCount(ctx, disc)
	if n > 0 {
		return nil
	}

	<-deps.after(time.Duration(deps.waitSeconds) * time.Second)

	n, _ = deps.sessionCount(ctx, disc)
	if n > 0 {
		return nil
	}

	if _, err := fmt.Fprintln(out, "No live session detected — replaying a demo. (catacomb demo to replay anytime)"); err != nil {
		return err
	}
	return deps.replayDemo(ctx, disc)
}

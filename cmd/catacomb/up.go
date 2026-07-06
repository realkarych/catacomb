package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

var startCmd = func(c *exec.Cmd) error { return c.Start() }

type upDeps struct {
	readDiscovery func(string) (daemon.Discovery, error)
	startDaemon   func() error
	installHooks  func() error
	pollHealthz   func(ctx context.Context, addr string) error
	sessionCount  func(ctx context.Context, disc daemon.Discovery) (int, error)
	replayDemo    func(ctx context.Context, disc daemon.Discovery) error
	after         func(time.Duration) <-chan time.Time
	discoveryPath string
	waitSeconds   int
	noDemo        bool
	history       bool
	projectsDir   string
	daemonDone    <-chan error
}

func newUpCmd() *cobra.Command {
	var foreground bool
	var noDemo, global, history bool
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the daemon (if needed) and install the Claude Code hooks",
		Long: `Start the daemon if it is not already running, install the Claude Code
hooks, and print the daemon address.

By default up observes only sessions started in the current directory, and
only live activity. Use --global to install hooks for every project, and
--history to load sessions you have already run.

If a daemon is already running, --history does not restart it; up prints the
exact command to restart it with history enabled.`,
		Example: `  # observe the current project, live only
  catacomb up

  # observe every project (all live sessions)
  catacomb up --global

  # also load past sessions from ~/.claude/projects
  catacomb up --global --history`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			discPath := clientDiscoveryPath()
			var transcriptDir string
			if history {
				projects, err := claudeProjectsDir()
				if err != nil {
					return err
				}
				transcriptDir = projects
			}
			var daemonDone <-chan error
			runCtx := cmd.Context()
			if foreground {
				home, herr := osUserHomeDir()
				if herr != nil {
					return fmt.Errorf("up: resolve home: %w", herr)
				}
				cfg, cerr := resolveConfig(daemonFlags{}, os.ReadFile, os.LookupEnv, home)
				if cerr != nil {
					return cerr
				}
				if transcriptDir != "" {
					enabled := true
					cfg.Sources.JSONL.Enabled = &enabled
					cfg.Sources.JSONL.TranscriptDir = transcriptDir
				}
				fgParams := daemonParams{
					store:              cfg.Store,
					sinks:              cfg.Sinks,
					sources:            cfg.Sources,
					discoveryPath:      resolveDiscovery(cfg.Daemon.Discovery),
					configPath:         configFilePath(daemonFlags{}, os.LookupEnv, home),
					reaperWindow:       time.Duration(cfg.Daemon.ReaperWindow),
					maxShards:          cfg.Daemon.MaxShards,
					allowPayloadAccess: cfg.Daemon.AllowPayloadAccess,
					allowAnnotations:   cfg.Daemon.AllowAnnotations,
				}
				fgCtx, fgStop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
				defer fgStop()
				done := make(chan error, 1)
				go func() { done <- fgRunDaemon(fgCtx, defaultDaemonDeps(), fgParams) }()
				daemonDone = done
				discPath = fgParams.discoveryPath
				noDemo = true
				runCtx = fgCtx
			}
			var startDaemonFn func() error
			if foreground {
				startDaemonFn = func() error { return nil }
			} else {
				startDaemonFn = buildStartDaemon(discPath, transcriptDir, "")
			}
			deps := upDeps{
				readDiscovery: daemon.ReadDiscovery,
				discoveryPath: discPath,
				startDaemon:   startDaemonFn,
				installHooks:  buildInstallHooks(discPath, global),
				pollHealthz:   prodPollHealthz,
				sessionCount:  prodSessionCount,
				replayDemo:    prodReplayDemo,
				after:         time.After,
				waitSeconds:   5,
				noDemo:        noDemo,
				history:       history,
				projectsDir:   transcriptDir,
				daemonDone:    daemonDone,
			}
			return runUp(runCtx, cmd.OutOrStdout(), deps)
		},
	}
	cmd.Flags().BoolVar(&noDemo, "no-demo", false, "skip the demo fallback if no session appears")
	cmd.Flags().BoolVar(&global, "global", false, "install hooks for all projects (~/.claude/settings.json) instead of the current directory")
	cmd.Flags().BoolVar(&history, "history", false, "tail ~/.claude/projects so past sessions are ingested")
	cmd.Flags().BoolVarP(&foreground, "foreground", "F", false, "run the daemon attached in the current process (no fork; Ctrl-C stops; for debugging)")
	return cmd
}

func claudeProjectsDir() (string, error) {
	home, err := osUserHomeDir()
	if err != nil {
		return "", fmt.Errorf("up: resolve home: %w", err)
	}
	return filepath.Join(home, ".claude", "projects"), nil
}

func buildStartDaemon(discPath, transcriptDir, configPath string) func() error {
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
		args := []string{"daemon"}
		if transcriptDir != "" {
			args = append(args, "--transcript-dir", transcriptDir)
		}
		if configPath != "" {
			args = append(args, "--config", configPath)
		}
		c := execCommand(exe, args...)
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

var fgRunDaemon = runDaemonWith

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
		if deps.history {
			if err := reportHistoryScope(out, disc, deps.projectsDir); err != nil {
				return err
			}
		}
	}

	if err := deps.installHooks(); err != nil {
		return err
	}

	if _, err := fmt.Fprintf(out, "daemon ready at http://%s\n", disc.Addr); err != nil {
		return err
	}

	if deps.noDemo {
		if deps.daemonDone != nil {
			return <-deps.daemonDone
		}
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

func reportHistoryScope(out io.Writer, disc daemon.Discovery, projectsDir string) error {
	if projectsDir != "" && disc.TranscriptDir == projectsDir {
		_, err := fmt.Fprintf(out, "daemon already observing all history (%s)\n", projectsDir)
		return err
	}
	_, err := fmt.Fprintf(out, "daemon already running (pid %d); --history applies only when starting a fresh daemon.\nto tail all history, restart it:\n\n  %s\n\n", disc.Pid, restartCommand(disc, projectsDir))
	return err
}

func restartCommand(disc daemon.Discovery, projectsDir string) string {
	var b strings.Builder
	if disc.Pid != 0 {
		_, _ = fmt.Fprintf(&b, "kill %d && ", disc.Pid)
	}
	_, _ = fmt.Fprintf(&b, "catacomb daemon --transcript-dir %q", projectsDir)
	if disc.DBPath != "" {
		_, _ = fmt.Fprintf(&b, " --db %q", disc.DBPath)
	}
	if disc.AllowPayloadAccess {
		b.WriteString(" --allow-payload-access")
	}
	return b.String()
}

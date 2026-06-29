package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/realkarych/catacomb/daemon"
)

const (
	downStopInterval = 100 * time.Millisecond
	downStopAttempts = 50
)

var (
	downSignal = signalProcess
	downSleep  = time.Sleep
)

func signalProcess(pid int, sig syscall.Signal) error {
	p, _ := os.FindProcess(pid)
	return p.Signal(sig)
}

func waitGone(pid int) bool {
	for i := 0; i < downStopAttempts; i++ {
		if err := downSignal(pid, syscall.Signal(0)); err != nil {
			return true
		}
		downSleep(downStopInterval)
	}
	return false
}

type downOpts struct {
	uninstall bool
	purge     bool
	all       bool
	dbPaths   []string
	force     bool
	dryRun    bool
	yes       bool
	asJSON    bool
}

type downReport struct {
	DaemonStopped    bool     `json:"daemon_stopped"`
	DiscoveryRemoved bool     `json:"discovery_removed"`
	HooksRemoved     []string `json:"hooks_removed"`
	DatabasesRemoved []string `json:"databases_removed"`
	StateRemoved     []string `json:"state_removed"`
	Warnings         []string `json:"warnings,omitempty"`
	DryRun           bool     `json:"dry_run"`
}

var (
	downReadDiscovery = daemon.ReadDiscovery
	downRemove        = os.Remove
	downRemoveAll     = os.RemoveAll
	downStat          = os.Stat
	downHookTargets   = defaultHookTargets
)

func dbTargets(opts downOpts, disc daemon.Discovery, haveDisc bool) []string {
	var targets []string
	if haveDisc && disc.DBPath != "" {
		targets = append(targets, disc.DBPath)
	}
	targets = append(targets, opts.dbPaths...)
	return targets
}

func removeWithSiblings(db string) (bool, error) {
	removedAny := false
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := downRemove(db + suffix)
		if err == nil {
			removedAny = true
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("down: remove %s: %w", db+suffix, err)
		}
	}
	return removedAny, nil
}

func stateTargets(discoveryPath string) ([]string, error) {
	home, err := osUserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("down: home: %w", err)
	}
	return []string{discoveryPath + ".log", filepath.Join(home, ".catacomb")}, nil
}

func purgeLocal(opts downOpts, disc daemon.Discovery, haveDisc bool, discoveryPath string) ([]string, []string, []string, error) {
	var dbs, state, warnings []string

	targets := dbTargets(opts, disc, haveDisc)
	if len(targets) == 0 {
		warnings = append(warnings, "no database path known; other databases may remain where you ran catacomb (pass --db)")
	}
	for _, db := range targets {
		removed, err := removeWithSiblings(db)
		if err != nil {
			return nil, nil, nil, err
		}
		if removed {
			dbs = append(dbs, db)
		}
	}

	st, err := stateTargets(discoveryPath)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, path := range st {
		err := downRemoveAll(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, nil, nil, fmt.Errorf("down: remove %s: %w", path, err)
		}
		if _, statErr := downStat(path); os.IsNotExist(statErr) {
			state = append(state, path)
		}
	}
	return dbs, state, warnings, nil
}

func defaultHookTargets() ([]string, error) {
	global, err := settingsPath(false, true)
	if err != nil {
		return nil, err
	}
	return []string{filepath.Join(".claude", "settings.json"), global}, nil
}

func uninstallHooks() ([]string, error) {
	targets, err := downHookTargets()
	if err != nil {
		return nil, err
	}
	exe, err := osExecutable()
	if err != nil {
		return nil, fmt.Errorf("down: executable: %w", err)
	}
	var removed []string
	for _, path := range targets {
		if _, statErr := downStat(path); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, fmt.Errorf("down: stat %s: %w", path, statErr)
		}
		if err := installHooks(path, daemon.DiscoveryPath(), exe, true); err != nil {
			return nil, err
		}
		removed = append(removed, path)
	}
	return removed, nil
}

var (
	downIsTerminal = defaultIsTerminal
	downConfirm    = func(out io.Writer, prompt string) (bool, error) {
		return readConfirm(os.Stdin, out, prompt)
	}
)

func defaultIsTerminal() bool {
	return isatty.IsTerminal(os.Stdin.Fd())
}

func readConfirm(in io.Reader, out io.Writer, prompt string) (bool, error) {
	_, _ = fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

func confirmDestructive(out io.Writer, opts downOpts) (bool, error) {
	if !opts.purge || opts.yes || opts.dryRun {
		return true, nil
	}
	if !downIsTerminal() {
		return false, ErrConfirmationRequired
	}
	return downConfirm(out, "This permanently deletes catacomb data. Continue? [y/N] ")
}

func newDownCmd() *cobra.Command {
	var opts downOpts
	cmd := &cobra.Command{
		Use:   "down",
		Short: "Stop the daemon and optionally remove catacomb's artifacts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runDown(cmd.OutOrStdout(), opts, daemon.DiscoveryPath())
		},
	}
	cmd.Flags().BoolVar(&opts.uninstall, "uninstall", false, "also remove catacomb hook entries from .claude/settings.json")
	cmd.Flags().BoolVar(&opts.purge, "purge", false, "also delete the local database and ~/.catacomb state")
	cmd.Flags().BoolVar(&opts.all, "all", false, "shorthand for --uninstall --purge")
	cmd.Flags().StringArrayVar(&opts.dbPaths, "db", nil, "additional database file to delete under --purge (repeatable)")
	cmd.Flags().BoolVar(&opts.force, "force", false, "escalate a stuck daemon stop to SIGKILL")
	cmd.Flags().BoolVar(&opts.dryRun, "dry-run", false, "print what would be done without changing anything")
	cmd.Flags().BoolVarP(&opts.yes, "yes", "y", false, "skip the confirmation prompt (required in non-interactive shells)")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "output a machine-readable report")
	return cmd
}

func runDown(out io.Writer, opts downOpts, discoveryPath string) error {
	if opts.all {
		opts.uninstall = true
		opts.purge = true
	}
	rep := downReport{DryRun: opts.dryRun}

	proceed, cerr := confirmDestructive(out, opts)
	if cerr != nil {
		return cerr
	}
	if !proceed {
		_, _ = fmt.Fprintln(out, "aborted")
		return nil
	}

	disc, derr := downReadDiscovery(discoveryPath)
	haveDisc := derr == nil
	if derr != nil && !errors.Is(derr, os.ErrNotExist) {
		return derr
	}

	if opts.dryRun {
		return planDown(out, opts, disc, haveDisc, discoveryPath)
	}

	if !haveDisc {
		_, _ = fmt.Fprintln(out, "no daemon running")
	} else {
		_, serr := stopDaemon(disc.Pid, opts.force)
		if serr != nil {
			return serr
		}
		rep.DaemonStopped = true
		if err := downRemove(discoveryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("down: remove discovery: %w", err)
		}
		rep.DiscoveryRemoved = true
	}

	if opts.uninstall {
		removed, err := uninstallHooks()
		if err != nil {
			return err
		}
		rep.HooksRemoved = removed
	}

	if opts.purge {
		dbs, state, warns, err := purgeLocal(opts, disc, haveDisc, discoveryPath)
		if err != nil {
			return err
		}
		rep.DatabasesRemoved = dbs
		rep.StateRemoved = state
		rep.Warnings = warns
	}

	return writeDownReport(out, rep, opts.asJSON)
}

func planDown(out io.Writer, opts downOpts, disc daemon.Discovery, haveDisc bool, discoveryPath string) error {
	rep := downReport{DryRun: true}
	if haveDisc {
		rep.DaemonStopped = true
		rep.DiscoveryRemoved = true
	}
	if opts.uninstall {
		if targets, err := downHookTargets(); err == nil {
			for _, path := range targets {
				if _, statErr := downStat(path); statErr == nil {
					rep.HooksRemoved = append(rep.HooksRemoved, path)
				}
			}
		}
	}
	if opts.purge {
		rep.DatabasesRemoved = dbTargets(opts, disc, haveDisc)
		if st, err := stateTargets(discoveryPath); err == nil {
			rep.StateRemoved = st
		}
		if len(dbTargets(opts, disc, haveDisc)) == 0 {
			rep.Warnings = append(rep.Warnings, "no database path known; pass --db")
		}
	}
	return writeDownReport(out, rep, opts.asJSON)
}

func writeDownReport(out io.Writer, rep downReport, asJSON bool) error {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	stopMsg, verb := "daemon stopped", "removed"
	if rep.DryRun {
		stopMsg, verb = "would stop daemon", "would remove"
	}
	if rep.DaemonStopped {
		_, _ = fmt.Fprintln(out, stopMsg)
	}
	if rep.DiscoveryRemoved && rep.DryRun {
		_, _ = fmt.Fprintln(out, "would remove discovery file")
	}
	for _, h := range rep.HooksRemoved {
		_, _ = fmt.Fprintf(out, "%s hooks: %s\n", verb, h)
	}
	for _, d := range rep.DatabasesRemoved {
		_, _ = fmt.Fprintf(out, "%s database: %s\n", verb, d)
	}
	for _, s := range rep.StateRemoved {
		_, _ = fmt.Fprintf(out, "%s state: %s\n", verb, s)
	}
	for _, w := range rep.Warnings {
		_, _ = fmt.Fprintf(out, "warning: %s\n", w)
	}
	return nil
}

func stopDaemon(pid int, force bool) (bool, error) {
	if pid <= 0 {
		return false, nil
	}
	if err := downSignal(pid, syscall.Signal(0)); err != nil {
		return false, nil
	}
	if err := downSignal(pid, syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDaemonStop, err)
	}
	if waitGone(pid) {
		return true, nil
	}
	if !force {
		return false, ErrDaemonStop
	}
	if err := downSignal(pid, syscall.SIGKILL); err != nil {
		return false, fmt.Errorf("%w: %w", ErrDaemonStop, err)
	}
	if waitGone(pid) {
		return true, nil
	}
	return false, ErrDaemonStop
}

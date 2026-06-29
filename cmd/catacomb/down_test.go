package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func swapSignal(t *testing.T, fn func(int, syscall.Signal) error) {
	t.Helper()
	orig := downSignal
	downSignal = fn
	t.Cleanup(func() { downSignal = orig })
}

func swapSleepNoop(t *testing.T) {
	t.Helper()
	orig := downSleep
	downSleep = func(_ time.Duration) {}
	t.Cleanup(func() { downSleep = orig })
}

func TestSignalProcessAliveAndDead(t *testing.T) {
	require.NoError(t, signalProcess(os.Getpid(), syscall.Signal(0)))
	assert.Error(t, signalProcess(1<<30, syscall.Signal(0)))
}

func TestStopDaemonNoPid(t *testing.T) {
	stopped, err := stopDaemon(0, false)
	require.NoError(t, err)
	assert.False(t, stopped)
}

func TestStopDaemonNotAlive(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	stopped, err := stopDaemon(42, false)
	require.NoError(t, err)
	assert.False(t, stopped)
}

func TestStopDaemonGraceful(t *testing.T) {
	swapSleepNoop(t)
	n := 0
	swapSignal(t, func(_ int, _ syscall.Signal) error {
		n++
		if n >= 3 {
			return errors.New("gone")
		}
		return nil
	})
	stopped, err := stopDaemon(42, false)
	require.NoError(t, err)
	assert.True(t, stopped)
}

func TestStopDaemonStuckNoForce(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, _ syscall.Signal) error { return nil })
	stopped, err := stopDaemon(42, false)
	assert.False(t, stopped)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestStopDaemonForceKill(t *testing.T) {
	swapSleepNoop(t)
	killed := false
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			killed = true
			return nil
		}
		if killed {
			return errors.New("gone")
		}
		return nil
	})
	stopped, err := stopDaemon(42, true)
	require.NoError(t, err)
	assert.True(t, stopped)
}

func TestStopDaemonSigtermError(t *testing.T) {
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			return errors.New("eperm")
		}
		return nil
	})
	_, err := stopDaemon(42, false)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestStopDaemonForceKillError(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			return errors.New("eperm")
		}
		return nil
	})
	_, err := stopDaemon(42, true)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestStopDaemonForceStillAlive(t *testing.T) {
	swapSleepNoop(t)
	swapSignal(t, func(_ int, _ syscall.Signal) error { return nil })
	stopped, err := stopDaemon(42, true)
	assert.False(t, stopped)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func writeDisc(t *testing.T, pid int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{
		Addr: "127.0.0.1:1", Token: "tok", Pid: pid, DBPath: filepath.Join(dir, "catacomb.db"),
	}))
	return path, dir
}

func TestRunDownNoDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{}, path))
	assert.Contains(t, out.String(), "no daemon")
}

func TestRunDownStopsAndRemovesDiscovery(t *testing.T) {
	swapSleepNoop(t)
	sent := false
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			sent = true
			return nil
		}
		if sent {
			return errors.New("gone")
		}
		return nil
	})
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{}, path))
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
	assert.Contains(t, out.String(), "stopped")
}

func TestRunDownStaleDiscoveryRemoved(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{}, path))
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr))
}

func TestRunDownReadDiscoveryError(t *testing.T) {
	orig := downReadDiscovery
	downReadDiscovery = func(string) (daemon.Discovery, error) { return daemon.Discovery{}, errors.New("boom") }
	t.Cleanup(func() { downReadDiscovery = orig })
	err := runDown(io.Discard, downOpts{}, "/whatever")
	assert.Error(t, err)
}

func TestRunDownStopError(t *testing.T) {
	swapSignal(t, func(_ int, sig syscall.Signal) error {
		if sig == syscall.SIGTERM {
			return errors.New("eperm")
		}
		return nil
	})
	path, _ := writeDisc(t, 4242)
	err := runDown(io.Discard, downOpts{}, path)
	assert.ErrorIs(t, err, ErrDaemonStop)
}

func TestRunDownJSON(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{asJSON: true}, path))
	var rep downReport
	require.NoError(t, json.Unmarshal([]byte(out.String()), &rep))
	assert.True(t, rep.DiscoveryRemoved)
}

func TestNewDownCmdRegistered(t *testing.T) {
	cmd := newDownCmd()
	assert.Equal(t, "down", cmd.Name())
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(t.TempDir(), "none.json"))
	cmd.SetOut(io.Discard)
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())
}

func TestRunDownRemoveError(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	orig := downRemove
	downRemove = func(string) error { return errors.New("permission denied") }
	t.Cleanup(func() { downRemove = orig })
	err := runDown(io.Discard, downOpts{}, path)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "down: remove discovery")
}

func TestRunDownAllFlag(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	path := filepath.Join(t.TempDir(), "absent.json")
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return nil, nil }
	t.Cleanup(func() { downHookTargets = orig })
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{all: true, yes: true}, path))
	assert.Contains(t, out.String(), "no daemon")
}

func TestRunDownDryRun(t *testing.T) {
	swapSignal(t, func(int, syscall.Signal) error { return errors.New("dead") })
	path, _ := writeDisc(t, 4242)
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{dryRun: true}, path))
	assert.Contains(t, out.String(), "would")
}

func TestWriteDownReportLoops(t *testing.T) {
	rep := downReport{
		HooksRemoved:     []string{"hook1"},
		DatabasesRemoved: []string{"db1"},
		StateRemoved:     []string{"state1"},
		Warnings:         []string{"warn1"},
	}
	var out strings.Builder
	require.NoError(t, writeDownReport(&out, rep, false))
	s := out.String()
	assert.Contains(t, s, "hooks: hook1")
	assert.Contains(t, s, "database: db1")
	assert.Contains(t, s, "state: state1")
	assert.Contains(t, s, "warning: warn1")
}

func TestUninstallHooksPrunesExistingOnly(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj.json")
	glob := filepath.Join(t.TempDir(), "absent-global.json")
	require.NoError(t, installHooks(proj, "/run/d.json", "/usr/bin/catacomb", false))

	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{proj, glob}, nil }
	t.Cleanup(func() { downHookTargets = orig })

	removed, err := uninstallHooks()
	require.NoError(t, err)
	assert.Equal(t, []string{proj}, removed)

	_, statErr := os.Stat(glob)
	assert.True(t, os.IsNotExist(statErr), "absent settings file must not be created")
}

func TestRunDownUninstall(t *testing.T) {
	proj := filepath.Join(t.TempDir(), "proj.json")
	require.NoError(t, installHooks(proj, "/run/d.json", "/usr/bin/catacomb", false))
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{proj}, nil }
	t.Cleanup(func() { downHookTargets = orig })

	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{uninstall: true}, path))
	assert.Contains(t, out.String(), "removed hooks")
}

func TestUninstallHooksTargetError(t *testing.T) {
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return nil, errors.New("no home") }
	t.Cleanup(func() { downHookTargets = orig })
	_, err := uninstallHooks()
	assert.Error(t, err)
}

func TestDefaultHookTargets(t *testing.T) {
	got, err := downHookTargets()
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Contains(t, got[0], filepath.Join(".claude", "settings.json"))
	assert.Contains(t, got[1], filepath.Join(".claude", "settings.json"))
}

func TestUninstallHooksExeError(t *testing.T) {
	orig := osExecutable
	t.Cleanup(func() { osExecutable = orig })
	osExecutable = func() (string, error) { return "", errors.New("exe") }
	_, err := uninstallHooks()
	assert.Error(t, err)
}

func TestUninstallHooksInstallError(t *testing.T) {
	malformed := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(malformed, []byte("{not json}"), 0o644))
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{malformed}, nil }
	t.Cleanup(func() { downHookTargets = orig })
	_, err := uninstallHooks()
	assert.Error(t, err)
}

func TestUninstallHooksStatError(t *testing.T) {
	origT := downHookTargets
	downHookTargets = func() ([]string, error) { return []string{"/x/settings.json"}, nil }
	t.Cleanup(func() { downHookTargets = origT })
	origS := downStat
	downStat = func(string) (os.FileInfo, error) { return nil, errors.New("perm denied") }
	t.Cleanup(func() { downStat = origS })
	_, err := uninstallHooks()
	assert.Error(t, err)
}

func TestRunDownUninstallError(t *testing.T) {
	orig := downHookTargets
	downHookTargets = func() ([]string, error) { return nil, errors.New("boom") }
	t.Cleanup(func() { downHookTargets = orig })
	path := filepath.Join(t.TempDir(), "absent.json")
	err := runDown(io.Discard, downOpts{uninstall: true}, path)
	assert.Error(t, err)
}

func TestDefaultHookTargetsHomeError(t *testing.T) {
	old := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = old })
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	_, err := defaultHookTargets()
	assert.Error(t, err)
}

func swapTTY(t *testing.T, v bool) {
	t.Helper()
	orig := downIsTerminal
	downIsTerminal = func() bool { return v }
	t.Cleanup(func() { downIsTerminal = orig })
}

func TestConfirmYesFlagBypasses(t *testing.T) {
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true, yes: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmDryRunBypasses(t *testing.T) {
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true, dryRun: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmNonDestructiveBypasses(t *testing.T) {
	ok, err := confirmDestructive(io.Discard, downOpts{uninstall: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmNonTTYRefuses(t *testing.T) {
	swapTTY(t, false)
	_, err := confirmDestructive(io.Discard, downOpts{purge: true})
	assert.ErrorIs(t, err, ErrConfirmationRequired)
}

func TestConfirmTTYPromptYes(t *testing.T) {
	swapTTY(t, true)
	orig := downConfirm
	downConfirm = func(io.Writer, string) (bool, error) { return true, nil }
	t.Cleanup(func() { downConfirm = orig })
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true})
	require.NoError(t, err)
	assert.True(t, ok)
}

func TestConfirmTTYPromptNo(t *testing.T) {
	swapTTY(t, true)
	orig := downConfirm
	downConfirm = func(io.Writer, string) (bool, error) { return false, nil }
	t.Cleanup(func() { downConfirm = orig })
	ok, err := confirmDestructive(io.Discard, downOpts{purge: true})
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestReadConfirmVariants(t *testing.T) {
	for _, in := range []string{"y\n", "yes\n", "Y\n"} {
		ok, err := readConfirm(strings.NewReader(in), io.Discard, "p? ")
		require.NoError(t, err)
		assert.True(t, ok)
	}
	ok, err := readConfirm(strings.NewReader("n\n"), io.Discard, "p? ")
	require.NoError(t, err)
	assert.False(t, ok)
	ok, err = readConfirm(strings.NewReader(""), io.Discard, "p? ")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestDefaultIsTerminalPipeIsNotTTY(t *testing.T) {
	r, _, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	assert.False(t, defaultIsTerminal())
}

func TestReadConfirmReaderError(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	_ = w.Close()
	_ = r.Close()
	_, err = readConfirm(r, io.Discard, "p? ")
	assert.Error(t, err)
}

func TestDownConfirmReadsStdin(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = orig })
	_, _ = w.WriteString("y\n")
	_ = w.Close()
	ok, err := downConfirm(io.Discard, "p? ")
	require.NoError(t, err)
	assert.True(t, ok)
}

func touch(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("x"), 0o600))
}

func TestPurgeRemovesDBWithSiblings(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	dir := t.TempDir()
	db := filepath.Join(dir, "catacomb.db")
	touch(t, db)
	touch(t, db+"-wal")
	touch(t, db+"-shm")
	disc := daemon.Discovery{DBPath: db}

	dbs, _, _, err := purgeLocal(downOpts{purge: true, yes: true}, disc, true, filepath.Join(dir, "daemon.json"))
	require.NoError(t, err)
	assert.Contains(t, dbs, db)
	for _, suffix := range []string{"", "-wal", "-shm"} {
		_, statErr := os.Stat(db + suffix)
		assert.True(t, os.IsNotExist(statErr))
	}
}

func TestPurgeExtraDBPaths(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	dir := t.TempDir()
	extra := filepath.Join(dir, "other.db")
	touch(t, extra)
	dbs, _, _, err := purgeLocal(downOpts{purge: true, yes: true, dbPaths: []string{extra}}, daemon.Discovery{}, false, filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	assert.Contains(t, dbs, extra)
}

func TestPurgeWarnsWhenNoKnownDB(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	dir := t.TempDir()
	_, _, warns, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{}, false, filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	require.NotEmpty(t, warns)
	assert.Contains(t, warns[0], "--db")
}

func TestPurgeRemovesStateAndLog(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })

	catacombDir := filepath.Join(home, ".catacomb")
	require.NoError(t, os.MkdirAll(catacombDir, 0o700))
	discPath := filepath.Join(t.TempDir(), "daemon.json")
	touch(t, discPath+".log")

	_, state, _, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{DBPath: "/nope"}, true, discPath)
	require.NoError(t, err)
	assert.Contains(t, state, catacombDir)
	_, statErr := os.Stat(catacombDir)
	assert.True(t, os.IsNotExist(statErr))
}

func TestPurgeHomeError(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = origHome })
	_, _, _, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{}, false, "/x/d.json")
	assert.Error(t, err)
}

func TestRunDownPurgeAbortsOnNo(t *testing.T) {
	swapTTY(t, true)
	orig := downConfirm
	downConfirm = func(io.Writer, string) (bool, error) { return false, nil }
	t.Cleanup(func() { downConfirm = orig })
	path := filepath.Join(t.TempDir(), "absent.json")
	var out strings.Builder
	require.NoError(t, runDown(&out, downOpts{purge: true}, path))
	assert.Contains(t, out.String(), "aborted")
}

func TestRunDownPurgeNonTTY(t *testing.T) {
	swapTTY(t, false)
	path := filepath.Join(t.TempDir(), "absent.json")
	err := runDown(io.Discard, downOpts{purge: true}, path)
	assert.ErrorIs(t, err, ErrConfirmationRequired)
}

func TestRemoveWithSiblingsRemoveError(t *testing.T) {
	orig := downRemove
	downRemove = func(string) error { return errors.New("eperm") }
	t.Cleanup(func() { downRemove = orig })
	_, err := removeWithSiblings("/whatever.db")
	assert.Error(t, err)
}

func TestPurgeDBRemoveError(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	origRemove := downRemove
	downRemove = func(string) error { return errors.New("eperm") }
	t.Cleanup(func() { downRemove = origRemove })
	dir := t.TempDir()
	db := filepath.Join(dir, "catacomb.db")
	touch(t, db)
	disc := daemon.Discovery{DBPath: db}
	_, _, _, err := purgeLocal(downOpts{purge: true, yes: true}, disc, true, filepath.Join(dir, "d.json"))
	assert.Error(t, err)
}

func TestPurgeRemoveAllNotExistContinues(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	origRA := downRemoveAll
	downRemoveAll = func(string) error { return os.ErrNotExist }
	t.Cleanup(func() { downRemoveAll = origRA })
	dir := t.TempDir()
	_, state, _, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{}, false, filepath.Join(dir, "d.json"))
	require.NoError(t, err)
	assert.Empty(t, state)
}

func TestPurgeRemoveAllError(t *testing.T) {
	home := t.TempDir()
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return home, nil }
	t.Cleanup(func() { osUserHomeDir = origHome })
	origRA := downRemoveAll
	downRemoveAll = func(string) error { return errors.New("eperm") }
	t.Cleanup(func() { downRemoveAll = origRA })
	dir := t.TempDir()
	_, _, _, err := purgeLocal(downOpts{purge: true, yes: true}, daemon.Discovery{}, false, filepath.Join(dir, "d.json"))
	assert.Error(t, err)
}

func TestRunDownPurgeError(t *testing.T) {
	origHome := osUserHomeDir
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	t.Cleanup(func() { osUserHomeDir = origHome })
	path := filepath.Join(t.TempDir(), "absent.json")
	err := runDown(io.Discard, downOpts{purge: true, yes: true}, path)
	assert.Error(t, err)
}

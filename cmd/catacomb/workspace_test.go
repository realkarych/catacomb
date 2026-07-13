package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
)

type execCapture struct {
	names []string
	cmds  []*exec.Cmd
}

func stubWorkspaceExec(t *testing.T, exits map[string]int) *execCapture {
	t.Helper()
	t.Setenv("GO_HELPER_WS", "1")
	cap := &execCapture{}
	orig := execCommandContext
	execCommandContext = func(ctx context.Context, name string, _ ...string) *exec.Cmd {
		c := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperWorkspaceExit", "--", strconv.Itoa(exits[name]))
		cap.names = append(cap.names, name)
		cap.cmds = append(cap.cmds, c)
		return c
	}
	t.Cleanup(func() { execCommandContext = orig })
	return cap
}

func TestHelperWorkspaceExit(t *testing.T) {
	if os.Getenv("GO_HELPER_WS") != "1" {
		t.Skip("helper process")
	}
	code := 0
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			code, _ = strconv.Atoi(os.Args[i+1])
		}
	}
	os.Exit(code)
}

func wsCell(ws *bench.Workspace) bench.Cell {
	return bench.Cell{RunID: "bench-b-t-v-r1", Task: bench.Task{ID: "t", Workspace: ws}}
}

func TestSetupWorkspaceFreshDirPerCall(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}})
	dir1, code, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	require.Zero(t, code)
	dir2, _, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	require.NotEqual(t, dir1, dir2)
	for _, d := range []string{dir1, dir2} {
		require.DirExists(t, d)
		require.Equal(t, base, filepath.Dir(d))
		require.True(t, strings.HasPrefix(filepath.Base(d), "bench-b-t-v-r1-"))
	}
	require.Equal(t, []string{"ws-cmd", "ws-cmd"}, cap.names)
	require.Equal(t, dir1, cap.cmds[0].Dir)
	require.Equal(t, dir2, cap.cmds[1].Dir)
}

func TestSetupWorkspacePatchEnvOnlyWhenDeclared(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	with := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Patch: "fix.patch", PatchAbs: "/abs/fix.patch"})
	_, _, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, with, workspaceOpts{baseDir: base})
	require.True(t, ok)
	require.Contains(t, cap.cmds[0].Env, "CATACOMB_PATCH=/abs/fix.patch")
	without := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}})
	_, _, ok = setupWorkspace(t.Context(), io.Discard, io.Discard, without, workspaceOpts{baseDir: base})
	require.True(t, ok)
	for _, kv := range cap.cmds[1].Env {
		require.False(t, strings.HasPrefix(kv, "CATACOMB_PATCH="))
	}
}

func TestSetupWorkspaceCmdFailureReturnsDirForCleanup(t *testing.T) {
	base := t.TempDir()
	stubWorkspaceExec(t, map[string]int{"ws-cmd": 3})
	dir, code, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}}), workspaceOpts{baseDir: base})
	require.False(t, ok)
	require.Equal(t, 3, code)
	require.NotEmpty(t, dir)
}

func TestSetupWorkspaceMkdirTempFailure(t *testing.T) {
	stubWorkspaceExec(t, nil)
	orig := mkdirTempFn
	mkdirTempFn = func(string, string) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { mkdirTempFn = orig })
	var errBuf bytes.Buffer
	dir, code, ok := setupWorkspace(t.Context(), io.Discard, &errBuf, wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}}), workspaceOpts{})
	require.False(t, ok)
	require.Empty(t, dir)
	require.Equal(t, -1, code)
	require.Contains(t, errBuf.String(), "workspace")
}

func TestCleanupWorkspaceTeardownRunsAndDirRemoved(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd", "arg1"}})
	dir, _, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	notes := cleanupWorkspace(io.Discard, cell, dir, false)
	require.Empty(t, notes)
	require.Equal(t, []string{"ws-cmd", "td-cmd"}, cap.names)
	require.Equal(t, dir, cap.cmds[1].Dir)
	require.NoDirExists(t, dir)
}

func TestCleanupWorkspaceKeepSkipsRemovalTeardownStillRuns(t *testing.T) {
	base := t.TempDir()
	cap := stubWorkspaceExec(t, nil)
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}})
	dir, _, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base, keep: true})
	require.True(t, ok)
	var errBuf bytes.Buffer
	notes := cleanupWorkspace(&errBuf, cell, dir, true)
	require.Empty(t, notes)
	require.DirExists(t, dir)
	require.Len(t, cap.names, 2)
	require.Contains(t, errBuf.String(), "workspace kept: "+dir)
}

func TestCleanupWorkspaceFailureNotes(t *testing.T) {
	base := t.TempDir()
	stubWorkspaceExec(t, map[string]int{"td-cmd": 5})
	cell := wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}, Teardown: []string{"td-cmd"}})
	dir, _, ok := setupWorkspace(t.Context(), io.Discard, io.Discard, cell, workspaceOpts{baseDir: base})
	require.True(t, ok)
	origRM := removeAllFn
	removeAllFn = func(string) error { return errors.New("busy mount") }
	t.Cleanup(func() { removeAllFn = origRM })
	notes := cleanupWorkspace(io.Discard, cell, dir, false)
	require.Len(t, notes, 2)
	require.Contains(t, notes[0], "workspace teardown")
	require.Contains(t, notes[1], "workspace remove")
}

func TestCleanupWorkspaceEmptyDirNoop(t *testing.T) {
	require.Empty(t, cleanupWorkspace(io.Discard, bench.Cell{}, "", false))
}

func TestCleanupWorkspaceNoTeardownRemovesDir(t *testing.T) {
	cap := stubWorkspaceExec(t, nil)
	dir := t.TempDir()
	require.Empty(t, cleanupWorkspace(io.Discard, wsCell(&bench.Workspace{Cmd: []string{"ws-cmd"}}), dir, false))
	require.NoDirExists(t, dir)
	nilWS := t.TempDir()
	require.Empty(t, cleanupWorkspace(io.Discard, bench.Cell{}, nilWS, false))
	require.NoDirExists(t, nilWS)
	require.Empty(t, cap.names)
}

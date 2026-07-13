package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/realkarych/catacomb/bench"
)

var (
	mkdirTempFn     = os.MkdirTemp
	removeAllFn     = os.RemoveAll
	teardownTimeout = time.Minute
)

type workspaceOpts struct {
	baseDir string
	keep    bool
}

func setupWorkspace(ctx context.Context, stdout, stderr io.Writer, cell bench.Cell, o workspaceOpts) (string, int, bool) {
	ws := cell.EffectiveWorkspace()
	dir, err := mkdirTempFn(o.baseDir, cell.RunID+"-*")
	if err != nil {
		fmt.Fprintf(stderr, "bench %s: workspace: %s\n", cell.RunID, err)
		return "", -1, false
	}
	c := execCommandContext(ctx, ws.Cmd[0], ws.Cmd[1:]...)
	c.Dir = dir
	c.Stdout = stdout
	c.Stderr = stderr
	c.WaitDelay = 10 * time.Second
	c.Env = workspaceEnv(ws)
	if code, ok := exitInfo(c.Run()); !ok {
		return dir, code, false
	}
	return dir, 0, true
}

func workspaceEnv(ws *bench.Workspace) []string {
	env := os.Environ()
	if ws.PatchAbs != "" {
		env = append(env, "CATACOMB_PATCH="+ws.PatchAbs)
	}
	return env
}

func cleanupWorkspace(stderr io.Writer, cell bench.Cell, dir string, keep bool) []string {
	if dir == "" {
		return nil
	}
	var notes []string
	if note := runTeardown(stderr, cell, dir); note != "" {
		notes = append(notes, note)
	}
	if keep {
		fmt.Fprintf(stderr, "bench %s: workspace kept: %s\n", cell.RunID, dir)
		return notes
	}
	if err := removeAllFn(dir); err != nil {
		note := "workspace remove: " + err.Error()
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
		notes = append(notes, note)
	}
	return notes
}

func runTeardown(stderr io.Writer, cell bench.Cell, dir string) string {
	ws := cell.EffectiveWorkspace()
	if ws == nil || len(ws.Teardown) == 0 {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), teardownTimeout)
	defer cancel()
	c := execCommandContext(ctx, ws.Teardown[0], ws.Teardown[1:]...)
	c.Dir = dir
	c.Stderr = stderr
	c.WaitDelay = 10 * time.Second
	if err := c.Run(); err != nil {
		note := "workspace teardown: " + err.Error()
		fmt.Fprintf(stderr, "bench %s: %s\n", cell.RunID, note)
		return note
	}
	return ""
}

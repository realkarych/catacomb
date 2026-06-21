package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func TestStreamForwardDelivers(t *testing.T) {
	d, discovery := runTestDaemon(t)
	var warn bytes.Buffer
	body := bytes.NewReader([]byte(`{"type":"system","subtype":"init","session_id":"s1"}` + "\n"))
	streamForward(&warn, discovery, body)
	assert.Empty(t, warn.String())
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestStreamForwardMissingDiscovery(t *testing.T) {
	var warn bytes.Buffer
	streamForward(&warn, filepath.Join(t.TempDir(), "nope.json"), bytes.NewReader([]byte(`{}`)))
	assert.Contains(t, warn.String(), "discovery")
}

func TestStreamForwardDaemonDown(t *testing.T) {
	ln, err := daemon.ListenLoopback()
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: addr, Token: "t"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`)))
	assert.Contains(t, warn.String(), "forward")
}

func TestRunTeesAndForwards(t *testing.T) {
	d, discovery := runTestDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"run", "--", "claude", "-p"})
	require.NoError(t, root.Execute())
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestRunExitCodePropagates(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", "FAIL", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })
	err := runChild(io.Discard, io.Discard, discovery, "", []string{"claude"})
	var ee *exec.ExitError
	require.ErrorAs(t, err, &ee)
	assert.Equal(t, 7, ee.ExitCode())
}

func TestRunSetsRunID(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", "ENV", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })
	var out bytes.Buffer
	_ = runChild(&out, io.Discard, discovery, "run-42", []string{"claude"})
	assert.Contains(t, out.String(), "CATACOMB_RUN_ID=run-42")
}

func TestHelperProcess(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	switch {
	case len(args) > 0 && args[0] == "FAIL":
		os.Exit(7)
	case len(args) > 0 && args[0] == "ENV":
		for _, e := range os.Environ() {
			if len(e) >= 16 && e[:15] == "CATACOMB_RUN_ID" {
				fmt.Fprintln(os.Stdout, e)
			}
		}
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","session_id":"s1"}`)
		os.Exit(0)
	}
}

func TestStreamForwardBadAddr(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: "\x7f", Token: "t"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`)))
	assert.NotEmpty(t, warn.String())
}

func TestStreamForwardNon2xx(t *testing.T) {
	_, discovery := runTestDaemon(t)
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: d.Addr, Token: "wrong"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`+"\n")))
	assert.Contains(t, warn.String(), "status 401")
}

func TestIngestStreamJSONCommandWiring(t *testing.T) {
	_, discovery := runTestDaemon(t)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	root := newRootCmd()
	root.SetArgs([]string{"ingest", "stream-json"})
	root.SetIn(bytes.NewReader([]byte(`{"type":"system","subtype":"init","session_id":"s9"}` + "\n")))
	var errOut bytes.Buffer
	root.SetErr(&errOut)
	require.NoError(t, root.Execute())
}

func TestRunChildStartError(t *testing.T) {
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "does-not-exist-binary"))
	}
	t.Cleanup(func() { execCommand = orig })
	err := runChild(io.Discard, io.Discard, filepath.Join(t.TempDir(), "d.json"), "", []string{"nope"})
	require.Error(t, err)
}

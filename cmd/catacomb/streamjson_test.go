package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	streamForward(&warn, discovery, body, "")
	assert.Empty(t, warn.String())
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 30*time.Second, 10*time.Millisecond)
}

func TestStreamForwardMissingDiscovery(t *testing.T) {
	var warn bytes.Buffer
	streamForward(&warn, filepath.Join(t.TempDir(), "nope.json"), bytes.NewReader([]byte(`{}`)), "")
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
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`)), "")
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
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, 30*time.Second, 10*time.Millisecond)
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
	err := runChild(io.Discard, io.Discard, discovery, "", []string{"claude"}, "")
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
	_ = runChild(&out, io.Discard, discovery, "run-42", []string{"claude"}, "")
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
			if strings.HasPrefix(e, "CATACOMB_RUN_ID=") || strings.HasPrefix(e, "CATACOMB_LABELS=") {
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
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`)), "")
	assert.NotEmpty(t, warn.String())
}

func TestStreamForwardNon2xx(t *testing.T) {
	_, discovery := runTestDaemon(t)
	d, err := daemon.ReadDiscovery(discovery)
	require.NoError(t, err)
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: d.Addr, Token: "wrong"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{"type":"system","subtype":"init","session_id":"s1"}`+"\n")), "")
	assert.Contains(t, warn.String(), "status 401")
}

func TestStreamForwardSetsLabelsHeader(t *testing.T) {
	gotHeader := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader <- r.Header.Get("X-Catacomb-Labels")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: srv.Listener.Addr().String(), Token: "tok"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`+"\n")), "team=alpha,env=prod")
	assert.Empty(t, warn.String())
	assert.Equal(t, "team=alpha,env=prod", <-gotHeader)
}

func TestStreamForwardOmitsLabelsHeaderWhenEmpty(t *testing.T) {
	present := make(chan bool, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, ok := r.Header["X-Catacomb-Labels"]
		present <- ok
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: srv.Listener.Addr().String(), Token: "tok"}))
	var warn bytes.Buffer
	streamForward(&warn, discovery, bytes.NewReader([]byte(`{}`+"\n")), "")
	assert.Empty(t, warn.String())
	assert.False(t, <-present)
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
	err := runChild(io.Discard, io.Discard, filepath.Join(t.TempDir(), "d.json"), "", []string{"nope"}, "")
	require.Error(t, err)
}

func TestLossyWriterNonBlocking(t *testing.T) {
	w := &lossyWriter{ch: make(chan []byte, 1)}
	n, err := w.Write([]byte("first"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, int64(0), w.dropped.Load())

	n, err = w.Write([]byte("second"))
	require.NoError(t, err)
	assert.Equal(t, 6, n)
	assert.Equal(t, int64(1), w.dropped.Load())
}

func TestLossyWriterIsolatesSlice(t *testing.T) {
	w := &lossyWriter{ch: make(chan []byte, 1)}
	buf := []byte("hello")
	_, _ = w.Write(buf)
	buf[0] = 'X'
	got := <-w.ch
	assert.Equal(t, byte('h'), got[0])
}

func TestRunChildPumpForwardsToDiscovery(t *testing.T) {
	received := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err == nil {
			received <- body
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	discovery := filepath.Join(t.TempDir(), "d.json")
	host := srv.Listener.Addr().String()
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: host, Token: "tok"}))

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })

	var out bytes.Buffer
	require.NoError(t, runChild(&out, io.Discard, discovery, "", []string{"claude"}, ""))

	select {
	case body := <-received:
		assert.Contains(t, string(body), `"session_id":"s1"`)
	case <-time.After(3 * time.Second):
		t.Fatal("daemon never received forwarded data")
	}
}

func TestRunChildForwardsLabelsHeader(t *testing.T) {
	gotHeader := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		gotHeader <- r.Header.Get("X-Catacomb-Labels")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: srv.Listener.Addr().String(), Token: "tok"}))

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })

	var out bytes.Buffer
	require.NoError(t, runChild(&out, io.Discard, discovery, "", []string{"claude"}, "team=alpha"))

	select {
	case h := <-gotHeader:
		assert.Equal(t, "team=alpha", h)
	case <-time.After(3 * time.Second):
		t.Fatal("daemon never received forwarded data")
	}
}

func TestRunLabelFlagMergesWithEnv(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	t.Setenv("CATACOMB_LABELS", "basket=b1,variant=v1")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", "ENV", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })

	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"run", "--label", "variant=v2", "--label", "rep=3", "--", "claude"})
	require.NoError(t, root.Execute())

	got := make([]string, 0, 1)
	for _, l := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if strings.HasPrefix(l, "CATACOMB_LABELS=") {
			got = append(got, l)
		}
	}
	require.Len(t, got, 1)
	assert.Equal(t, "CATACOMB_LABELS=basket=b1,rep=3,variant=v2", got[0])
}

func TestRunNoLabelsLeavesChildEnvUnset(t *testing.T) {
	discovery := filepath.Join(t.TempDir(), "d.json")
	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", "ENV", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })

	var out bytes.Buffer
	require.NoError(t, runChild(&out, io.Discard, discovery, "", []string{"claude"}, ""))
	assert.NotContains(t, out.String(), "CATACOMB_LABELS=")
}

func TestRunChildShutdownNoHang(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	host := srv.Listener.Addr().String()
	srv.Close()

	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: host, Token: "tok"}))

	t.Setenv("GO_WANT_HELPER_PROCESS", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })

	done := make(chan error, 1)
	go func() {
		done <- runChild(io.Discard, io.Discard, discovery, "", []string{"claude"}, "")
	}()

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond)
}

package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func TestRunUIOpensAndPrints(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "127.0.0.1:12345",
		Token: "tok",
	}))

	var openedURL string
	orig := openBrowser
	openBrowser = func(u string) error {
		openedURL = u
		return nil
	}
	t.Cleanup(func() { openBrowser = orig })

	var out strings.Builder
	require.NoError(t, runUI(disc, false, &out))

	wantURL := "http://127.0.0.1:12345/?token=tok"
	assert.Equal(t, wantURL+"\n", out.String())
	assert.Equal(t, wantURL, openedURL)
}

func TestRunUINoOpen(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "127.0.0.1:12345",
		Token: "tok",
	}))

	var openerCalled bool
	orig := openBrowser
	openBrowser = func(_ string) error {
		openerCalled = true
		return nil
	}
	t.Cleanup(func() { openBrowser = orig })

	var out strings.Builder
	require.NoError(t, runUI(disc, true, &out))

	assert.False(t, openerCalled, "opener must not be called with --no-open")
	assert.Contains(t, out.String(), "http://127.0.0.1:12345/?token=tok")
}

func TestRunUIDiscoveryError(t *testing.T) {
	err := runUI("/no/such/path.json", false, io.Discard)
	require.Error(t, err)
}

func TestRunUIDiscoveryNotFoundReturnsErrNoDaemon(t *testing.T) {
	err := runUI(t.TempDir()+"/missing.json", false, io.Discard)
	assert.True(t, errors.Is(err, ErrNoDaemon))
}

func TestRunUIDiscoveryParseError(t *testing.T) {
	disc := t.TempDir() + "/bad.json"
	require.NoError(t, os.WriteFile(disc, []byte("{not json}"), 0o600))
	err := runUI(disc, false, io.Discard)
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrNoDaemon))
}

func TestRunUIOpenError(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "127.0.0.1:12345",
		Token: "tok",
	}))

	orig := openBrowser
	openBrowser = func(_ string) error {
		return errors.New("open failed")
	}
	t.Cleanup(func() { openBrowser = orig })

	err := runUI(disc, false, io.Discard)
	require.Error(t, err)
}

func TestOpenBrowserVarIsNotNil(t *testing.T) {
	assert.NotNil(t, openBrowser)
}

func TestUICmdRunE(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "127.0.0.1:12345",
		Token: "tok",
	}))

	orig := openBrowser
	openBrowser = func(_ string) error { return nil }
	t.Cleanup(func() { openBrowser = orig })

	t.Setenv("CATACOMB_DISCOVERY", disc)
	cmd := newUICmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}

func TestUICmdNoOpenFlag(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "127.0.0.1:12345",
		Token: "tok",
	}))

	var openerCalled bool
	orig := openBrowser
	openBrowser = func(_ string) error {
		openerCalled = true
		return nil
	}
	t.Cleanup(func() { openBrowser = orig })

	t.Setenv("CATACOMB_DISCOVERY", disc)
	cmd := newUICmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.ParseFlags([]string{"--no-open"}))
	require.NoError(t, cmd.Execute())
	assert.False(t, openerCalled)
}

func TestBrowserCommandArgs(t *testing.T) {
	tests := []struct {
		goos     string
		wantArgs []string
	}{
		{
			goos:     "darwin",
			wantArgs: []string{"open", "http://example.com"},
		},
		{
			goos:     "windows",
			wantArgs: []string{"rundll32", "url.dll,FileProtocolHandler", "http://example.com"},
		},
		{
			goos:     "linux",
			wantArgs: []string{"xdg-open", "http://example.com"},
		},
		{
			goos:     "freebsd",
			wantArgs: []string{"xdg-open", "http://example.com"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.goos, func(t *testing.T) {
			cmd := browserCommand(tc.goos, "http://example.com")
			require.NotNil(t, cmd)
			assert.Equal(t, tc.wantArgs, cmd.Args)
		})
	}
}

type failWriter struct{}

func (failWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("write failed")
}

func TestRunUIWriteError(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{
		Addr:  "127.0.0.1:12345",
		Token: "tok",
	}))

	err := runUI(disc, true, failWriter{})
	require.Error(t, err)
}

func TestOpenBrowserCallsStartCmd(t *testing.T) {
	var started bool
	orig := startCmd
	startCmd = func(_ *exec.Cmd) error {
		started = true
		return nil
	}
	t.Cleanup(func() { startCmd = orig })

	require.NoError(t, openBrowser("http://example.com"))
	assert.True(t, started)
}

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
	"github.com/realkarych/catacomb/tui"
)

type fakeObsClient struct{ tui.Client }

func TestRunObserveNoDaemon(t *testing.T) {
	deps := observeDeps{
		readDiscovery: func(string) (daemon.Discovery, error) {
			return daemon.Discovery{}, fmt.Errorf("read: %w", os.ErrNotExist)
		},
		discoveryPath: "/no/such.json",
		newClient:     func(daemon.Discovery) tui.Client { return fakeObsClient{} },
		runProgram:    func(tui.Model) error { return nil },
	}
	err := runObserve(context.Background(), io.Discard, deps)
	assert.True(t, errors.Is(err, ErrNoDaemon))
}

func TestRunObserveDiscoveryOtherError(t *testing.T) {
	boom := errors.New("boom")
	deps := observeDeps{
		readDiscovery: func(string) (daemon.Discovery, error) {
			return daemon.Discovery{}, boom
		},
		newClient:  func(daemon.Discovery) tui.Client { return fakeObsClient{} },
		runProgram: func(tui.Model) error { return nil },
	}
	err := runObserve(context.Background(), io.Discard, deps)
	assert.ErrorIs(t, err, boom)
}

func TestRunObserveRunsProgram(t *testing.T) {
	ran := false
	deps := observeDeps{
		readDiscovery: func(string) (daemon.Discovery, error) {
			return daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}, nil
		},
		newClient:  func(daemon.Discovery) tui.Client { return fakeObsClient{} },
		newModel:   tui.NewModel,
		runProgram: func(tui.Model) error { ran = true; return nil },
		noColor:    true,
	}
	require.NoError(t, runObserve(context.Background(), io.Discard, deps))
	assert.True(t, ran)
}

func TestRunObserveProgramError(t *testing.T) {
	deps := observeDeps{
		readDiscovery: func(string) (daemon.Discovery, error) {
			return daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}, nil
		},
		newClient:  func(daemon.Discovery) tui.Client { return fakeObsClient{} },
		newModel:   tui.NewModel,
		runProgram: func(tui.Model) error { return errors.New("boom") },
	}
	err := runObserve(context.Background(), io.Discard, deps)
	require.Error(t, err)
}

func TestObserveCmdFlagsAndArg(t *testing.T) {
	cmd := newObserveCmd()
	require.NotNil(t, cmd.Flags().Lookup("no-color"))
	assert.Equal(t, "observe", cmd.Name())
}

func TestObserveCmdRunE(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}))
	t.Setenv("CATACOMB_DISCOVERY", disc)
	prev := teaRun
	t.Cleanup(func() { teaRun = prev })
	teaRun = func(tui.Model) error { return nil }
	cmd := newObserveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"s1"})
	require.NoError(t, cmd.Execute())
}

func TestObserveCmdRunENoArg(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}))
	t.Setenv("CATACOMB_DISCOVERY", disc)
	prev := teaRun
	t.Cleanup(func() { teaRun = prev })
	teaRun = func(tui.Model) error { return nil }
	cmd := newObserveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}

func TestObserveCmdRunEError(t *testing.T) {
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}))
	t.Setenv("CATACOMB_DISCOVERY", disc)
	prev := teaRun
	t.Cleanup(func() { teaRun = prev })
	teaRun = func(tui.Model) error { return errors.New("boom") }
	cmd := newObserveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	require.Error(t, err)
}

func TestObserveCmdNoDaemon(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", t.TempDir()+"/missing.json")
	cmd := newObserveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNoDaemon))
}

func TestShouldDisableColor(t *testing.T) {
	assert.True(t, shouldDisableColor(true, "", true))
	assert.True(t, shouldDisableColor(false, "1", true))
	assert.True(t, shouldDisableColor(false, "", false))
	assert.False(t, shouldDisableColor(false, "", true))
}

func TestStdoutIsTTY(t *testing.T) {
	result := stdoutIsTTY()
	assert.IsType(t, true, result)
}

func TestObserveNonTTYForcesNoColor(t *testing.T) {
	prevTTY := stdoutIsTTY
	t.Cleanup(func() { stdoutIsTTY = prevTTY })
	stdoutIsTTY = func() bool { return false }
	disc := t.TempDir() + "/d.json"
	require.NoError(t, daemon.WriteDiscovery(disc, daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}))
	t.Setenv("CATACOMB_DISCOVERY", disc)
	prev := teaRun
	t.Cleanup(func() { teaRun = prev })
	teaRun = func(tui.Model) error { return nil }
	cmd := newObserveCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	require.NoError(t, cmd.Execute())
}

func TestTeaRunVarNotNil(t *testing.T) {
	assert.NotNil(t, teaRun)
}

func TestRunObserveHashDerivedCtxCancelsWithParent(t *testing.T) {
	parent, parentCancel := context.WithCancel(context.Background())
	parentCancel()
	var capturedCtx context.Context
	deps := observeDeps{
		readDiscovery: func(string) (daemon.Discovery, error) {
			return daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"}, nil
		},
		newClient: func(daemon.Discovery) tui.Client { return fakeObsClient{} },
		newModel: func(ctx context.Context, c tui.Client, h string, nc bool) tui.Model {
			capturedCtx = ctx
			return tui.NewModel(ctx, c, h, nc)
		},
		runProgram: func(tui.Model) error { return nil },
		noColor:    true,
	}
	require.NoError(t, runObserveHash(parent, io.Discard, deps, ""))
	require.NotNil(t, capturedCtx)
	select {
	case <-capturedCtx.Done():
	default:
		t.Fatal("derived ctx not cancelled when parent was already cancelled")
	}
}

package daemon

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSocketPathEnvOverride(t *testing.T) {
	t.Setenv("CATACOMB_SOCKET", "/tmp/x/custom.sock")
	assert.Equal(t, "/tmp/x/custom.sock", SocketPath())
}

func TestSocketPathXDG(t *testing.T) {
	t.Setenv("CATACOMB_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/u")
	assert.Equal(t, filepath.Join("/run/u", "catacomb", "daemon.sock"), SocketPath())
}

func TestSocketPathHome(t *testing.T) {
	t.Setenv("CATACOMB_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "/home/u")
	assert.Equal(t, filepath.Join("/home/u", ".catacomb", "run", "daemon.sock"), SocketPath())
}

func TestSocketPathHomeError(t *testing.T) {
	t.Setenv("CATACOMB_SOCKET", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	t.Setenv("HOME", "")
	assert.Equal(t, filepath.Join(os.TempDir(), "catacomb", "daemon.sock"), SocketPath())
}

func TestListenHappy(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	ln, err := Listen(sock)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	fi, statErr := os.Stat(sock)
	require.NoError(t, statErr)
	assert.Equal(t, os.FileMode(0o600), fi.Mode().Perm())
}

func TestListenReplacesStaleSocket(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	require.NoError(t, os.WriteFile(sock, []byte("stale"), 0o600))
	ln, err := Listen(sock)
	require.NoError(t, err)
	_ = ln.Close()
}

func TestListenMkdirError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	_, err := Listen(filepath.Join(file, "sub", "d.sock"))
	require.Error(t, err)
}

func TestListenStaleRemoveError(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "d.sock")
	require.NoError(t, os.MkdirAll(filepath.Join(sock, "child"), 0o700))
	_, err := Listen(sock)
	require.Error(t, err)
}

func TestListenListenError(t *testing.T) {
	old := netListen
	t.Cleanup(func() { netListen = old })
	netListen = func(string, string) (net.Listener, error) { return nil, errors.New("boom") }
	_, err := Listen(filepath.Join(t.TempDir(), "d.sock"))
	require.Error(t, err)
}

func TestListenChmodError(t *testing.T) {
	old := osChmod
	t.Cleanup(func() { osChmod = old })
	osChmod = func(string, os.FileMode) error { return errors.New("boom") }
	_, err := Listen(filepath.Join(t.TempDir(), "d.sock"))
	require.Error(t, err)
}

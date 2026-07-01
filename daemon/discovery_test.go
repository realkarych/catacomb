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

func TestDiscoveryPathEnvOverride(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", "/tmp/x/d.json")
	assert.Equal(t, "/tmp/x/d.json", DiscoveryPath())
}

func TestDiscoveryPathXDG(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/u")
	assert.Equal(t, filepath.Join("/run/u", "catacomb", "daemon.json"), DiscoveryPath())
}

func TestDiscoveryPathHome(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	old := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = old })
	osUserHomeDir = func() (string, error) { return "/home/u", nil }
	assert.Equal(t, filepath.Join("/home/u", ".catacomb", "run", "daemon.json"), DiscoveryPath())
}

func TestDiscoveryPathHomeError(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", "")
	t.Setenv("XDG_RUNTIME_DIR", "")
	old := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = old })
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	assert.Equal(t, filepath.Join(os.TempDir(), "catacomb", "daemon.json"), DiscoveryPath())
}

func TestNewTokenUnique(t *testing.T) {
	a, err := NewToken()
	require.NoError(t, err)
	b, err := NewToken()
	require.NoError(t, err)
	assert.NotEmpty(t, a)
	assert.Len(t, a, 64)
	assert.NotEqual(t, a, b)
}

func TestNewTokenError(t *testing.T) {
	old := randRead
	t.Cleanup(func() { randRead = old })
	randRead = func([]byte) (int, error) { return 0, errors.New("boom") }
	_, err := NewToken()
	require.Error(t, err)
}

func TestListenLoopback(t *testing.T) {
	ln, err := ListenLoopback()
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	assert.Contains(t, ln.Addr().String(), "127.0.0.1:")
}

func TestListenLoopbackError(t *testing.T) {
	old := netListen
	t.Cleanup(func() { netListen = old })
	netListen = func(string, string) (net.Listener, error) { return nil, errors.New("boom") }
	_, err := ListenLoopback()
	require.Error(t, err)
}

func TestWriteReadDiscoveryNewFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	in := Discovery{Addr: "127.0.0.1:5001", Token: "tok", Pid: 12345, StartedAt: "2026-06-24T10:00:00Z"}
	require.NoError(t, WriteDiscovery(path, in))
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, 12345, got.Pid)
	assert.Equal(t, "2026-06-24T10:00:00Z", got.StartedAt)
}

func TestWriteReadDiscoveryRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "daemon.json")
	require.NoError(t, WriteDiscovery(path, Discovery{Addr: "127.0.0.1:5000", Token: "tok"}))
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:5000", got.Addr)
	assert.Equal(t, "tok", got.Token)
}

func TestWriteDiscoveryMkdirError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	require.NoError(t, os.WriteFile(file, []byte("x"), 0o600))
	err := WriteDiscovery(filepath.Join(file, "sub", "daemon.json"), Discovery{})
	require.Error(t, err)
}

func TestWriteDiscoveryMarshalError(t *testing.T) {
	old := jsonMarshal
	t.Cleanup(func() { jsonMarshal = old })
	jsonMarshal = func(any) ([]byte, error) { return nil, errors.New("boom") }
	err := WriteDiscovery(filepath.Join(t.TempDir(), "daemon.json"), Discovery{})
	require.Error(t, err)
}

func TestWriteDiscoveryWriteError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "asdir")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	err := WriteDiscovery(dir, Discovery{})
	require.Error(t, err)
}

func TestReadDiscoveryReadError(t *testing.T) {
	_, err := ReadDiscovery(filepath.Join(t.TempDir(), "nope.json"))
	require.Error(t, err)
}

func TestReadDiscoveryParseError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, os.WriteFile(path, []byte("{not json}"), 0o600))
	_, err := ReadDiscovery(path)
	require.Error(t, err)
}

func TestDiscoveryNewFieldsRoundTrip(t *testing.T) {
	d := Discovery{
		Addr:           "127.0.0.1:1",
		Token:          "tok",
		StoreBackend:   "memory",
		SinkTypes:      []string{"otlp", "postgres"},
		SourcesEnabled: []string{"hooks", "otel"},
		ReaperWindow:   "30m0s",
		MaxShards:      4096,
	}
	tmp := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, WriteDiscovery(tmp, d))
	got, err := ReadDiscovery(tmp)
	require.NoError(t, err)
	assert.Equal(t, "memory", got.StoreBackend)
	assert.Equal(t, []string{"otlp", "postgres"}, got.SinkTypes)
	assert.Equal(t, []string{"hooks", "otel"}, got.SourcesEnabled)
	assert.Equal(t, "30m0s", got.ReaperWindow)
	assert.Equal(t, 4096, got.MaxShards)
}

func TestDiscoveryNewFieldsOmitEmpty(t *testing.T) {
	d := Discovery{Addr: "127.0.0.1:1", Token: "tok"}
	tmp := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, WriteDiscovery(tmp, d))
	b, err := os.ReadFile(tmp)
	require.NoError(t, err)
	assert.NotContains(t, string(b), "store_backend")
	assert.NotContains(t, string(b), "sink_types")
	assert.NotContains(t, string(b), "sources_enabled")
	assert.NotContains(t, string(b), "reaper_window")
}

func TestDiscoveryConfigPathRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "d.json")
	in := Discovery{
		Addr:       "127.0.0.1:1",
		Token:      "tok",
		ConfigPath: "/etc/catacomb/custom.yaml",
	}
	require.NoError(t, WriteDiscovery(path, in))
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, "/etc/catacomb/custom.yaml", got.ConfigPath)
}

func TestWriteDiscoveryAtomicNoTempLeftover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, WriteDiscovery(path, Discovery{Addr: "127.0.0.1:1", Token: "tok"}))
	_, statErr := os.Stat(path + ".new")
	assert.True(t, os.IsNotExist(statErr), "temp file must be renamed away, not left behind")
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1:1", got.Addr)
}

func TestWriteDiscoveryTempWriteError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.json")
	require.NoError(t, os.MkdirAll(path+".new", 0o700))
	err := WriteDiscovery(path, Discovery{Addr: "127.0.0.1:1", Token: "tok"})
	require.Error(t, err)
}

func TestWriteDiscoveryStartTokenRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	require.NoError(t, WriteDiscovery(path, Discovery{Addr: "127.0.0.1:1", Token: "tok", StartToken: 424242, BootID: "boot-xyz"}))
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, int64(424242), got.StartToken)
	assert.Equal(t, "boot-xyz", got.BootID)
}

func TestWriteReadDiscoveryScopeFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.json")
	in := Discovery{
		Addr:               "127.0.0.1:5001",
		Token:              "tok",
		TranscriptDir:      "/home/u/.claude/projects",
		DBPath:             "/home/u/.catacomb/catacomb.db",
		AllowPayloadAccess: true,
	}
	require.NoError(t, WriteDiscovery(path, in))
	got, err := ReadDiscovery(path)
	require.NoError(t, err)
	assert.Equal(t, "/home/u/.claude/projects", got.TranscriptDir)
	assert.Equal(t, "/home/u/.catacomb/catacomb.db", got.DBPath)
	assert.True(t, got.AllowPayloadAccess)
}

package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
)

type Discovery struct {
	Addr               string   `json:"addr"`
	Token              string   `json:"token"`
	GRPCAddr           string   `json:"grpc_addr,omitempty"`
	Pid                int      `json:"pid,omitempty"`
	StartedAt          string   `json:"started_at,omitempty"`
	StartToken         int64    `json:"start_token,omitempty"`
	TranscriptDir      string   `json:"transcript_dir,omitempty"`
	DBPath             string   `json:"db_path,omitempty"`
	ConfigPath         string   `json:"config_path,omitempty"`
	AllowPayloadAccess bool     `json:"allow_payload_access,omitempty"`
	AllowAnnotations   bool     `json:"allow_annotations,omitempty"`
	StoreBackend       string   `json:"store_backend,omitempty"`
	SinkTypes          []string `json:"sink_types,omitempty"`
	SourcesEnabled     []string `json:"sources_enabled,omitempty"`
	ReaperWindow       string   `json:"reaper_window,omitempty"`
	MaxShards          int      `json:"max_shards"`
}

var (
	osUserHomeDir = os.UserHomeDir
	netListen     = net.Listen
	randRead      = rand.Read
	jsonMarshal   = json.Marshal
)

func DiscoveryPath() string {
	if p := os.Getenv("CATACOMB_DISCOVERY"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "catacomb", "daemon.json")
	}
	home, err := osUserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "catacomb", "daemon.json")
	}
	return filepath.Join(home, ".catacomb", "run", "daemon.json")
}

func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := randRead(b); err != nil {
		return "", fmt.Errorf("daemon.NewToken: %w", err)
	}
	return hex.EncodeToString(b), nil
}

func ListenLoopback() (net.Listener, error) {
	ln, err := netListen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("daemon.ListenLoopback: %w", err)
	}
	return ln, nil
}

func WriteDiscovery(path string, d Discovery) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("daemon.WriteDiscovery mkdir: %w", err)
	}
	b, err := jsonMarshal(d)
	if err != nil {
		return fmt.Errorf("daemon.WriteDiscovery marshal: %w", err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("daemon.WriteDiscovery write: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("daemon.WriteDiscovery rename: %w", err)
	}
	return nil
}

func ReadDiscovery(path string) (Discovery, error) {
	var d Discovery
	b, err := os.ReadFile(path)
	if err != nil {
		return d, fmt.Errorf("daemon.ReadDiscovery read: %w", err)
	}
	if err := json.Unmarshal(b, &d); err != nil {
		return d, fmt.Errorf("daemon.ReadDiscovery parse: %w", err)
	}
	return d, nil
}

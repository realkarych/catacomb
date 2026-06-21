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
	Addr     string `json:"addr"`
	Token    string `json:"token"`
	GRPCAddr string `json:"grpc_addr,omitempty"`
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
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("daemon.WriteDiscovery write: %w", err)
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

package daemon

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
)

var (
	netListen = net.Listen
	osChmod   = os.Chmod
)

func SocketPath() string {
	if p := os.Getenv("CATACOMB_SOCKET"); p != "" {
		return p
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "catacomb", "daemon.sock")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "catacomb", "daemon.sock")
	}
	return filepath.Join(home, ".catacomb", "run", "daemon.sock")
}

func Listen(path string) (net.Listener, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("daemon.Listen mkdir: %w", err)
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("daemon.Listen stale: %w", err)
	}
	ln, err := netListen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("daemon.Listen: %w", err)
	}
	if err := osChmod(path, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("daemon.Listen chmod: %w", err)
	}
	return ln, nil
}

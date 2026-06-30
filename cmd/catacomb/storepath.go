package main

import "path/filepath"

func defaultDBPath() string {
	home, err := osUserHomeDir()
	if err != nil {
		return "catacomb.db"
	}
	return filepath.Join(home, ".catacomb", "catacomb.db")
}

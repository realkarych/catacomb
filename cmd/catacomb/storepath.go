package main

import (
	"os"
	"path/filepath"
)

var osUserHomeDir = os.UserHomeDir

func defaultDBPath() string {
	home, err := osUserHomeDir()
	if err != nil {
		return "catacomb.db"
	}
	return filepath.Join(home, ".catacomb", "catacomb.db")
}

package main

import (
	"os"

	"github.com/realkarych/catacomb/daemon"
)

func clientDiscoveryPath() string {
	return clientDiscoveryPathWith(os.LookupEnv, os.ReadFile, osUserHomeDir)
}

func clientDiscoveryPathWith(lookupEnv func(string) (string, bool), readFile func(string) ([]byte, error), home func() (string, error)) string {
	if p, ok := lookupEnv("CATACOMB_DISCOVERY"); ok && p != "" {
		return p
	}
	h, err := home()
	if err != nil {
		return daemon.DiscoveryPath()
	}
	cfg, cerr := resolveConfig(daemonFlags{}, readFile, lookupEnv, h)
	if cerr != nil {
		return daemon.DiscoveryPath()
	}
	return resolveDiscovery(cfg.Daemon.Discovery)
}

package main

import (
	"errors"
	"os"

	"github.com/realkarych/catacomb/config"
)

func resolveStorePath(readFile func(string) ([]byte, error), lookupEnv func(string) (string, bool), home string) string {
	getenv := func(k string) string { v, _ := lookupEnv(k); return v }
	cfg := config.Defaults()
	data, err := readFile(configFilePath(daemonFlags{}, lookupEnv, home))
	switch {
	case err == nil:
		fileCfg, perr := config.Parse(data)
		if perr != nil {
			return config.ExpandPath(config.DefaultSQLitePath, home, getenv)
		}
		cfg = config.Merge(cfg, fileCfg)
	case errors.Is(err, os.ErrNotExist):
	default:
		return config.ExpandPath(config.DefaultSQLitePath, home, getenv)
	}
	cfg = config.Merge(cfg, config.FromEnv(lookupEnv))
	return config.ExpandPath(cfg.Store.SQLite.Path, home, getenv)
}

func defaultBatchDBPath() string {
	home, err := osUserHomeDir()
	if err != nil {
		return defaultDBPath()
	}
	return resolveStorePath(os.ReadFile, os.LookupEnv, home)
}

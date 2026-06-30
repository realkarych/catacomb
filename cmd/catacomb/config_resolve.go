package main

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/realkarych/catacomb/config"
)

type daemonFlags struct {
	configPath            string
	configPathSet         bool
	dbPath                string
	dbPathSet             bool
	discoveryPath         string
	discoveryPathSet      bool
	reaperWindow          time.Duration
	reaperWindowSet       bool
	maxShards             int
	maxShardsSet          bool
	allowPayloadAccess    bool
	allowPayloadAccessSet bool
	allowAnnotations      bool
	allowAnnotationsSet   bool
}

func configFilePath(f daemonFlags, lookupEnv func(string) (string, bool), home string) string {
	getenv := func(k string) string { v, _ := lookupEnv(k); return v }
	if f.configPathSet {
		return config.ExpandPath(f.configPath, home, getenv)
	}
	if v, ok := lookupEnv("CATACOMB_CONFIG"); ok {
		return config.ExpandPath(v, home, getenv)
	}
	return config.ExpandPath(config.DefaultConfigPath, home, getenv)
}

func resolveConfig(f daemonFlags, readFile func(string) ([]byte, error), lookupEnv func(string) (string, bool), home string) (config.Config, error) {
	getenv := func(k string) string { v, _ := lookupEnv(k); return v }
	cfg := config.Defaults()
	data, err := readFile(configFilePath(f, lookupEnv, home))
	switch {
	case err == nil:
		fileCfg, perr := config.Parse(data)
		if perr != nil {
			return config.Config{}, fmt.Errorf("daemon.resolveConfig: %w", perr)
		}
		cfg = config.Merge(cfg, fileCfg)
	case errors.Is(err, os.ErrNotExist):
	default:
		return config.Config{}, fmt.Errorf("daemon.resolveConfig read: %w", err)
	}
	cfg = config.Merge(cfg, config.FromEnv(lookupEnv))
	cfg = applyDaemonFlags(cfg, f)
	cfg = config.ExpandPaths(cfg, home, getenv)
	if err := config.Validate(cfg); err != nil {
		return config.Config{}, fmt.Errorf("daemon.resolveConfig: %w", err)
	}
	return cfg, nil
}

func applyDaemonFlags(cfg config.Config, f daemonFlags) config.Config {
	if f.dbPathSet {
		cfg.Store.SQLite.Path = f.dbPath
	}
	if f.discoveryPathSet {
		cfg.Daemon.Discovery = f.discoveryPath
	}
	if f.reaperWindowSet {
		cfg.Daemon.ReaperWindow = config.Duration(f.reaperWindow)
	}
	if f.maxShardsSet {
		cfg.Daemon.MaxShards = f.maxShards
	}
	if f.allowPayloadAccessSet {
		cfg.Daemon.AllowPayloadAccess = f.allowPayloadAccess
	}
	if f.allowAnnotationsSet {
		cfg.Daemon.AllowAnnotations = f.allowAnnotations
	}
	return cfg
}

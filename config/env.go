package config

import (
	"os"
	"path/filepath"
	"strings"
)

func FromEnv(lookup func(string) (string, bool)) Config {
	var c Config
	if v, ok := lookup("CATACOMB_DB"); ok {
		c.Store.SQLite.Path = v
	}
	if v, ok := lookup("CATACOMB_DISCOVERY"); ok {
		c.Daemon.Discovery = v
	}
	return c
}

func ExpandPath(path, home string, getenv func(string) string) string {
	if path == "" {
		return ""
	}
	switch {
	case path == "~":
		path = home
	case strings.HasPrefix(path, "~/"):
		path = filepath.Join(home, path[2:])
	}
	return os.Expand(path, getenv)
}

func ExpandPaths(c Config, home string, getenv func(string) string) Config {
	c.Store.SQLite.Path = ExpandPath(c.Store.SQLite.Path, home, getenv)
	c.Daemon.Discovery = ExpandPath(c.Daemon.Discovery, home, getenv)
	c.Sources.JSONL.TranscriptDir = ExpandPath(c.Sources.JSONL.TranscriptDir, home, getenv)
	c.Sinks = append([]Sink(nil), c.Sinks...)
	for i := range c.Sinks {
		c.Sinks[i].Path = ExpandPath(c.Sinks[i].Path, home, getenv)
	}
	return c
}

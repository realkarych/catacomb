package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/realkarych/catacomb/daemon"
)

func TestClientDiscoveryPathWith_EnvSet(t *testing.T) {
	readFileCalled := false
	readFile := func(string) ([]byte, error) {
		readFileCalled = true
		return nil, errors.New("must not be called")
	}
	lookup := func(k string) (string, bool) {
		if k == "CATACOMB_DISCOVERY" {
			return "/env/daemon.json", true
		}
		return "", false
	}
	home := func() (string, error) { return "/home/u", nil }
	got := clientDiscoveryPathWith(lookup, readFile, home)
	assert.Equal(t, "/env/daemon.json", got)
	assert.False(t, readFileCalled, "readFile must not be called when CATACOMB_DISCOVERY is set")
}

func TestClientDiscoveryPathWith_ConfigFileDaemonDiscovery(t *testing.T) {
	customPath := filepath.FromSlash("/custom/daemon.json")
	yaml := "daemon:\n  discovery: " + customPath + "\n"
	readFile := func(string) ([]byte, error) {
		return []byte(yaml), nil
	}
	lookup := func(k string) (string, bool) { return "", false }
	home := func() (string, error) { return "/home/u", nil }
	got := clientDiscoveryPathWith(lookup, readFile, home)
	assert.Equal(t, customPath, got)
}

func TestClientDiscoveryPathWith_NoConfigDaemonDiscovery(t *testing.T) {
	readFile := func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	lookup := func(k string) (string, bool) { return "", false }
	home := func() (string, error) { return "/home/u", nil }
	got := clientDiscoveryPathWith(lookup, readFile, home)
	assert.Equal(t, daemon.DiscoveryPath(), got)
}

func TestClientDiscoveryPathWith_HomeError(t *testing.T) {
	readFileCalled := false
	readFile := func(string) ([]byte, error) {
		readFileCalled = true
		return nil, fs.ErrNotExist
	}
	lookup := func(k string) (string, bool) { return "", false }
	home := func() (string, error) { return "", errors.New("no home") }
	got := clientDiscoveryPathWith(lookup, readFile, home)
	assert.Equal(t, daemon.DiscoveryPath(), got)
	assert.False(t, readFileCalled, "readFile must not be called when home() fails")
}

func TestClientDiscoveryPathWith_ConfigParseError(t *testing.T) {
	readFile := func(string) ([]byte, error) {
		return []byte("store:\n  nope: 1\n"), nil
	}
	lookup := func(k string) (string, bool) { return "", false }
	home := func() (string, error) { return "/home/u", nil }
	got := clientDiscoveryPathWith(lookup, readFile, home)
	assert.Equal(t, daemon.DiscoveryPath(), got)
}

func TestClientDiscoveryPathWith_ConfigReadError(t *testing.T) {
	readFile := func(string) ([]byte, error) { return nil, errors.New("disk error") }
	lookup := func(k string) (string, bool) { return "", false }
	home := func() (string, error) { return "/home/u", nil }
	got := clientDiscoveryPathWith(lookup, readFile, home)
	assert.Equal(t, daemon.DiscoveryPath(), got)
}

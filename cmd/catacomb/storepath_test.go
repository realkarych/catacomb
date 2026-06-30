package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDefaultDBPathFromHome(t *testing.T) {
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return "/home/u", nil }
	assert.Equal(t, filepath.Join("/home/u", ".catacomb", "catacomb.db"), defaultDBPath())
}

func TestDefaultDBPathFallback(t *testing.T) {
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return "", errors.New("no home") }
	assert.Equal(t, "catacomb.db", defaultDBPath())
}

func TestBatchCommandsUseDefaultDBPath(t *testing.T) {
	for _, cmd := range []*struct {
		name string
		def  string
	}{
		{"runs", newRunsCmd().Flags().Lookup("db").DefValue},
		{"inspect", newInspectCmd().Flags().Lookup("db").DefValue},
		{"snapshot", newSnapshotCmd().Flags().Lookup("db").DefValue},
		{"export", newExportCmd().Flags().Lookup("db").DefValue},
		{"replay", newReplayCmd().Flags().Lookup("db").DefValue},
	} {
		assert.Equal(t, defaultDBPath(), cmd.def, "command %s --db default", cmd.name)
	}
}

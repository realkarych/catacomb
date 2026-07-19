package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stubHomeDir(t *testing.T, home string, err error) {
	t.Helper()
	orig := osUserHomeDir
	t.Cleanup(func() { osUserHomeDir = orig })
	osUserHomeDir = func() (string, error) { return home, err }
}

func TestDefaultDBPathFromHome(t *testing.T) {
	stubHomeDir(t, "/home/u", nil)
	assert.Equal(t, filepath.Join("/home/u", ".catacomb", "catacomb.db"), defaultDBPath())
}

func TestDefaultDBPathFallback(t *testing.T) {
	stubHomeDir(t, "", errors.New("no home"))
	assert.Equal(t, "catacomb.db", defaultDBPath())
}

func lookupDBDefault(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	flag := cmd.Flags().Lookup("db")
	require.NotNil(t, flag, "command %q must expose --db", cmd.Name())
	return flag.DefValue
}

func TestCommandsWithDBFlagDefaultToTheHomeStorePath(t *testing.T) {
	stubHomeDir(t, "/fixture-home", nil)
	want := filepath.Join("/fixture-home", ".catacomb", "catacomb.db")

	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"regress", newRegressCmd()},
		{"trends", newTrendsCmd()},
		{"pack", newPackCmd()},
		{"baseline set", newBaselineSetCmd()},
		{"baseline list", newBaselineListCmd()},
		{"baseline rm", newBaselineRmCmd()},
		{"baseline export", newBaselineExportCmd()},
		{"baseline import", newBaselineImportCmd()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, want, lookupDBDefault(t, tc.cmd))
		})
	}
}

func TestCommandsWithDBFlagFallBackToCwdWhenHomeIsUnresolvable(t *testing.T) {
	stubHomeDir(t, "", errors.New("no home"))
	for _, cmd := range []*cobra.Command{newRegressCmd(), newTrendsCmd(), newPackCmd(), newBaselineSetCmd()} {
		t.Run(cmd.Name(), func(t *testing.T) {
			assert.Equal(t, "catacomb.db", lookupDBDefault(t, cmd))
		})
	}
}

package main

import (
	"bytes"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVersionCommand(t *testing.T) {
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"version"})

	require.NoError(t, root.Execute())
	require.Equal(t, "catacomb dev\n", out.String())
}

func TestVersionFromBuild(t *testing.T) {
	bi := func(v string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: v}}, true
		}
	}
	none := func() (*debug.BuildInfo, bool) { return nil, false }

	require.Equal(t, "v1.2.3", versionFromBuild("v1.2.3", bi("v9.9.9")))
	require.Equal(t, "v0.1.1", versionFromBuild("dev", bi("v0.1.1")))
	require.Equal(t, "dev", versionFromBuild("dev", bi("(devel)")))
	require.Equal(t, "dev", versionFromBuild("dev", bi("")))
	require.Equal(t, "dev", versionFromBuild("dev", none))
}

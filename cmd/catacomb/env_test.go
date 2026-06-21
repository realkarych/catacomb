package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func TestEnvCmd(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{Addr: "127.0.0.1:5000", Token: "tok"}))

	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetArgs([]string{"env", "--discovery", path})
	root.SetOut(buf)
	require.NoError(t, root.Execute())

	out := buf.String()
	require.True(t, strings.Contains(out, "CLAUDE_CODE_ENABLE_TELEMETRY=1"), "missing CLAUDE_CODE_ENABLE_TELEMETRY=1")
	require.True(t, strings.Contains(out, "OTEL_TRACES_EXPORTER=otlp"), "missing OTEL_TRACES_EXPORTER=otlp")
	require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf"), "missing OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf")
	require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:5000"), "missing OTEL_EXPORTER_OTLP_ENDPOINT")
	require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_HEADERS=authorization=Bearer tok"), "missing OTEL_EXPORTER_OTLP_HEADERS")
}

func TestEnvCmdMissingDiscovery(t *testing.T) {
	root := newRootCmd()
	root.SetArgs([]string{"env", "--discovery", "/nonexistent/path/d.json"})
	require.Error(t, root.Execute())
}

func TestEnvCmdDefaultDiscovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "d.json")
	require.NoError(t, daemon.WriteDiscovery(path, daemon.Discovery{Addr: "127.0.0.1:6000", Token: "tok2"}))
	t.Setenv("CATACOMB_DISCOVERY", path)

	buf := &bytes.Buffer{}
	root := newRootCmd()
	root.SetArgs([]string{"env"})
	root.SetOut(buf)
	require.NoError(t, root.Execute())

	out := buf.String()
	require.True(t, strings.Contains(out, "OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:6000"), "missing endpoint")
}

package codex

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/drift"
)

var contractPlaceholderRE = regexp.MustCompile(`__[A-Z_]+__`)

type contractRecord struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func contractScan(t *testing.T, path string, substitute bool) (map[[2]string]bool, []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	pairs := map[[2]string]bool{}
	versions := []string{}
	for _, raw := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		line := raw
		if substitute {
			line = strings.ReplaceAll(line, "__EPOCH__", "0")
			line = contractPlaceholderRE.ReplaceAllString(line, "x")
		}
		var rec contractRecord
		require.NoErrorf(t, json.Unmarshal([]byte(line), &rec), "path=%s line=%s", path, line)
		var payload struct {
			Type       string `json:"type"`
			CliVersion string `json:"cli_version"`
		}
		if len(rec.Payload) > 0 {
			require.NoErrorf(t, json.Unmarshal(rec.Payload, &payload), "path=%s payload=%s", path, rec.Payload)
		}
		pairs[[2]string{rec.Type, payload.Type}] = true
		if rec.Type == "session_meta" && payload.CliVersion != "" {
			versions = append(versions, payload.CliVersion)
		}
	}
	return pairs, versions
}

func TestCodexFixtureContract(t *testing.T) {
	testdata, err := filepath.Glob("testdata/*.jsonl")
	require.NoError(t, err)
	require.NotEmpty(t, testdata)

	canon := map[[2]string]bool{}
	for _, f := range testdata {
		pairs, versions := contractScan(t, f, false)
		for k := range pairs {
			canon[k] = true
		}
		for _, v := range versions {
			assert.Equalf(t, drift.TestedCodexVersion, v, "testdata %s stamps cli_version %q, want pinned %q", f, v, drift.TestedCodexVersion)
		}
	}
	require.NotEmpty(t, canon)

	fixtures, err := filepath.Glob("../../e2e/hermetic/prod/fixtures/*codex*.jsonl.tmpl")
	require.NoError(t, err)
	require.NotEmpty(t, fixtures)

	seen := map[[2]string]bool{}
	for _, f := range fixtures {
		pairs, versions := contractScan(t, f, true)
		require.NotEmptyf(t, pairs, "fixture %s produced no records", f)
		for k := range pairs {
			seen[k] = true
			assert.Truef(t, canon[k], "fixture %s emits record/payload type %v absent from ingest/codex/testdata canon", f, k)
		}
		for _, v := range versions {
			assert.Equalf(t, drift.TestedCodexVersion, v, "fixture %s stamps cli_version %q, want pinned %q", f, v, drift.TestedCodexVersion)
		}
	}
	require.NotEmpty(t, seen)
}

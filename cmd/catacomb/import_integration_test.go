package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/regress"
)

func TestImportThenVerifyThenRegress(t *testing.T) {
	dir := t.TempDir()
	basket := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(basket, []byte(`basket: checkout
reps: 1
tasks:
  - id: add-item
    cmd: ["claude", "-p", "add an item", "--output-format", "stream-json"]
    verify:
      cmd: ["verify"]
variants:
  - id: trunk
  - id: patched
`), 0o600))

	projects := filepath.Join(dir, "projects")
	runs := filepath.Join(dir, "runs")
	fixture, err := os.ReadFile(filepath.Join("testdata", "session_marked.jsonl"))
	require.NoError(t, err)

	for _, v := range []string{"trunk", "patched"} {
		sid := "sess-" + v
		dst := filepath.Join(projects, "proj", sid+".jsonl")
		require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, os.WriteFile(dst, fixture, 0o600))
		var out, errb bytes.Buffer
		require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
			task: "add-item", variant: v, rep: 1, sessionID: sid, projectsDir: projects, runsDir: runs,
		}))
		assert.Empty(t, errb.String())
	}

	stubVerify(t)
	var vout, verrb bytes.Buffer
	require.NoError(t, runVerify(context.Background(), &vout, &verrb, basket, verifyFlags{runsDir: runs}))
	assert.Contains(t, vout.String(), "verify import-checkout-add-item-trunk-r1: ok")
	assert.Contains(t, vout.String(), "verify import-checkout-add-item-patched-r1: ok")
	assert.Empty(t, verrb.String())

	var rout, rerrb bytes.Buffer
	rf := regressFlags{
		runsDir:    runs,
		baseline:   "label:variant=trunk",
		candidate:  "label:variant=patched",
		thresholds: regress.DefaultThresholds(),
	}
	require.NoError(t, runRegress(&rout, &rerrb, openStore(nil), newPricer, rf))
	assert.Contains(t, rout.String(), "overall")
	assert.Empty(t, rerrb.String())
}

func TestImportCodexThenVerifyThenRegress(t *testing.T) {
	resetDriftWarnings()
	captureDriftOut(t)
	dir := t.TempDir()
	basket := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(basket, []byte(`basket: checkout
runtime: codex
reps: 1
tasks:
  - id: add-item
    cmd: ["codex", "exec", "add an item", "--json"]
    verify:
      cmd: ["verify"]
variants:
  - id: trunk
  - id: patched
`), 0o600))

	runs := filepath.Join(dir, "runs")
	for i, v := range []string{"trunk", "patched"} {
		sessions := filepath.Join(dir, "sessions-"+v)
		mainID := fmt.Sprintf("019f6b85-%04d-7be3-81dc-ae8563860180", i)
		childID := fmt.Sprintf("019f6b85-%04d-7be3-81dc-ae8563860199", i)
		stageCodexSessionTreeIDs(t, sessions, "0.144.4", mainID, childID)
		var out, errb bytes.Buffer
		require.NoError(t, runImport(context.Background(), &out, &errb, basket, importFlags{
			task: "add-item", variant: v, rep: 1, sessionID: mainID, sessionsDir: sessions, runsDir: runs,
		}))
		assert.Empty(t, errb.String())
	}

	stubVerify(t)
	var vout, verrb bytes.Buffer
	require.NoError(t, runVerify(context.Background(), &vout, &verrb, basket, verifyFlags{runsDir: runs}))
	assert.Contains(t, vout.String(), "verify import-checkout-add-item-trunk-r1: ok")
	assert.Contains(t, vout.String(), "verify import-checkout-add-item-patched-r1: ok")
	assert.Empty(t, verrb.String())

	var rout, rerrb bytes.Buffer
	rf := regressFlags{
		runsDir:    runs,
		baseline:   "label:variant=trunk",
		candidate:  "label:variant=patched",
		thresholds: regress.DefaultThresholds(),
	}
	require.NoError(t, runRegress(&rout, &rerrb, openStore(nil), newPricer, rf))
	assert.Contains(t, rout.String(), "overall")
	assert.Contains(t, rout.String(), "tokens_out")
	assert.Empty(t, rerrb.String())
}

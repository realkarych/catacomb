package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/ingest/drift"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

func captureDriftOut(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := driftOut
	driftOut = &buf
	t.Cleanup(func() { driftOut = orig })
	return &buf
}

func writeDriftyCopy(t *testing.T, src string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	content := strings.TrimRight(string(data), "\n") + "\n" + `{"type":"checkpoint_v9","sessionId":"s1"}` + "\n"
	path := filepath.Join(t.TempDir(), "drifty.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func writeVersionedCopy(t *testing.T, src, version string) string {
	t.Helper()
	data, err := os.ReadFile(src)
	require.NoError(t, err)
	extra := fmt.Sprintf(`{"type":"user","uuid":"uv","sessionId":"s1","timestamp":"2026-06-20T10:00:09Z","version":%q,"message":{"role":"user","content":"ping"}}`, version)
	content := strings.TrimRight(string(data), "\n") + "\n" + extra + "\n"
	path := filepath.Join(t.TempDir(), "versioned.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func TestParseTranscriptsRenumbersSeq(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	sub := filepath.Join(t.TempDir(), "agent-a.jsonl")
	data, err := os.ReadFile(main)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sub, data, 0o600))
	obs, err := parseTranscripts(main, []string{sub}, "exec-1")
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	for i, o := range obs {
		require.Equal(t, uint64(i+1), o.Seq)
		require.Equal(t, "exec-1", o.ExecutionID)
	}
	_, err = parseTranscripts(filepath.Join(t.TempDir(), "absent.jsonl"), nil, "exec-1")
	require.Error(t, err)
}

func TestParseTranscriptsMalformedLine(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	require.NoError(t, os.WriteFile(bad, []byte(`{"type":`), 0o600))
	_, err := parseTranscripts(bad, nil, "exec-1")
	require.Error(t, err)
}

func TestBoundaryObservationsShape(t *testing.T) {
	start, end := time.Unix(10, 0), time.Unix(20, 0)
	obs := boundaryObservations("sess-9", "task:t1", start, end)
	require.Len(t, obs, 2)
	for i, want := range []string{"start", "end"} {
		assert.Equal(t, "marker", obs[i].Kind)
		assert.Equal(t, model.SourceHook, obs[i].Source)
		assert.Equal(t, "task:t1", obs[i].Attrs["name"])
		assert.Equal(t, want, obs[i].Attrs["boundary"])
		assert.Equal(t, "sess-9", obs[i].Correlation.SessionID)
		assert.Equal(t, "sess-9", obs[i].RunID)
	}
	assert.True(t, obs[0].EventTime.Equal(start.UTC()), "start boundary carries the start time")
	assert.True(t, obs[1].EventTime.Equal(end.UTC()), "end boundary carries the end time")
	assert.True(t, obs[0].ObservedAt.Equal(start.UTC()))
	assert.True(t, obs[1].ObservedAt.Equal(end.UTC()))
	assert.NotEqual(t, obs[0].ObsID, obs[1].ObsID)
}

func nodeIDs(nodes []*model.Node) []string {
	ids := make([]string, 0, len(nodes))
	for _, n := range nodes {
		ids = append(ids, n.ID)
	}
	return ids
}

func edgeIDs(edges []*model.Edge) []string {
	ids := make([]string, 0, len(edges))
	for _, e := range edges {
		ids = append(ids, e.ID)
	}
	return ids
}

func TestLoadGraphOfflineInjectsMarkersWithoutMutatingCallerObservations(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	boundary := boundaryObservations("s", "task:demo", time.Unix(1, 0), time.Unix(2, 0))

	plain, err := loadGraphOffline(main, nil, "exec-2", nil, nil)
	require.NoError(t, err)
	require.NotContains(t, graphMarkerNames(plain), "task:demo")

	g, err := loadGraphOffline(main, nil, "exec-2", nil, boundary)
	require.NoError(t, err)
	require.Contains(t, graphMarkerNames(g), "task:demo")

	for _, o := range boundary {
		require.Empty(t, o.ExecutionID)
		require.Zero(t, o.Seq)
	}
}

func TestLoadGraphOfflineIsDeterministicAcrossLoads(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	load := func() ([]string, []string) {
		g, err := loadGraphOffline(main, nil, "exec-2", nil,
			boundaryObservations("s", "task:demo", time.Unix(1, 0), time.Unix(2, 0)))
		require.NoError(t, err)
		nodes, edges := sortedGraphSnapshot(g)
		return nodeIDs(nodes), edgeIDs(edges)
	}
	firstNodes, firstEdges := load()
	secondNodes, secondEdges := load()
	require.NotEmpty(t, firstNodes)
	require.NotEmpty(t, firstEdges)
	require.Equal(t, firstNodes, secondNodes)
	require.Equal(t, firstEdges, secondEdges)
}

func pricedTurnCosts(t *testing.T, g *reduce.Graph) map[string]float64 {
	t.Helper()
	nodes, _ := sortedGraphSnapshot(g)
	out := map[string]float64{}
	for _, n := range nodes {
		if n.Type != model.NodeAssistantTurn {
			require.Nil(t, n.CostUSD, n.ID)
			continue
		}
		if n.CostUSD == nil {
			continue
		}
		out[n.ID] = *n.CostUSD
	}
	return out
}

func TestLoadGraphOfflinePricesAssistantTurnsOnlyWhenPricerGiven(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")

	unpriced, err := loadGraphOffline(main, nil, "exec-3", nil, nil)
	require.NoError(t, err)
	require.Empty(t, pricedTurnCosts(t, unpriced))

	priced, err := loadGraphOffline(main, nil, "exec-3", newPricer(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, pricedTurnCosts(t, priced))

	nodes, _ := sortedGraphSnapshot(priced)
	tokenBearingTurns := 0
	for _, n := range nodes {
		if n.Type != model.NodeAssistantTurn {
			continue
		}
		require.NotNil(t, n.CostUSD, n.ID)
		if n.TokensIn == nil && n.TokensOut == nil {
			assert.Zero(t, *n.CostUSD, n.ID)
			continue
		}
		tokenBearingTurns++
		assert.Positive(t, *n.CostUSD, n.ID)
	}
	require.Positive(t, tokenBearingTurns)
}

func TestLoadGraphOfflineMergesSubagentTranscriptAndFailsOnMissingFile(t *testing.T) {
	main := filepath.Join("testdata", "session.jsonl")
	sub := filepath.Join(t.TempDir(), "agent-a.jsonl")
	data, err := os.ReadFile(main)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(sub, data, 0o600))

	mainOnly, err := loadGraphOffline(main, nil, "exec-3", newPricer(), nil)
	require.NoError(t, err)
	withSub, err := loadGraphOffline(main, []string{sub}, "exec-3", newPricer(), nil)
	require.NoError(t, err)

	mainObs, err := parseTranscripts(main, nil, "exec-3")
	require.NoError(t, err)
	bothObs, err := parseTranscripts(main, []string{sub}, "exec-3")
	require.NoError(t, err)
	require.Len(t, bothObs, 2*len(mainObs))

	mainNodes, _ := sortedGraphSnapshot(mainOnly)
	subNodes, _ := sortedGraphSnapshot(withSub)
	require.Equal(t, nodeIDs(mainNodes), nodeIDs(subNodes))

	_, err = loadGraphOffline(filepath.Join(t.TempDir(), "absent.jsonl"), nil, "exec-3", newPricer(), nil)
	require.Error(t, err)
}

func TestParseTranscriptsWarnsOnUnknownRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	content := `{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"hi"}}` + "\n" +
		`{"type":"checkpoint_v9","sessionId":"s1"}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	buf := captureDriftOut(t)

	obs, err := parseTranscripts(path, nil, "exec-w")
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	assert.Contains(t, buf.String(), "unrecognized transcript record")
	assert.Contains(t, buf.String(), "unknown_record_type=1")
}

func TestParseTranscriptsWarnsWithDriftReasonsInSortedOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "s.jsonl")
	lines := []string{
		`{"type":"user","uuid":"u1","sessionId":"s1","timestamp":"2026-06-20T10:00:00Z","message":{"role":"user","content":"hi"}}`,
		`{"type":"checkpoint_v9","sessionId":"s1"}`,
		`{"type":"assistant","uuid":"a1","sessionId":"s1","timestamp":"not-a-timestamp","message":{"role":"assistant","id":"msg_1","model":"claude-opus-4-8","content":[{"type":"user_prompt_v9"}]}}`,
	}
	require.NoError(t, os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600))

	buf := captureDriftOut(t)
	_, err := parseTranscripts(path, nil, "exec-sorted")
	require.NoError(t, err)

	assert.Equal(t,
		"warning: 3 unrecognized transcript record(s) ["+
			drift.ReasonBadTimestamp+"=1, "+
			drift.ReasonUnknownContentBlock+"=1, "+
			drift.ReasonUnknownRecordType+"=1]\n",
		buf.String())
}

func TestParseTranscriptsNoWarnOnCleanTranscript(t *testing.T) {
	buf := captureDriftOut(t)

	_, err := parseTranscripts(filepath.Join("testdata", "session.jsonl"), nil, "exec-c")
	require.NoError(t, err)
	assert.Empty(t, buf.String())
}

func TestMaxObservedVersionFor(t *testing.T) {
	obs := []model.Observation{
		{Attrs: nil},
		{Attrs: map[string]any{"claude_code_version": 123}},
		{Attrs: map[string]any{"claude_code_version": "1.2.3"}},
		{Attrs: map[string]any{"claude_code_version": "9.9.9"}},
		{Attrs: map[string]any{"claude_code_version": "2.0.0"}},
		{Attrs: map[string]any{"codex_version": "0.150.0"}},
		{Attrs: map[string]any{"codex_version": "0.144.4"}},
	}
	assert.Equal(t, "9.9.9", maxObservedVersionFor(drift.RuntimeClaudeCode, obs))
	assert.Equal(t, "0.150.0", maxObservedVersionFor(drift.RuntimeCodex, obs))
	assert.Equal(t, "", maxObservedVersionFor(drift.RuntimeClaudeCode, nil))
}

func TestWarnVersionForFiresAndStaysSilent(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	warnVersionFor(drift.RuntimeClaudeCode, "9.9.9")
	out := buf.String()
	assert.Contains(t, out, "Claude Code version 9.9.9")
	assert.Contains(t, out, drift.TestedClaudeCodeVersion)
	assert.Contains(t, out, "newer than tested")

	buf.Reset()
	warnVersionFor(drift.RuntimeClaudeCode, drift.TestedClaudeCodeVersion)
	warnVersionFor(drift.RuntimeClaudeCode, "")
	warnVersionFor(drift.RuntimeClaudeCode, "1.0.0")
	warnVersionFor(drift.RuntimeCodex, drift.TestedCodexVersion)
	assert.Empty(t, buf.String())
}

func TestWarnVersionForCodexMessage(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	warnVersionFor(drift.RuntimeCodex, "0.150.0")
	assert.Equal(t, "warning: transcript Codex version 0.150.0 is newer than tested "+drift.TestedCodexVersion+"\n", buf.String())
}

func TestWarnVersionForDedupesPerRuntime(t *testing.T) {
	resetDriftWarnings()
	var buf bytes.Buffer
	old := driftOut
	driftOut = &buf
	defer func() { driftOut = old }()

	high := "999.0.0"
	warnVersionFor(drift.RuntimeClaudeCode, high)
	warnVersionFor(drift.RuntimeClaudeCode, high)
	assert.Equal(t, 1, strings.Count(buf.String(), "newer than tested"))
	warnVersionFor(drift.RuntimeCodex, high)
	assert.Equal(t, 2, strings.Count(buf.String(), "newer than tested"))
	assert.Equal(t, 1, strings.Count(buf.String(), "Codex version"))
}

func TestParseTranscriptsWarnsOnNewerVersion(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	path := writeVersionedCopy(t, filepath.Join("testdata", "session.jsonl"), "9.9.9")
	_, err := parseTranscripts(path, nil, "exec-v")
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "9.9.9")
	assert.Contains(t, buf.String(), "newer than tested")
}

func TestParseTranscriptsNoVersionWarnAtCeiling(t *testing.T) {
	buf := captureDriftOut(t)
	path := writeVersionedCopy(t, filepath.Join("testdata", "session.jsonl"), drift.TestedClaudeCodeVersion)
	_, err := parseTranscripts(path, nil, "exec-v2")
	require.NoError(t, err)
	assert.NotContains(t, buf.String(), "newer than tested")
}

func TestParseTranscriptsWarnsDriftAndVersionTogether(t *testing.T) {
	resetDriftWarnings()
	buf := captureDriftOut(t)
	drifty := writeDriftyCopy(t, filepath.Join("testdata", "session.jsonl"))
	path := writeVersionedCopy(t, drifty, "9.9.9")
	_, err := parseTranscripts(path, nil, "exec-dv")
	require.NoError(t, err)
	out := buf.String()
	assert.Contains(t, out, "unrecognized transcript record")
	assert.Contains(t, out, "newer than tested")
	assert.Contains(t, out, "9.9.9")
}

func TestGraphFromObservationsAppliesExtraAndPricer(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	extra := boundaryObservations("s1", "task:t1", time.Unix(0, 0).UTC(), time.Unix(10, 0).UTC())

	priced := graphFromObservations(obs, "exec-1", newPricer(), extra)
	require.Contains(t, graphMarkerNames(priced), "task:t1")
	costs := pricedTurnCosts(t, priced)
	require.NotEmpty(t, costs)

	obs2, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	unpriced := graphFromObservations(obs2, "exec-1", nil,
		boundaryObservations("s1", "task:t1", time.Unix(0, 0).UTC(), time.Unix(10, 0).UTC()))
	require.Contains(t, graphMarkerNames(unpriced), "task:t1")
	require.Empty(t, pricedTurnCosts(t, unpriced))
}

func TestGraphFromObservationsSequencesExtraAfterTranscript(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	require.NotEmpty(t, obs)
	extra := boundaryObservations("s1", "task:t1", time.Unix(0, 0).UTC(), time.Unix(10, 0).UTC())
	base := len(obs)

	graphFromObservations(obs, "exec-9", nil, extra)

	require.Zero(t, extra[0].Seq)
	require.Zero(t, extra[1].Seq)
	require.Equal(t, uint64(base), obs[base-1].Seq)
}

func TestTranscriptTimeBoundsMatchTranscriptExtremes(t *testing.T) {
	obs, err := parseTranscripts("testdata/session.jsonl", nil, "exec-1")
	require.NoError(t, err)
	require.NotEmpty(t, obs)

	var times []time.Time
	for _, o := range obs {
		if !o.EventTime.IsZero() {
			times = append(times, o.EventTime)
		}
	}
	require.NotEmpty(t, times)
	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	start, end, ok := transcriptTimeBounds(obs)
	require.True(t, ok)
	assert.True(t, start.Equal(times[0]), "start %s want %s", start, times[0])
	assert.True(t, end.Equal(times[len(times)-1]), "end %s want %s", end, times[len(times)-1])
	assert.False(t, start.Equal(end))
}

func TestTranscriptTimeBoundsEmpty(t *testing.T) {
	_, _, ok := transcriptTimeBounds(nil)
	assert.False(t, ok)
}

func TestTranscriptTimeBoundsSkipsZeroAndOutOfOrder(t *testing.T) {
	obs := []model.Observation{
		{EventTime: time.Unix(100, 0).UTC()},
		{},
		{EventTime: time.Unix(50, 0).UTC()},
		{EventTime: time.Unix(200, 0).UTC()},
	}
	start, end, ok := transcriptTimeBounds(obs)
	require.True(t, ok)
	assert.True(t, start.Equal(time.Unix(50, 0).UTC()))
	assert.True(t, end.Equal(time.Unix(200, 0).UTC()))
}

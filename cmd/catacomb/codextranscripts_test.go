package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	codexMainThread  = "019f6b85-627f-7be3-81dc-ae8563860180"
	codexChildThread = "019f6b85-eeee-7be3-81dc-ae8563860199"
	codexGrandThread = "019f6b85-aaaa-7be3-81dc-ae8563860181"
	codexOtherThread = "019f6b85-bbbb-7be3-81dc-ae8563860182"
)

func codexRolloutName(threadID string) string {
	return "rollout-2026-07-16T15-22-11-" + threadID + ".jsonl"
}

func codexMetaLine(t *testing.T, payload map[string]any) []byte {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"timestamp": "2026-07-16T15:22:11.578Z",
		"type":      "session_meta",
		"payload":   payload,
	})
	require.NoError(t, err)
	return append(line, '\n')
}

func codexDayDir(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "2026", "07", "16")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	return dir
}

func stageCodexRollout(t *testing.T, root, threadID string, payload map[string]any) string {
	t.Helper()
	p := filepath.Join(codexDayDir(t, root), codexRolloutName(threadID))
	require.NoError(t, os.WriteFile(p, codexMetaLine(t, payload), 0o600))
	return p
}

func writeZstFile(t *testing.T, path string, data []byte) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	enc, err := zstd.NewWriter(f)
	require.NoError(t, err)
	_, err = enc.Write(data)
	require.NoError(t, err)
	require.NoError(t, enc.Close())
	require.NoError(t, f.Close())
}

func mainPayload() map[string]any {
	return map[string]any{
		"session_id": codexMainThread, "id": codexMainThread,
		"cwd": "/w", "cli_version": "0.144.4", "source": "exec",
	}
}

func childPayload(threadID, parent string) map[string]any {
	return map[string]any{
		"session_id": threadID, "id": threadID,
		"cwd": "/w", "cli_version": "0.144.4",
		"parent_thread_id": parent,
	}
}

func nestedChildPayload(threadID, parent string) map[string]any {
	return map[string]any{
		"id":  threadID,
		"cwd": "/w", "cli_version": "0.144.4",
		"source": map[string]any{"subagent": map[string]any{"thread_spawn": map[string]any{
			"parent_thread_id": parent, "depth": 1, "agent_role": "explorer",
		}}},
	}
}

func TestResolveCodexTranscriptsDiscoversDescendants(t *testing.T) {
	root := t.TempDir()
	main := stageCodexRollout(t, root, codexMainThread, mainPayload())
	child := stageCodexRollout(t, root, codexChildThread, childPayload(codexChildThread, codexMainThread))
	grand := stageCodexRollout(t, root, codexGrandThread, nestedChildPayload(codexGrandThread, codexChildThread))
	stageCodexRollout(t, root, codexOtherThread, childPayload(codexOtherThread, "unrelated-thread"))

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Equal(t, main, ts.Main)
	want := []string{grand, child}
	if want[0] > want[1] {
		want[0], want[1] = want[1], want[0]
	}
	assert.Equal(t, want, ts.Subagents)
}

func TestResolveCodexTranscriptsZstMain(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(codexDayDir(t, root), codexRolloutName(codexMainThread)+".zst")
	writeZstFile(t, p, codexMetaLine(t, mainPayload()))

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Equal(t, p, ts.Main)
	assert.Empty(t, ts.Subagents)
}

func TestResolveCodexTranscriptsZstChild(t *testing.T) {
	root := t.TempDir()
	stageCodexRollout(t, root, codexMainThread, mainPayload())
	p := filepath.Join(codexDayDir(t, root), codexRolloutName(codexChildThread)+".zst")
	writeZstFile(t, p, codexMetaLine(t, childPayload(codexChildThread, codexMainThread)))

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Equal(t, []string{p}, ts.Subagents)
}

func TestResolveCodexTranscriptsRejectsThreadIDFragment(t *testing.T) {
	root := t.TempDir()
	full := "019f6b85-627f-7be3-81dc-ae8563860201"
	fragment := "ae8563860201"
	p := filepath.Join(codexDayDir(t, root), "rollout-2026-07-16T15-40-00-"+full+".jsonl")
	require.NoError(t, os.WriteFile(p, codexMetaLine(t, map[string]any{
		"session_id": full, "id": full,
		"cwd": "/w", "cli_version": "0.144.4", "source": "exec",
	}), 0o600))

	_, err := resolveCodexTranscripts(root, fragment)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transcript for session "+fragment+" under "+root)

	ts, err := resolveCodexTranscripts(root, full)
	require.NoError(t, err)
	assert.Equal(t, p, ts.Main)
}

func TestResolveCodexTranscriptsNotFound(t *testing.T) {
	root := t.TempDir()
	_, err := resolveCodexTranscripts(root, codexMainThread)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no transcript for session "+codexMainThread+" under "+root)
}

func TestResolveCodexTranscriptsAmbiguous(t *testing.T) {
	root := t.TempDir()
	stageCodexRollout(t, root, codexMainThread, mainPayload())
	p := filepath.Join(codexDayDir(t, root), codexRolloutName(codexMainThread)+".zst")
	writeZstFile(t, p, codexMetaLine(t, mainPayload()))

	_, err := resolveCodexTranscripts(root, codexMainThread)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous session "+codexMainThread+": 2 matches")
}

func TestResolveCodexTranscriptsBadPattern(t *testing.T) {
	root := t.TempDir()
	stageCodexRollout(t, root, codexMainThread, mainPayload())
	_, err := resolveCodexTranscripts(root, "[a")
	require.ErrorIs(t, err, filepath.ErrBadPattern)
}

func TestResolveCodexTranscriptsCycleGuard(t *testing.T) {
	root := t.TempDir()
	main := stageCodexRollout(t, root, codexMainThread, mainPayload())
	child := stageCodexRollout(t, root, codexChildThread, childPayload(codexChildThread, codexMainThread))
	grand := stageCodexRollout(t, root, codexGrandThread, childPayload(codexGrandThread, codexChildThread))
	back := filepath.Join(codexDayDir(t, root), "rollout-2026-07-16T15-23-11-"+codexChildThread+".jsonl")
	require.NoError(t, os.WriteFile(back, codexMetaLine(t, childPayload(codexChildThread, codexGrandThread)), 0o600))

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Equal(t, main, ts.Main)
	assert.ElementsMatch(t, []string{child, grand}, ts.Subagents)
}

func TestResolveCodexTranscriptsUnreachableCyclePair(t *testing.T) {
	root := t.TempDir()
	stageCodexRollout(t, root, codexMainThread, mainPayload())
	stageCodexRollout(t, root, codexChildThread, childPayload(codexChildThread, codexGrandThread))
	stageCodexRollout(t, root, codexGrandThread, childPayload(codexGrandThread, codexChildThread))

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Empty(t, ts.Subagents)
}

func TestResolveCodexTranscriptsSkipsUndecodableCandidates(t *testing.T) {
	root := t.TempDir()
	main := stageCodexRollout(t, root, codexMainThread, mainPayload())
	child := stageCodexRollout(t, root, codexChildThread, childPayload(codexChildThread, codexMainThread))
	day := codexDayDir(t, root)
	require.NoError(t, os.WriteFile(filepath.Join(day, codexRolloutName("garbage-1111")), []byte("not json\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(day, codexRolloutName("empty-2222")), nil, 0o600))
	notMeta := `{"timestamp":"2026-07-16T15:22:11.578Z","type":"turn_context","payload":{"turn_id":"T1"}}` + "\n"
	require.NoError(t, os.WriteFile(filepath.Join(day, codexRolloutName("nometa-3333")), []byte(notMeta), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(day, codexRolloutName("badzst-4444")+".zst"), []byte("not zstd"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(day, "notes.txt"), []byte("skip me"), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(day, codexRolloutName("adir-5555")), 0o755))

	ts, err := resolveCodexTranscripts(root, codexMainThread)
	require.NoError(t, err)
	assert.Equal(t, main, ts.Main)
	assert.Equal(t, []string{child}, ts.Subagents)
}

func TestCodexThreadIDFromFilename(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"rollout-2026-07-16T15-22-11-" + codexMainThread + ".jsonl", codexMainThread},
		{"rollout-2026-07-16T15-22-11-" + codexMainThread + ".jsonl.zst", codexMainThread},
		{"rollout-2026-07-16T15-22-11-.jsonl", ""},
		{"rollout-notatimestamp-abc.jsonl", ""},
		{"agent-1.jsonl", ""},
		{"rollout-2026-07-16T15-22-11-abc.jsonl.gz", ""},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, codexThreadIDFromFilename(tc.name), tc.name)
	}
}

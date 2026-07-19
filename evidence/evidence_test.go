package evidence_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/evidence"
)

func sampleMeta(runID, variant string) evidence.Meta {
	return evidence.Meta{
		RunID: runID, Task: "t1", Variant: variant, Rep: 1,
		SessionID: "sess-1", Labels: map[string]string{"basket": "b", "variant": variant},
		ExitCode: 0, BasketHash: "h", MarkerName: "task:t1",
		MarkerStart: time.Unix(100, 0).UTC(), MarkerEnd: time.Unix(200, 0).UTC(),
		FinishedAt: time.Unix(201, 0).UTC(),
	}
}

func TestWriteReadRoundtrip(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{\"a\":1}\n{\"b\":2}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-1")
	m := sampleMeta("run-1", "base")
	require.NoError(t, evidence.Write(dir, m, []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
	got, err := evidence.ReadMeta(dir)
	require.NoError(t, err)
	require.Equal(t, m, got)
	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1}\n{\"b\":2}\n", string(copied), "secret-free lines must be copied through byte-identical")
}

func TestWriteRedactsEachSourceLineIndependently(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	body := "{\"tool\":\"Bash\",\"cmd\":\"aws configure set key AKIAIOSFODNN7EXAMPLE\"}\n" +
		"{\"tool\":\"Read\",\"file\":\"main.go\"}\n" +
		"{\"password\":\"hunter2\",\"note\":\"keep me\"}\n"
	require.NoError(t, os.WriteFile(src, []byte(body), 0o600))
	dir := filepath.Join(t.TempDir(), "run-red")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-red", "base"), []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))

	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t,
		"{\"cmd\":\"aws configure set key ‹redacted:aws-key›\",\"tool\":\"Bash\"}\n"+
			"{\"tool\":\"Read\",\"file\":\"main.go\"}\n"+
			"{\"note\":\"keep me\",\"password\":\"‹redacted:sensitive-key›\"}\n",
		string(copied),
	)
}

func TestWriteKeepsOneOutputLinePerNonEmptySourceLine(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{\"a\":1}\n\n{\"b\":2}\n\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-blank")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-blank", "base"), []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))

	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1}\n{\"b\":2}\n", string(copied), "blank lines are dropped; every other line survives exactly once")
}

func TestWriteAppendsMissingFinalNewline(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{\"a\":1}\n{\"b\":2}"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-nonl")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-nonl", "base"), []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))

	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1}\n{\"b\":2}\n", string(copied), "a final line without a newline must still be emitted, newline-terminated")
}

func TestWriteDecompressesZstSources(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl.zst")
	f, err := os.Create(src)
	require.NoError(t, err)
	enc, err := zstd.NewWriter(f)
	require.NoError(t, err)
	_, err = enc.Write([]byte("{\"a\":1}\n{\"b\":2}\n"))
	require.NoError(t, err)
	require.NoError(t, enc.Close())
	require.NoError(t, f.Close())

	dir := filepath.Join(t.TempDir(), "run-z")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-z", "base"), []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}}))
	copied, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	require.Equal(t, "{\"a\":1}\n{\"b\":2}\n", string(copied))
}

func TestWriteCorruptZstSourceFailsWithoutStampingMeta(t *testing.T) {
	src := filepath.Join(t.TempDir(), "bad.jsonl.zst")
	require.NoError(t, os.WriteFile(src, []byte("not zstd"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-bz")
	err := evidence.Write(dir, sampleMeta("run-bz", "base"), []evidence.SourceFile{{Src: src, Rel: "session.jsonl"}})
	require.Error(t, err)

	_, serr := os.Stat(filepath.Join(dir, "meta.json"))
	require.ErrorIs(t, serr, os.ErrNotExist, "a failed Write must not leave a meta.json that makes the dir look like a complete run")
	_, merr := evidence.ReadMeta(dir)
	require.ErrorIs(t, merr, os.ErrNotExist, "ScanRuns must not be able to pick this half-written dir up as a run")
}

func TestWriteNestedRelAndErrors(t *testing.T) {
	src := filepath.Join(t.TempDir(), "agent.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-2")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-2", "base"), []evidence.SourceFile{{Src: src, Rel: filepath.Join("subagents", "agent-1.jsonl")}}))
	_, err := os.Stat(filepath.Join(dir, "subagents", "agent-1.jsonl"))
	require.NoError(t, err)
	require.Error(t, evidence.Write(filepath.Join(t.TempDir(), "run-3"), sampleMeta("run-3", "base"), []evidence.SourceFile{{Src: filepath.Join(t.TempDir(), "missing.jsonl"), Rel: "session.jsonl"}}))
}

func TestWriteRejectsNonLocalRel(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-x")
	sentinel := filepath.Join(dir, "keep.txt")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	require.NoError(t, os.WriteFile(sentinel, []byte("x"), 0o600))
	abs := filepath.Join(t.TempDir(), "abs.jsonl")
	for _, rel := range []string{filepath.Join("..", "x"), abs} {
		err := evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{{Src: src, Rel: rel}})
		require.Error(t, err)
		require.Contains(t, err.Error(), "rel")
		require.Contains(t, err.Error(), strconv.Quote(rel))
		_, serr := os.Stat(sentinel)
		require.NoError(t, serr, "target dir must stay untouched")
	}
}

func TestWriteRemovesStaleFiles(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-1")
	require.NoError(t, evidence.Write(dir, sampleMeta("run-1", "base"), []evidence.SourceFile{
		{Src: src, Rel: "session.jsonl"},
		{Src: src, Rel: filepath.Join("subagents", "agent-1.jsonl")},
	}))
	require.NoError(t, evidence.Write(dir, sampleMeta("run-1", "base"), []evidence.SourceFile{
		{Src: src, Rel: "session.jsonl"},
	}))
	_, err := os.Stat(filepath.Join(dir, "subagents", "agent-1.jsonl"))
	require.ErrorIs(t, err, os.ErrNotExist)
	_, err = os.Stat(filepath.Join(dir, "session.jsonl"))
	require.NoError(t, err)
	_, err = os.Stat(filepath.Join(dir, "meta.json"))
	require.NoError(t, err)
}

func TestMetaEnvJSONShape(t *testing.T) {
	tests := []struct {
		name string
		env  *evidence.EnvStamps
		want map[string]any
	}{
		{name: "nil env omits key", env: nil, want: nil},
		{
			name: "full env",
			env: &evidence.EnvStamps{
				CatacombVersion:   "1.2.3",
				ModelID:           "claude-opus-4-8",
				ClaudeCodeVersion: "2.1.100",
				AgentRuntime:      "claude-code",
				AgentVersion:      "2.1.100",
				Resources:         evidence.Resources{OS: "linux", Arch: "amd64", CPUs: 8},
			},
			want: map[string]any{
				"catacomb_version":    "1.2.3",
				"model_id":            "claude-opus-4-8",
				"claude_code_version": "2.1.100",
				"agent_runtime":       "claude-code",
				"agent_version":       "2.1.100",
				"resources":           map[string]any{"os": "linux", "arch": "amd64", "cpus": float64(8)},
			},
		},
		{
			name: "codex env leaves claude code version out",
			env: &evidence.EnvStamps{
				CatacombVersion: "1.2.3",
				ModelID:         "gpt-5.4-mini",
				AgentRuntime:    "codex",
				AgentVersion:    "0.144.4",
				Resources:       evidence.Resources{OS: "linux", Arch: "amd64", CPUs: 8},
			},
			want: map[string]any{
				"catacomb_version": "1.2.3",
				"model_id":         "gpt-5.4-mini",
				"agent_runtime":    "codex",
				"agent_version":    "0.144.4",
				"resources":        map[string]any{"os": "linux", "arch": "amd64", "cpus": float64(8)},
			},
		},
		{
			name: "empty model, versions, and runtime omitted",
			env: &evidence.EnvStamps{
				CatacombVersion: "1.2.3",
				Resources:       evidence.Resources{OS: "darwin", Arch: "arm64", CPUs: 1},
			},
			want: map[string]any{
				"catacomb_version": "1.2.3",
				"resources":        map[string]any{"os": "darwin", "arch": "arm64", "cpus": float64(1)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := sampleMeta("run-env", "base")
			m.Env = tt.env
			data, err := json.Marshal(m)
			require.NoError(t, err)
			var raw map[string]any
			require.NoError(t, json.Unmarshal(data, &raw))
			gotEnv, ok := raw["env"]
			if tt.want == nil {
				require.False(t, ok)
				return
			}
			require.True(t, ok)
			require.Equal(t, tt.want, gotEnv)
			var back evidence.Meta
			require.NoError(t, json.Unmarshal(data, &back))
			require.Equal(t, m, back)
		})
	}
}

func TestWriteReadRoundtripWithEnv(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run-env")
	m := sampleMeta("run-env", "base")
	m.Env = &evidence.EnvStamps{
		CatacombVersion:   "9.9.9",
		ModelID:           "m-1",
		ClaudeCodeVersion: "2.0.0",
		Resources:         evidence.Resources{OS: "linux", Arch: "arm64", CPUs: 4},
	}
	require.NoError(t, evidence.Write(dir, m, nil))
	got, err := evidence.ReadMeta(dir)
	require.NoError(t, err)
	require.Equal(t, m, got)
}

func TestReadMetaLegacyWithoutEnv(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run-legacy")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	legacy := `{"run_id":"run-legacy","task":"t1","variant":"base","rep":1,` +
		`"session_id":"s","exit_code":0,"basket_hash":"h","marker_name":"task:t1",` +
		`"marker_start":"2026-06-20T10:00:00Z","marker_end":"2026-06-20T10:01:00Z",` +
		`"finished_at":"2026-06-20T10:01:01Z"}`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte(legacy), 0o600))
	got, err := evidence.ReadMeta(dir)
	require.NoError(t, err)
	require.Nil(t, got.Env)
	require.Equal(t, "run-legacy", got.RunID)
	runs, err := evidence.ScanRuns(root)
	require.NoError(t, err)
	require.Len(t, runs, 1)
	require.Nil(t, runs[0].Meta.Env)
}

func TestReadMetaMissingFileIsNotExist(t *testing.T) {
	_, err := evidence.ReadMeta(t.TempDir())
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestReadMetaMalformedJSONIsSyntaxError(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "meta.json"), []byte("{"), 0o600))
	_, err := evidence.ReadMeta(dir)
	require.Error(t, err)
	var syntax *json.SyntaxError
	require.ErrorAs(t, err, &syntax)
}

func TestScanRunsReturnsRunsSortedByRunID(t *testing.T) {
	root := t.TempDir()
	dirToRun := map[string]string{"01": "run-d", "02": "run-b", "03": "run-c", "04": "run-a"}
	for dir, id := range dirToRun {
		require.NoError(t, evidence.Write(filepath.Join(root, dir), sampleMeta(id, "base"), nil))
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "junk"), 0o700))

	runs, err := evidence.ScanRuns(root)
	require.NoError(t, err)

	ids := make([]string, len(runs))
	dirs := make([]string, len(runs))
	for i, r := range runs {
		ids[i] = r.Meta.RunID
		dirs[i] = filepath.Base(r.Dir)
	}
	require.Equal(t, []string{"run-a", "run-b", "run-c", "run-d"}, ids, "ScanRuns must order by RunID, not by directory-listing order")
	require.Equal(t, []string{"04", "02", "03", "01"}, dirs, "each Run must keep the Dir it was read from, so RunID order is not dir order")
}

func TestScanRunsMissingRootErrors(t *testing.T) {
	_, err := evidence.ScanRuns(filepath.Join(t.TempDir(), "absent"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestMatchLabels(t *testing.T) {
	tests := []struct {
		name string
		have map[string]string
		want map[string]string
		ok   bool
	}{
		{"empty want matches anything", map[string]string{"a": "1"}, nil, true},
		{"subset matches", map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1"}, true},
		{"exact match", map[string]string{"a": "1"}, map[string]string{"a": "1"}, true},
		{"all keys must match, not just one", map[string]string{"a": "1", "b": "2"}, map[string]string{"a": "1", "b": "9"}, false},
		{"value mismatch fails", map[string]string{"a": "1"}, map[string]string{"a": "2"}, false},
		{"missing key fails", map[string]string{"a": "1"}, map[string]string{"z": "1"}, false},
		{"missing key wanting empty value matches", map[string]string{"a": "1"}, map[string]string{"z": ""}, true},
		{"nil have with non-empty want fails", nil, map[string]string{"a": "1"}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.ok, evidence.MatchLabels(tc.have, tc.want))
		})
	}
}

func TestScanRunsSkipsPlainFiles(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "stray.txt"), []byte("x"), 0o600))
	runs, err := evidence.ScanRuns(root)
	require.NoError(t, err)
	require.Empty(t, runs)
}

func TestWriteReplaceDirError(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0o600))
	require.Error(t, evidence.Write(filepath.Join(blocker, "run-x"), sampleMeta("run-x", "base"), nil))
}

func TestWriteMarshalError(t *testing.T) {
	m := sampleMeta("run-x", "base")
	m.FinishedAt = time.Date(10001, 1, 1, 0, 0, 0, 0, time.UTC)
	require.Error(t, evidence.Write(filepath.Join(t.TempDir(), "run-x"), m, nil))
}

func TestWriteMetaFileError(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-x")
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "alt"), []evidence.SourceFile{{Src: src, Rel: filepath.Join("meta.json", "x.jsonl")}}))
}

func TestWriteCopyDestErrors(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.jsonl")
	require.NoError(t, os.WriteFile(src, []byte("{}\n"), 0o600))
	dir := filepath.Join(t.TempDir(), "run-x")
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{
		{Src: src, Rel: "sub"},
		{Src: src, Rel: filepath.Join("sub", "a.jsonl")},
	}))
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{
		{Src: src, Rel: filepath.Join("taken", "x.jsonl")},
		{Src: src, Rel: "taken"},
	}))
}

func TestWriteSrcReadError(t *testing.T) {
	srcDir := t.TempDir()
	dir := filepath.Join(t.TempDir(), "run-x")
	require.Error(t, evidence.Write(dir, sampleMeta("run-x", "base"), []evidence.SourceFile{{Src: srcDir, Rel: "session.jsonl"}}))
}

func TestEnvStampsWorkspaceSerialization(t *testing.T) {
	with := evidence.EnvStamps{
		CatacombVersion: "v",
		Workspace:       &evidence.WorkspaceStamp{Rev: "r42", PatchSHA256: "ab34"},
	}
	data, err := json.Marshal(with)
	require.NoError(t, err)
	require.Contains(t, string(data), `"workspace":{"rev":"r42","patch_sha256":"ab34"}`)

	without := evidence.EnvStamps{CatacombVersion: "v"}
	data, err = json.Marshal(without)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))
	require.NotContains(t, m, "workspace")

	partial := evidence.EnvStamps{Workspace: &evidence.WorkspaceStamp{Rev: "r42"}}
	data, err = json.Marshal(partial)
	require.NoError(t, err)
	require.Contains(t, string(data), `"workspace":{"rev":"r42"}`)
}

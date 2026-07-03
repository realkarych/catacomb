package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
	"github.com/realkarych/catacomb/daemon"
)

type markRecorder struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (m *markRecorder) add(b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bodies = append(m.bodies, append([]byte(nil), b...))
}

func (m *markRecorder) snapshot() [][]byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([][]byte, len(m.bodies))
	copy(out, m.bodies)
	return out
}

type graphRecorder struct {
	mu    sync.Mutex
	count int
	fail  bool
	byID  map[string][]string
}

func (g *graphRecorder) requests() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.count
}

func (g *graphRecorder) setMarkers(id string, names ...string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.byID[id] = names
}

func (g *graphRecorder) setFail(v bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fail = v
}

func graphMarkerEvents(names []string) []map[string]any {
	evs := make([]map[string]any, 0, len(names))
	for _, n := range names {
		evs = append(evs, map[string]any{
			"kind": "node_upsert",
			"node": map[string]any{"type": "marker", "name": n},
		})
	}
	return evs
}

func benchServerWithGraph(t *testing.T, markStatus int) (string, *markRecorder, *graphRecorder) {
	t.Helper()
	rec := &markRecorder{}
	g := &graphRecorder{byID: map[string][]string{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if r.URL.Path == "/v1/mark" {
			rec.add(body)
			w.WriteHeader(markStatus)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/sessions/") && strings.HasSuffix(r.URL.Path, "/graph") {
			g.mu.Lock()
			g.count++
			fail := g.fail
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/sessions/"), "/graph")
			names := g.byID[id]
			g.mu.Unlock()
			if fail {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(graphMarkerEvents(names))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: srv.Listener.Addr().String(), Token: "tok"}))
	return discovery, rec, g
}

func benchServer(t *testing.T, status int) (string, *markRecorder) {
	t.Helper()
	discovery, rec, _ := benchServerWithGraph(t, status)
	return discovery, rec
}

func fakeBenchExec(t *testing.T) {
	t.Helper()
	t.Setenv("GO_WANT_BENCH_HELPER", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestBenchHelperProcess", "--", name}, args...)
		return exec.Command(os.Args[0], cs...)
	}
	t.Cleanup(func() { execCommand = orig })
}

func writeBasket(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

func readManifest(t *testing.T, path string) []bench.ManifestEntry {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var out []bench.ManifestEntry
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var e bench.ManifestEntry
		require.NoError(t, json.Unmarshal(line, &e))
		out = append(out, e)
	}
	return out
}

func TestBenchHelperProcess(t *testing.T) {
	t.Helper()
	if os.Getenv("GO_WANT_BENCH_HELPER") != "1" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	if len(args) == 0 {
		os.Exit(0)
	}
	switch args[0] {
	case "OK":
		os.Exit(0)
	case "BOOM":
		os.Exit(3)
	case "CHILD":
		fmt.Fprintf(os.Stdout, "{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":%q}\n", os.Getenv("CATACOMB_RUN_ID"))
		fmt.Fprintln(os.Stdout, `{"type":"result","session_id":"ignored"}`)
		os.Exit(0)
	case "CHILD_FAIL":
		fmt.Fprintf(os.Stdout, "{\"type\":\"system\",\"subtype\":\"init\",\"session_id\":%q}\n", os.Getenv("CATACOMB_RUN_ID"))
		os.Exit(5)
	case "NOSESSION":
		fmt.Fprintln(os.Stdout, `{"type":"assistant"}`)
		os.Exit(0)
	case "CWD":
		wd, _ := os.Getwd()
		if resolved, err := filepath.EvalSymlinks(wd); err == nil {
			wd = resolved
		}
		fmt.Fprintln(os.Stdout, "CWD="+wd)
		os.Exit(0)
	case "CHILD_ENV":
		for _, k := range []string{"CATACOMB_RUN_ID", "CATACOMB_LABELS", "TASKONLY", "VARONLY", "SHARED"} {
			if v := os.Getenv(k); v != "" {
				fmt.Fprintf(os.Stdout, "%s=%s\n", k, v)
			}
		}
		os.Exit(0)
	}
	os.Exit(0)
}

func TestBenchDryRunPrintsTableAndExitsZero(t *testing.T) {
	spawned := false
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		spawned = true
		return exec.Command(os.Args[0])
	}
	t.Cleanup(func() { execCommand = orig })

	path := writeBasket(t, `basket: bdry
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--dry-run"}, &out, &errBuf)
	assert.Equal(t, 0, code)
	assert.Contains(t, out.String(), "RUN_ID")
	assert.Contains(t, out.String(), "TASK")
	assert.Contains(t, out.String(), "VARIANT")
	assert.Contains(t, out.String(), "REP")
	assert.Contains(t, out.String(), "bench-bdry-t1-v1-r1")
	assert.False(t, spawned)
	_, statErr := os.Stat(path + ".manifest.jsonl")
	assert.True(t, os.IsNotExist(statErr))
}

func TestBenchExecutesCellsInOrder(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bord
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
  - id: t2
    cmd: ["CHILD"]
variants:
  - id: v1
  - id: v2
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	order := []string{
		"bench-bord-t1-v1-r1",
		"bench-bord-t1-v2-r1",
		"bench-bord-t2-v1-r1",
		"bench-bord-t2-v2-r1",
	}
	prev := -1
	for _, id := range order {
		idx := strings.Index(out.String(), id)
		require.GreaterOrEqual(t, idx, 0, "missing %s", id)
		assert.Greater(t, idx, prev, "out of order: %s", id)
		prev = idx
	}
	entries := readManifest(t, manifest)
	require.Len(t, entries, 4)
	for i, id := range order {
		assert.Equal(t, id, entries[i].RunID)
	}
	assert.Contains(t, out.String(), "marked 4/4 cells")
}

func TestBenchChildReceivesEnvLabelsRunID(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	t.Setenv("CATACOMB_LABELS", "basket=OTHER,env=prod")

	path := writeBasket(t, `basket: benv
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD_ENV"]
    env:
      TASKONLY: fromtask
      SHARED: task
variants:
  - id: v1
    env:
      VARONLY: fromvar
      SHARED: variant
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	s := out.String()
	assert.Contains(t, s, "CATACOMB_RUN_ID=bench-benv-t1-v1-r1")
	assert.Contains(t, s, "CATACOMB_LABELS=basket=benv,env=prod,rep=1,task=t1,variant=v1")
	assert.Contains(t, s, "TASKONLY=fromtask")
	assert.Contains(t, s, "VARONLY=fromvar")
	assert.Contains(t, s, "SHARED=variant")
	assert.NotContains(t, s, "SHARED=task")
}

func TestBenchResumeSkipsCompleted(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bres
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
  - id: v2
`)
	_, hash, err := bench.Load(path)
	require.NoError(t, err)

	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	done := bench.ManifestEntry{RunID: "bench-bres-t1-v1-r1", Task: "t1", Variant: "v1", Rep: 1, BasketHash: hash}
	raw, _ := json.Marshal(done)
	require.NoError(t, os.WriteFile(manifest, append(raw, '\n'), 0o600))

	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest, "--resume"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.Contains(t, out.String(), "skip bench-bres-t1-v1-r1")
	assert.Contains(t, out.String(), "bench-bres-t1-v2-r1")
	assert.NotContains(t, out.String(), `"session_id":"bench-bres-t1-v1-r1"`)

	entries := readManifest(t, manifest)
	require.Len(t, entries, 2)
	assert.Equal(t, "bench-bres-t1-v2-r1", entries[1].RunID)
}

func TestBenchResumeAllSkippedOmitsCheckpointSummary(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, _ := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeCheckpointBasket(t, "resall", "tasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n    checkpoints: [plan]\n")
	_, hash, err := bench.Load(path)
	require.NoError(t, err)

	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	done := bench.ManifestEntry{RunID: "bench-resall-t1-v1-r1", Task: "t1", Variant: "v1", Rep: 1, BasketHash: hash}
	raw, _ := json.Marshal(done)
	require.NoError(t, os.WriteFile(manifest, append(raw, '\n'), 0o600))

	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest, "--resume"}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.Contains(t, out.String(), "skip bench-resall-t1-v1-r1")
	assert.NotContains(t, out.String(), "checkpoints[")
	assert.NotContains(t, out.String(), "marked")
}

func TestBenchDefaultManifestPath(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bdef
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, path+".manifest.jsonl")
	require.Len(t, entries, 1)
	assert.Equal(t, "bench-bdef-t1-v1-r1", entries[0].RunID)
}

func TestBenchResumeCompletedError(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: brce
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifestDir := filepath.Join(t.TempDir(), "manifest-is-a-dir")
	require.NoError(t, os.Mkdir(manifestDir, 0o755))

	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifestDir, "--resume"}, &out, &errBuf)
	assert.Equal(t, 2, code)
}

func TestBenchResumeHashMismatchIsOperational(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bmis
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	done := bench.ManifestEntry{RunID: "bench-bmis-t1-v1-r1", Task: "t1", Variant: "v1", Rep: 1, BasketHash: "deadbeef"}
	raw, _ := json.Marshal(done)
	require.NoError(t, os.WriteFile(manifest, append(raw, '\n'), 0o600))

	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest, "--resume"}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "delete the manifest")
}

func TestBenchFailFastStops(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bff
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD_FAIL"]
variants:
  - id: v1
  - id: v2
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest, "--fail-fast"}, &out, &errBuf)
	assert.Equal(t, 1, code)

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, "bench-bff-t1-v1-r1", entries[0].RunID)
	assert.Equal(t, 5, entries[0].ExitCode)
	assert.NotContains(t, out.String(), "bench-bff-t1-v2-r1")
	assert.NotContains(t, out.String(), "catacomb baseline set")
}

func TestBenchNonZeroCellExitDoesNotFailRun(t *testing.T) {
	fakeBenchExec(t)
	discovery, rec := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bnz
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD_FAIL"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, 5, entries[0].ExitCode)
	assert.True(t, entries[0].Marked)
	assert.Equal(t, "bench-bnz-t1-v1-r1", entries[0].SessionID)
	assert.Contains(t, out.String(), "catacomb baseline set")

	bodies := rec.snapshot()
	require.Len(t, bodies, 2)
	var start, end map[string]any
	require.NoError(t, json.Unmarshal(bodies[0], &start))
	require.NoError(t, json.Unmarshal(bodies[1], &end))
	assert.Equal(t, "start", start["boundary"])
	assert.Equal(t, "task:t1", start["name"])
	assert.Equal(t, "bench-bnz-t1-v1-r1", start["session_id"])
	assert.Equal(t, "end", end["boundary"])
	assert.Equal(t, "task:t1", end["name"])
}

func TestBenchSetupFailureRecordedAndContinues(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bset
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
    setup: ["BOOM"]
  - id: v2
    setup: ["OK"]
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 2)
	assert.Equal(t, "bench-bset-t1-v1-r1", entries[0].RunID)
	assert.Equal(t, 3, entries[0].ExitCode)
	assert.Equal(t, "setup failed", entries[0].Note)
	assert.False(t, entries[0].Marked)
	assert.Equal(t, "bench-bset-t1-v2-r1", entries[1].RunID)
	assert.Equal(t, 0, entries[1].ExitCode)
	assert.NotContains(t, out.String(), `"session_id":"bench-bset-t1-v1-r1"`)
	assert.Contains(t, out.String(), `"session_id":"bench-bset-t1-v2-r1"`)
}

func TestBenchSetupStartErrorRecorded(t *testing.T) {
	t.Setenv("GO_WANT_BENCH_HELPER", "1")
	orig := execCommand
	execCommand = func(name string, args ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "nope-binary"))
	}
	t.Cleanup(func() { execCommand = orig })
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: berr
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
    setup: ["OK"]
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, -1, entries[0].ExitCode)
	assert.Equal(t, "setup failed", entries[0].Note)
}

func TestBenchMarkersPostedWithSessionID(t *testing.T) {
	fakeBenchExec(t)
	discovery, rec := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bmark
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
    setup: ["", "OK"]
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	bodies := rec.snapshot()
	require.Len(t, bodies, 2)
	var start, end map[string]any
	require.NoError(t, json.Unmarshal(bodies[0], &start))
	require.NoError(t, json.Unmarshal(bodies[1], &end))
	assert.Equal(t, "bench-bmark-t1-v1-r1", start["session_id"])
	assert.Equal(t, "task:t1", start["name"])
	assert.Equal(t, "start", start["boundary"])
	assert.Equal(t, "end", end["boundary"])
	assert.Equal(t, "task:t1", end["name"])

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.True(t, entries[0].Marked)
	assert.Equal(t, "bench-bmark-t1-v1-r1", entries[0].SessionID)
	assert.Empty(t, entries[0].Note)
}

func TestBenchMarkedFalseWithoutSession(t *testing.T) {
	fakeBenchExec(t)
	discovery, rec := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bnos
reps: 1
tasks:
  - id: t1
    cmd: ["NOSESSION"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.Empty(t, rec.snapshot())
	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.False(t, entries[0].Marked)
	assert.Equal(t, "no session id observed", entries[0].Note)
	assert.Empty(t, entries[0].SessionID)
}

func TestBenchMarkerFailureRecordsNote(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusInternalServerError)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bmf
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.False(t, entries[0].Marked)
	assert.Equal(t, "marker failed", entries[0].Note)
	assert.Equal(t, "bench-bmf-t1-v1-r1", entries[0].SessionID)
	assert.Equal(t, 0, entries[0].ExitCode)
}

func TestBenchEpilogueSingleVariant(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bepi
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.Contains(t, out.String(), "catacomb baseline set bepi-v1 --label basket=bepi,variant=v1")
	assert.NotContains(t, out.String(), "catacomb regress")
}

func TestBenchEpilogueTwoVariants(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bepi2
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
  - id: v2
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.Contains(t, out.String(), "catacomb baseline set bepi2-v1 --label basket=bepi2,variant=v1")
	assert.Contains(t, out.String(), "catacomb regress --baseline label:basket=bepi2,variant=v1 --candidate label:basket=bepi2,variant=v2")
}

func TestBenchEpilogueNudgesLowReps(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bnudge
reps: 3
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.Contains(t, out.String(), "  note: reps=3 limits rate-gate sensitivity; prefer reps: 5 or more")
}

func TestBenchEpilogueNoNudgeAtFiveReps(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bnonudge
reps: 5
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.NotContains(t, out.String(), "limits rate-gate sensitivity")
}

func TestBenchBadBasketIsOperational(t *testing.T) {
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", filepath.Join(t.TempDir(), "missing.yaml")}, &out, &errBuf)
	assert.Equal(t, 2, code)
}

func TestBenchManifestAppendErrorIsOperational(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bapp
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "no-such-dir", "m.jsonl")

	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	assert.Equal(t, 2, code)
}

func TestBenchChildRunsInDir(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	workdir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(workdir)
	require.NoError(t, err)

	path := writeBasket(t, `basket: bdir
reps: 1
tasks:
  - id: t1
    cmd: ["CWD"]
    dir: `+workdir+`
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "CWD="+resolved)
}

func TestBenchChildSpawnFailureRecorded(t *testing.T) {
	orig := execCommand
	execCommand = func(_ string, _ ...string) *exec.Cmd {
		return exec.Command(filepath.Join(t.TempDir(), "nope-binary"))
	}
	t.Cleanup(func() { execCommand = orig })
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bspawn
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, -1, entries[0].ExitCode)
	assert.Contains(t, entries[0].Note, "spawn failed:")
	assert.False(t, entries[0].Marked)
	assert.Contains(t, errBuf.String(), "spawn failed:")
	assert.Contains(t, errBuf.String(), "bench-bspawn-t1-v1-r1")
}

func TestBenchNoDaemonIsOperational(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(t.TempDir(), "nope.json"))
	path := writeBasket(t, `basket: bnodaemon
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path}, &out, &errBuf)
	assert.Equal(t, 2, code)
	assert.Contains(t, errBuf.String(), "catacomb up")

	_, statErr := os.Stat(path + ".manifest.jsonl")
	assert.True(t, os.IsNotExist(statErr))
}

func TestBenchDaemonUnreachableIsOperational(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{Addr: addr, Token: "tok"}))
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bunreach
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path}, &out, &errBuf)
	assert.Equal(t, 2, code)
}

func TestBenchDryRunSkipsPreflight(t *testing.T) {
	t.Setenv("CATACOMB_DISCOVERY", filepath.Join(t.TempDir(), "nope.json"))
	path := writeBasket(t, `basket: bdrypre
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--dry-run"}, &out, &errBuf)
	assert.Equal(t, 0, code, errBuf.String())
	assert.Contains(t, out.String(), "bench-bdrypre-t1-v1-r1")
}

func TestBenchRerunWithoutResumeRefused(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: brerun
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	var out1, err1 bytes.Buffer
	require.Equal(t, 0, run([]string{"bench", path}, &out1, &err1), err1.String())

	var out2, err2 bytes.Buffer
	code2 := run([]string{"bench", path}, &out2, &err2)
	assert.Equal(t, 2, code2)
	assert.Contains(t, err2.String(), "manifest already has entries")

	var out3, err3 bytes.Buffer
	require.Equal(t, 0, run([]string{"bench", path, "--resume"}, &out3, &err3), err3.String())
	assert.Contains(t, out3.String(), "skip bench-brerun-t1-v1-r1")

	fresh := filepath.Join(t.TempDir(), "fresh.jsonl")
	var out4, err4 bytes.Buffer
	require.Equal(t, 0, run([]string{"bench", path, "--manifest", fresh}, &out4, &err4), err4.String())
	require.Len(t, readManifest(t, fresh), 1)
}

func TestBenchEpilogueTruncatesLongBaselineName(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	longVariant := strings.Repeat("v", 130)
	path := writeBasket(t, `basket: bt
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: `+longVariant+`
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	full := "bt-" + longVariant
	truncated := full[:128]
	assert.Contains(t, out.String(), "catacomb baseline set "+truncated+" --label basket=bt")
	assert.NotContains(t, out.String(), "catacomb baseline set "+full+" --label")
}

func TestBenchResumeHashMismatchDeterministic(t *testing.T) {
	fakeBenchExec(t)
	discovery, _ := benchServer(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeBasket(t, `basket: bdet
reps: 1
tasks:
  - id: t1
    cmd: ["CHILD"]
variants:
  - id: v1
`)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var buf bytes.Buffer
	for _, e := range []bench.ManifestEntry{
		{RunID: "aaa", Task: "t1", Variant: "v1", Rep: 1, BasketHash: "hashaaa"},
		{RunID: "zzz", Task: "t1", Variant: "v1", Rep: 1, BasketHash: "hashzzz"},
	} {
		raw, _ := json.Marshal(e)
		buf.Write(raw)
		buf.WriteByte('\n')
	}
	require.NoError(t, os.WriteFile(manifest, buf.Bytes(), 0o600))

	for i := 0; i < 3; i++ {
		var out, errBuf bytes.Buffer
		code := run([]string{"bench", path, "--manifest", manifest, "--resume"}, &out, &errBuf)
		assert.Equal(t, 2, code)
		assert.Contains(t, errBuf.String(), "manifest basket hash hashaaa")
	}
}

func TestBenchPreflightTimesOutOnBlockedDaemon(t *testing.T) {
	srv := blockingServer(t)
	origClient := statusHTTPClient
	statusHTTPClient = &http.Client{Timeout: 50 * time.Millisecond}
	t.Cleanup(func() { statusHTTPClient = origClient })

	discovery := filepath.Join(t.TempDir(), "d.json")
	require.NoError(t, daemon.WriteDiscovery(discovery, daemon.Discovery{
		Addr:  strings.TrimPrefix(srv.URL, "http://"),
		Token: "tok",
	}))
	_, err := benchPreflight(context.Background(), discovery)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unreachable")
}

func writeCheckpointBasket(t *testing.T, name, tasks string) string {
	t.Helper()
	return writeBasket(t, fmt.Sprintf("basket: %s\nreps: 1\n%svariants:\n  - id: v1\n", name, tasks))
}

func TestBenchCheckpointsAllPresent(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	graph.setMarkers("bench-cpall-t1-v1-r1", "plan", "tests.pass")

	path := writeCheckpointBasket(t, "cpall", "tasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n    checkpoints: [plan, tests.pass]\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Empty(t, entries[0].MissingCheckpoints)
	assert.NotContains(t, errBuf.String(), "missing checkpoints")
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 1/1")
	assert.Contains(t, out.String(), "checkpoints[t1]: tests.pass 1/1")
	assert.Equal(t, 1, graph.requests())
}

func TestBenchCheckpointsSomeMissing(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	graph.setMarkers("bench-cpmiss-t1-v1-r1", "plan")

	path := writeCheckpointBasket(t, "cpmiss", "tasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n    checkpoints: [plan, tests.pass, deploy]\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, []string{"tests.pass", "deploy"}, entries[0].MissingCheckpoints)
	assert.Contains(t, errBuf.String(), "cell bench-cpmiss-t1-v1-r1: missing checkpoints: tests.pass, deploy")
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 1/1")
	assert.Contains(t, out.String(), "checkpoints[t1]: tests.pass 0/1")
	assert.Contains(t, out.String(), "checkpoints[t1]: deploy 0/1")
}

func TestBenchCheckpointsFetchErrorSkips(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	graph.setFail(true)

	path := writeCheckpointBasket(t, "cperr", "tasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n    checkpoints: [plan]\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Empty(t, entries[0].MissingCheckpoints)
	assert.Equal(t, "checkpoint verification skipped: graph status 500", entries[0].Note)
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 0/0")
}

func TestBenchCheckpointsMarkerFailedAppendsSkip(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusInternalServerError)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	graph.setFail(true)

	path := writeCheckpointBasket(t, "cpboth", "tasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n    checkpoints: [plan]\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, "marker failed; checkpoint verification skipped: graph status 500", entries[0].Note)
	assert.Empty(t, entries[0].MissingCheckpoints)
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 0/0")
}

func TestBenchCheckpointsNoSessionSkips(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeCheckpointBasket(t, "cpnos", "tasks:\n  - id: t1\n    cmd: [\"NOSESSION\"]\n    checkpoints: [plan]\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 1)
	assert.Equal(t, "no session id observed", entries[0].Note)
	assert.Empty(t, entries[0].SessionID)
	assert.Empty(t, entries[0].MissingCheckpoints)
	assert.Equal(t, 0, graph.requests())
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 0/0")
}

func TestBenchCheckpointsMultipleTasks(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	graph.setMarkers("bench-cpmulti-t2-v1-r1", "alpha")
	graph.setMarkers("bench-cpmulti-t3-v1-r1", "beta")

	tasks := "tasks:\n" +
		"  - id: t1\n    cmd: [\"CHILD\"]\n" +
		"  - id: t2\n    cmd: [\"CHILD\"]\n    checkpoints: [alpha]\n" +
		"  - id: t3\n    cmd: [\"CHILD\"]\n    checkpoints: [beta, gamma]\n"
	path := writeCheckpointBasket(t, "cpmulti", tasks)
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	s := out.String()
	assert.NotContains(t, s, "checkpoints[t1]")
	iAlpha := strings.Index(s, "checkpoints[t2]: alpha 1/1")
	iBeta := strings.Index(s, "checkpoints[t3]: beta 1/1")
	iGamma := strings.Index(s, "checkpoints[t3]: gamma 0/1")
	require.GreaterOrEqual(t, iAlpha, 0)
	require.GreaterOrEqual(t, iBeta, 0)
	require.GreaterOrEqual(t, iGamma, 0)
	assert.Less(t, iAlpha, iBeta)
	assert.Less(t, iBeta, iGamma)
}

func TestBenchCheckpointsAccumulateAcrossReps(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)
	graph.setMarkers("bench-cpreps-t1-v1-r1", "plan")

	path := writeBasket(t, "basket: cpreps\nreps: 2\ntasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n    checkpoints: [plan]\nvariants:\n  - id: v1\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	entries := readManifest(t, manifest)
	require.Len(t, entries, 2)
	assert.Contains(t, out.String(), "checkpoints[t1]: plan 1/2")
	assert.Contains(t, errBuf.String(), "cell bench-cpreps-t1-v1-r2: missing checkpoints: plan")
}

func TestBenchNoCheckpointsNoGraphFetch(t *testing.T) {
	fakeBenchExec(t)
	discovery, _, graph := benchServerWithGraph(t, http.StatusOK)
	t.Setenv("CATACOMB_DISCOVERY", discovery)

	path := writeCheckpointBasket(t, "cpnone", "tasks:\n  - id: t1\n    cmd: [\"CHILD\"]\n")
	manifest := filepath.Join(t.TempDir(), "m.jsonl")
	var out, errBuf bytes.Buffer
	code := run([]string{"bench", path, "--manifest", manifest}, &out, &errBuf)
	require.Equal(t, 0, code, errBuf.String())

	assert.NotContains(t, out.String(), "checkpoints[")
	assert.Equal(t, 0, graph.requests())
}

func TestFetchSessionMarkersFiltersNonMarkers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `[
			{"kind":"run_started"},
			{"kind":"node_upsert"},
			{"kind":"node_upsert","node":{"type":"tool_call","name":"Bash"}},
			{"kind":"node_upsert","node":{"type":"marker","name":"plan"}},
			{"kind":"node_upsert","node":{"type":"marker","name":"task:t1"}}
		]`)
	}))
	t.Cleanup(srv.Close)
	disc := daemon.Discovery{Addr: strings.TrimPrefix(srv.URL, "http://"), Token: "tok"}

	got, err := fetchSessionMarkers(context.Background(), disc, "s1")
	require.NoError(t, err)
	_, hasPlan := got["plan"]
	_, hasTask := got["task:t1"]
	_, hasBash := got["Bash"]
	assert.True(t, hasPlan)
	assert.True(t, hasTask)
	assert.False(t, hasBash)
}

func TestFetchSessionMarkersBadAddr(t *testing.T) {
	_, err := fetchSessionMarkers(context.Background(), daemon.Discovery{Addr: "\x7f", Token: "t"}, "s1")
	require.Error(t, err)
}

func TestFetchSessionMarkersUnreachable(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())

	_, err = fetchSessionMarkers(context.Background(), daemon.Discovery{Addr: addr, Token: "t"}, "s1")
	require.Error(t, err)
}

func TestFetchSessionMarkersDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "not json")
	}))
	t.Cleanup(srv.Close)
	disc := daemon.Discovery{Addr: strings.TrimPrefix(srv.URL, "http://"), Token: "tok"}

	_, err := fetchSessionMarkers(context.Background(), disc, "s1")
	require.Error(t, err)
}

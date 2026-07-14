package bench_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/bench"
)

func TestLoadHappyPath(t *testing.T) {
	b, hash, err := bench.Load("testdata/basket.yaml")
	require.NoError(t, err)

	assert.Equal(t, "checkout", b.Name)
	assert.Equal(t, 2, b.Reps)
	require.Len(t, b.Tasks, 2)
	require.Len(t, b.Variants, 2)

	assert.Equal(t, "add-item", b.Tasks[0].ID)
	assert.Equal(t, []string{"make", "add"}, b.Tasks[0].Cmd)
	assert.Equal(t, filepath.Join("testdata", "services/cart"), b.Tasks[0].Dir)
	assert.Equal(t, map[string]string{"MODE": "fast"}, b.Tasks[0].Env)
	assert.Equal(t, "remove-item", b.Tasks[1].ID)
	assert.Empty(t, b.Tasks[1].Dir)

	assert.Equal(t, "baseline", b.Variants[0].ID)
	assert.Equal(t, map[string]string{"MODEL": "opus"}, b.Variants[0].Env)
	assert.Equal(t, []string{"echo setup-baseline"}, b.Variants[0].Setup)
	assert.Equal(t, []string{"echo setup-candidate", "echo warmup"}, b.Variants[1].Setup)

	assert.Len(t, hash, 64)
}

func TestLoadHashIsSha256OfRawBytes(t *testing.T) {
	data, err := os.ReadFile("testdata/basket.yaml")
	require.NoError(t, err)
	sum := sha256.Sum256(data)
	want := hex.EncodeToString(sum[:])

	_, hash, err := bench.Load("testdata/basket.yaml")
	require.NoError(t, err)
	assert.Equal(t, want, hash)
}

func TestLoadHashStability(t *testing.T) {
	_, h1, err := bench.Load("testdata/basket.yaml")
	require.NoError(t, err)
	_, h2, err := bench.Load("testdata/basket.yaml")
	require.NoError(t, err)
	assert.Equal(t, h1, h2)

	data, err := os.ReadFile("testdata/basket.yaml")
	require.NoError(t, err)
	changed := filepath.Join(t.TempDir(), "basket.yaml")
	require.NoError(t, os.WriteFile(changed, append(data, ' '), 0o600))
	_, h3, err := bench.Load(changed)
	require.NoError(t, err)
	assert.NotEqual(t, h1, h3)
}

func TestLoadReadError(t *testing.T) {
	_, _, err := bench.Load(t.TempDir())
	require.Error(t, err)
}

func TestLoadDecodeError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.yaml")
	require.NoError(t, os.WriteFile(path, []byte("basket: [unterminated\n"), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
}

func TestLoadUnknownFieldError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.yaml")
	body := "basket: c\nreps: 1\nbogus: x\ntasks:\n  - id: t\n    cmd: [\"echo\"]\nvariants:\n  - id: v\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
}

func TestTaskTimeoutDuration(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    time.Duration
		wantErr bool
	}{
		{"empty is unset", "", 0, false},
		{"thirty seconds", "30s", 30 * time.Second, false},
		{"negative", "-1s", 0, true},
		{"garbage", "banana", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := bench.Task{Timeout: tt.timeout}.TimeoutDuration()
			if tt.wantErr {
				require.ErrorIs(t, err, bench.ErrTimeout)
				assert.Zero(t, d)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d)
		})
	}
}

func TestLoadTimeoutRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "timeout.yaml")
	body := "basket: c\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\n    timeout: 30s\nvariants:\n  - id: v\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	b, _, err := bench.Load(path)
	require.NoError(t, err)
	require.Len(t, b.Tasks, 1)
	assert.Equal(t, "30s", b.Tasks[0].Timeout)
	d, err := b.Tasks[0].TimeoutDuration()
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, d)
}

func TestLoadRejectsInvalidTimeout(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad-timeout.yaml")
	body := "basket: c\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\n    timeout: banana\nvariants:\n  - id: v\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, bench.ErrTimeout)
	assert.Contains(t, err.Error(), "timeout")
}

func TestLoadValidationError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty-name.yaml")
	body := "basket: \"\"\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\nvariants:\n  - id: v\n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, bench.ErrEmptyBasketName)
}

func TestCellsExpansionOrderAndDerivation(t *testing.T) {
	b, _, err := bench.Load("testdata/basket.yaml")
	require.NoError(t, err)

	cells := b.Cells()
	require.Len(t, cells, 8)

	wantRunIDs := []string{
		"bench-checkout-add-item-baseline-r1",
		"bench-checkout-add-item-baseline-r2",
		"bench-checkout-add-item-candidate-r1",
		"bench-checkout-add-item-candidate-r2",
		"bench-checkout-remove-item-baseline-r1",
		"bench-checkout-remove-item-baseline-r2",
		"bench-checkout-remove-item-candidate-r1",
		"bench-checkout-remove-item-candidate-r2",
	}
	for i, c := range cells {
		assert.Equal(t, wantRunIDs[i], c.RunID)
	}

	first := cells[0]
	assert.Equal(t, "add-item", first.Task.ID)
	assert.Equal(t, "baseline", first.Variant.ID)
	assert.Equal(t, 1, first.Rep)
	assert.Equal(t, map[string]string{
		"basket": "checkout", "task": "add-item", "variant": "baseline", "rep": "1",
	}, first.Labels)

	last := cells[7]
	assert.Equal(t, "bench-checkout-remove-item-candidate-r2", last.RunID)
	assert.Equal(t, "remove-item", last.Task.ID)
	assert.Equal(t, "candidate", last.Variant.ID)
	assert.Equal(t, 2, last.Rep)
	assert.Equal(t, map[string]string{
		"basket": "checkout", "task": "remove-item", "variant": "candidate", "rep": "2",
	}, last.Labels)
}

func TestCellsEmptyWhenNoReps(t *testing.T) {
	b := bench.Basket{Name: "n", Reps: 0, Tasks: []bench.Task{{ID: "t"}}, Variants: []bench.Variant{{ID: "v"}}}
	assert.Empty(t, b.Cells())
}

func TestCellsNilWhenNegativeReps(t *testing.T) {
	b := bench.Basket{Name: "n", Reps: -1, Tasks: []bench.Task{{ID: "t"}}, Variants: []bench.Variant{{ID: "v"}}}
	assert.Nil(t, b.Cells())
}

func TestLoadRunIDCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "collide.yaml")
	body := `basket: c
reps: 1
tasks:
  - id: a-b
    cmd: ["echo"]
  - id: a
    cmd: ["echo"]
variants:
  - id: c
  - id: b-c
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.ErrorIs(t, err, bench.ErrRunIDCollision)
	assert.Contains(t, err.Error(), `run-id collision: task "a-b"/variant "c" and task "a"/variant "b-c"`)
}

func TestLoadCheckpointsRoundTrip(t *testing.T) {
	b, _, err := bench.Load("testdata/checkpoints.yaml")
	require.NoError(t, err)

	require.Len(t, b.Tasks, 2)
	assert.Equal(t, "build", b.Tasks[0].ID)
	assert.Equal(t, []string{"compiled", "task:link", "tests.pass"}, b.Tasks[0].Checkpoints)
	assert.Equal(t, "deploy", b.Tasks[1].ID)
	assert.Empty(t, b.Tasks[1].Checkpoints)
}

func TestLoadCheckpointValidation(t *testing.T) {
	const template = "basket: cp\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\n    checkpoints: %s\nvariants:\n  - id: v\n"
	long := strings.Repeat("a", 257)

	tests := []struct {
		name        string
		checkpoints string
		wantErr     bool
	}{
		{"empty name", `[""]`, true},
		{"too long", fmt.Sprintf("[%q]", long), true},
		{"space", `["a b"]`, true},
		{"comma", `["a,b"]`, true},
		{"duplicate within task", `["dup", "dup"]`, true},
		{"reserved marker collision", `["task:t"]`, true},
		{"colon and full charset accepted", `["task:other", "a.b_c-d"]`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "cp.yaml")
			body := fmt.Sprintf(template, tc.checkpoints)
			require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

			_, _, err := bench.Load(path)
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, bench.ErrCheckpoint)
				assert.Contains(t, err.Error(), "checkpoints[")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadVerifyRoundTrip(t *testing.T) {
	b, _, err := bench.Load("testdata/verify.yaml")
	require.NoError(t, err)

	require.Len(t, b.Tasks, 2)
	require.NotNil(t, b.Tasks[0].Verify)
	assert.Equal(t, []string{"pytest", "-q"}, b.Tasks[0].Verify.Cmd)
	assert.Equal(t, map[string]string{"CI": "1"}, b.Tasks[0].Verify.Env)
	assert.Equal(t, "45s", b.Tasks[0].Verify.Timeout)
	assert.Equal(t, []string{"dist/**/*.js", "*.log"}, b.Tasks[0].Artifacts)

	assert.Nil(t, b.Tasks[1].Verify)
	assert.Empty(t, b.Tasks[1].Artifacts)
}

func TestVerifyTimeoutDuration(t *testing.T) {
	tests := []struct {
		name    string
		timeout string
		want    time.Duration
		wantErr bool
	}{
		{"empty defaults to one minute", "", time.Minute, false},
		{"forty five seconds", "45s", 45 * time.Second, false},
		{"negative", "-1s", 0, true},
		{"garbage", "banana", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := bench.Verify{Cmd: []string{"x"}, Timeout: tt.timeout}.TimeoutDuration()
			if tt.wantErr {
				require.ErrorIs(t, err, bench.ErrTimeout)
				assert.Zero(t, d)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, d)
		})
	}
}

func TestLoadVerifyValidation(t *testing.T) {
	const template = "basket: vv\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\n    verify: %s\nvariants:\n  - id: v\n"

	tests := []struct {
		name    string
		verify  string
		wantErr error
		needle  string
	}{
		{"empty cmd", `{cmd: []}`, bench.ErrVerifyCmd, "verify.cmd"},
		{"bad timeout", `{cmd: ["x"], timeout: "nope"}`, bench.ErrTimeout, "verify.timeout"},
		{"valid", `{cmd: ["x"], timeout: "10s"}`, nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "verify.yaml")
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(template, tc.verify)), 0o600))

			_, _, err := bench.Load(path)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.wantErr)
			assert.Contains(t, err.Error(), tc.needle)
		})
	}
}

func TestLoadArtifactValidation(t *testing.T) {
	const template = "basket: aa\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\n    artifacts: %s\nvariants:\n  - id: v\n"

	tests := []struct {
		name      string
		artifacts string
		wantErr   bool
	}{
		{"empty entry", `[""]`, true},
		{"parent escape", `["../x"]`, true},
		{"absolute", `["/etc/passwd"]`, true},
		{"deep parent escape", `["a/../../b"]`, true},
		{"rooted and bare globs accepted", `["dist/**/*.js", "*.log", "logs/out.txt"]`, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "artifacts.yaml")
			require.NoError(t, os.WriteFile(path, []byte(fmt.Sprintf(template, tc.artifacts)), 0o600))

			_, _, err := bench.Load(path)
			if tc.wantErr {
				require.Error(t, err)
				assert.ErrorIs(t, err, bench.ErrArtifactGlob)
				assert.Contains(t, err.Error(), "artifacts[")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestLoadVerifyBlockChangesHash(t *testing.T) {
	const withoutVerify = "basket: h\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\nvariants:\n  - id: v\n"
	const withVerify = "basket: h\nreps: 1\ntasks:\n  - id: t\n    cmd: [\"echo\"]\n    verify: {cmd: [\"x\"]}\nvariants:\n  - id: v\n"

	dir := t.TempDir()
	p1 := filepath.Join(dir, "a.yaml")
	p2 := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(p1, []byte(withoutVerify), 0o600))
	require.NoError(t, os.WriteFile(p2, []byte(withVerify), 0o600))

	b1, h1, err := bench.Load(p1)
	require.NoError(t, err)
	assert.Nil(t, b1.Tasks[0].Verify)

	b2, h2, err := bench.Load(p2)
	require.NoError(t, err)
	require.NotNil(t, b2.Tasks[0].Verify)
	assert.Equal(t, []string{"x"}, b2.Tasks[0].Verify.Cmd)

	assert.NotEqual(t, h1, h2)
}

func TestLoadWorkspaceValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want error
	}{
		{"empty workspace cmd on task", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: []}
variants:
  - id: v1
`, bench.ErrWorkspaceCmd},
		{"empty workspace cmd on variant", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
variants:
  - id: v1
    workspace: {cmd: []}
`, bench.ErrWorkspaceCmd},
		{"task dir with task workspace", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    dir: /tmp
    workspace: {cmd: ["true"]}
variants:
  - id: v1
`, bench.ErrWorkspaceDir},
		{"task dir with variant workspace", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    dir: /tmp
variants:
  - id: v1
    workspace: {cmd: ["true"]}
`, bench.ErrWorkspaceDir},
		{"missing patch", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: nope.patch}
variants:
  - id: v1
`, bench.ErrWorkspacePatch},
		{"missing patch on variant", `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
variants:
  - id: v1
    workspace: {cmd: ["true"], patch: nope.patch}
`, bench.ErrWorkspacePatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "basket.yaml")
			require.NoError(t, os.WriteFile(path, []byte(tc.yaml), 0o600))
			_, _, err := bench.Load(path)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestLoadWorkspacePatchHashedAndResolved(t *testing.T) {
	dir := t.TempDir()
	content := []byte("patch-bytes\n")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.patch"), content, 0o600))
	yaml := `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: fix.patch, rev: r42}
variants:
  - id: v1
  - id: v2
    workspace: {cmd: ["sh", "x.sh"], patch: fix.patch}
`
	path := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	b, _, err := bench.Load(path)
	require.NoError(t, err)
	sum := sha256.Sum256(content)
	want := hex.EncodeToString(sum[:])
	tw := b.Tasks[0].Workspace
	require.Equal(t, filepath.Join(dir, "fix.patch"), tw.PatchAbs)
	require.Equal(t, want, tw.PatchSHA256)
	require.Equal(t, "r42", tw.Rev)
	vw := b.Variants[1].Workspace
	require.Equal(t, want, vw.PatchSHA256)
}

func TestLoadOfflineSkipsPatchResolution(t *testing.T) {
	dir := t.TempDir()
	yaml := `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: nope.patch}
variants:
  - id: v1
  - id: v2
    workspace: {cmd: ["true"], patch: nope.patch}
`
	path := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	_, _, err := bench.Load(path)
	require.ErrorIs(t, err, bench.ErrWorkspacePatch)

	b, hash, err := bench.LoadOffline(path)
	require.NoError(t, err)
	assert.NotEmpty(t, hash)
	assert.Equal(t, "nope.patch", b.Tasks[0].Workspace.Patch)
	assert.Empty(t, b.Tasks[0].Workspace.PatchAbs)
	assert.Empty(t, b.Tasks[0].Workspace.PatchSHA256)
	assert.Empty(t, b.Variants[1].Workspace.PatchAbs)
	assert.Empty(t, b.Variants[1].Workspace.PatchSHA256)
}

func TestLoadOfflineHashMatchesLoad(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.patch"), []byte("patch-bytes\n"), 0o600))
	yaml := `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: fix.patch}
variants:
  - id: v1
`
	path := filepath.Join(dir, "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	_, wantHash, err := bench.Load(path)
	require.NoError(t, err)
	b, gotHash, err := bench.LoadOffline(path)
	require.NoError(t, err)
	assert.Equal(t, wantHash, gotHash)
	assert.Empty(t, b.Tasks[0].Workspace.PatchAbs)
	assert.Empty(t, b.Tasks[0].Workspace.PatchSHA256)
}

func TestLoadOfflineStillValidates(t *testing.T) {
	yaml := `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    dir: /tmp
    workspace: {cmd: ["true"]}
variants:
  - id: v1
`
	path := filepath.Join(t.TempDir(), "basket.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	_, _, err := bench.LoadOffline(path)
	require.ErrorIs(t, err, bench.ErrWorkspaceDir)
}

func TestLoadWorkspacePatchAbsIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "fix.patch"), []byte("patch-bytes\n"), 0o600))

	t.Run("relative basket path", func(t *testing.T) {
		yaml := `
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: fix.patch}
variants:
  - id: v1
`
		path := filepath.Join(dir, "basket.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
		cwd, err := os.Getwd()
		require.NoError(t, err)
		rel, err := filepath.Rel(cwd, path)
		if err != nil {
			t.Skipf("basket path %q not relative to cwd %q: %v", path, cwd, err)
		}
		b, _, err := bench.Load(rel)
		require.NoError(t, err)
		got := b.Tasks[0].Workspace.PatchAbs
		require.True(t, filepath.IsAbs(got))
		require.Equal(t, filepath.Join(dir, "fix.patch"), got)
	})

	t.Run("absolute patch passthrough", func(t *testing.T) {
		abs := filepath.Join(dir, "fix.patch")
		yaml := fmt.Sprintf(`
basket: b
reps: 1
tasks:
  - id: t1
    cmd: ["echo"]
    workspace: {cmd: ["true"], patch: %q}
variants:
  - id: v1
`, abs)
		path := filepath.Join(dir, "abs-basket.yaml")
		require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
		b, _, err := bench.Load(path)
		require.NoError(t, err)
		require.Equal(t, abs, b.Tasks[0].Workspace.PatchAbs)
	})
}

func TestEffectiveWorkspace(t *testing.T) {
	taskWS := &bench.Workspace{Cmd: []string{"t"}}
	varWS := &bench.Workspace{Cmd: []string{"v"}}
	cases := []struct {
		name string
		cell bench.Cell
		want *bench.Workspace
	}{
		{"variant wins", bench.Cell{Task: bench.Task{Workspace: taskWS}, Variant: bench.Variant{Workspace: varWS}}, varWS},
		{"task fallback", bench.Cell{Task: bench.Task{Workspace: taskWS}}, taskWS},
		{"none", bench.Cell{}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.want == nil {
				require.Nil(t, tc.cell.EffectiveWorkspace())
				return
			}
			require.Same(t, tc.want, tc.cell.EffectiveWorkspace())
		})
	}
}

func TestLoadDashIDsNoCollision(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dashy.yaml")
	body := `basket: multi-word-basket
reps: 1
tasks:
  - id: add-item
    cmd: ["echo"]
  - id: remove-item
    cmd: ["echo"]
variants:
  - id: base-line
  - id: cand-idate
`
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	b, _, err := bench.Load(path)
	require.NoError(t, err)
	assert.Len(t, b.Cells(), 4)
}

func TestLoadTypeErrorIsHuman(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
basket: b
reps: 1
tasks:
  - id: t1
    cmd: "echo hi"
variants:
  - id: v1
`), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected a list of strings")
	assert.NotContains(t, err.Error(), "!!str")
}

func TestLoadValidationErrorNotDoubled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b.yaml")
	require.NoError(t, os.WriteFile(path, []byte(`
basket: b
reps: 0
tasks:
  - id: t1
    cmd: ["echo"]
variants:
  - id: v1
`), 0o600))
	_, _, err := bench.Load(path)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "bench: ")
	assert.ErrorIs(t, err, bench.ErrReps)
}

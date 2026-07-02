package bench_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	assert.Equal(t, "services/cart", b.Tasks[0].Dir)
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

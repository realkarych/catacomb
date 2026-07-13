package bench

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validBasket() Basket {
	return Basket{
		Name:     "checkout",
		Reps:     1,
		Tasks:    []Task{{ID: "t1", Cmd: []string{"echo"}}},
		Variants: []Variant{{ID: "v1"}},
	}
}

func TestValidateHappy(t *testing.T) {
	require.NoError(t, validate(validBasket()))
}

func TestResolvePatchAbsError(t *testing.T) {
	orig := absFn
	absFn = func(string) (string, error) { return "", errors.New("boom") }
	t.Cleanup(func() { absFn = orig })
	err := resolvePatch(&Workspace{Patch: "fix.patch"}, "base")
	require.ErrorIs(t, err, ErrWorkspacePatch)
	assert.ErrorContains(t, err, "boom")
}

func TestValidateErrors(t *testing.T) {
	long := strings.Repeat("a", 257)
	tests := []struct {
		name    string
		mutate  func(*Basket)
		wantErr error
		field   string
	}{
		{"empty name", func(b *Basket) { b.Name = "" }, ErrEmptyBasketName, "basket"},
		{"name too long", func(b *Basket) { b.Name = long }, ErrBasketNameLen, "basket"},
		{"name comma", func(b *Basket) { b.Name = "a,b" }, ErrCharset, "basket"},
		{"name space", func(b *Basket) { b.Name = "a b" }, ErrCharset, "basket"},
		{"name equals", func(b *Basket) { b.Name = "a=b" }, ErrCharset, "basket"},
		{"reps zero", func(b *Basket) { b.Reps = 0 }, ErrReps, "reps"},
		{"reps negative", func(b *Basket) { b.Reps = -3 }, ErrReps, "reps"},
		{"no tasks", func(b *Basket) { b.Tasks = nil }, ErrNoTasks, "tasks"},
		{"no variants", func(b *Basket) { b.Variants = nil }, ErrNoVariants, "variants"},
		{"empty task id", func(b *Basket) { b.Tasks[0].ID = "" }, ErrEmptyID, "task"},
		{"task id too long", func(b *Basket) { b.Tasks[0].ID = long }, ErrIDLen, "task"},
		{"task id comma", func(b *Basket) { b.Tasks[0].ID = "a,b" }, ErrCharset, "task"},
		{"task id space", func(b *Basket) { b.Tasks[0].ID = "a b" }, ErrCharset, "task"},
		{"task id equals", func(b *Basket) { b.Tasks[0].ID = "a=b" }, ErrCharset, "task"},
		{
			"duplicate task id",
			func(b *Basket) {
				b.Tasks = []Task{{ID: "dup", Cmd: []string{"a"}}, {ID: "dup", Cmd: []string{"b"}}}
			},
			ErrDuplicateID, "task",
		},
		{"empty cmd", func(b *Basket) { b.Tasks[0].Cmd = nil }, ErrEmptyCmd, "task"},
		{"negative timeout", func(b *Basket) { b.Tasks[0].Timeout = "-1s" }, ErrTimeout, "task"},
		{"garbage timeout", func(b *Basket) { b.Tasks[0].Timeout = "banana" }, ErrTimeout, "task"},
		{"empty variant id", func(b *Basket) { b.Variants[0].ID = "" }, ErrEmptyID, "variant"},
		{"variant id too long", func(b *Basket) { b.Variants[0].ID = long }, ErrIDLen, "variant"},
		{"variant id comma", func(b *Basket) { b.Variants[0].ID = "x,y" }, ErrCharset, "variant"},
		{"variant id equals", func(b *Basket) { b.Variants[0].ID = "x=y" }, ErrCharset, "variant"},
		{
			"duplicate variant id",
			func(b *Basket) { b.Variants = []Variant{{ID: "dup"}, {ID: "dup"}} },
			ErrDuplicateID, "variant",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := validBasket()
			tt.mutate(&b)
			err := validate(b)
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)
			assert.Contains(t, err.Error(), tt.field)
		})
	}
}

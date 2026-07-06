package storetest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func RunStoreContract(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()
	t.Run("baselines crud sorted overwrite", func(t *testing.T) {
		s := newStore(t)
		empty, err := s.ListBaselines()
		require.NoError(t, err)
		assert.Empty(t, empty)
		_, ok, err := s.GetBaseline("missing")
		require.NoError(t, err)
		assert.False(t, ok)
		require.NoError(t, s.DeleteBaseline("missing"))

		created := time.Unix(100, 0).UTC()
		require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "b", RunIDs: []string{"r2"}, CreatedAt: created}))
		require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "a", RunIDs: []string{"r1"}, Selector: map[string]string{"suite": "x"}, CreatedAt: created}))
		require.NoError(t, s.UpsertBaseline(model.Baseline{Name: "b", RunIDs: []string{"r2", "r3"}, CreatedAt: created}))

		got, ok, err := s.GetBaseline("b")
		require.NoError(t, err)
		require.True(t, ok)
		assert.Equal(t, []string{"r2", "r3"}, got.RunIDs)

		list, err := s.ListBaselines()
		require.NoError(t, err)
		require.Len(t, list, 2)
		assert.Equal(t, "a", list[0].Name)
		assert.Equal(t, map[string]string{"suite": "x"}, list[0].Selector)
		assert.Equal(t, created, list[0].CreatedAt)
		assert.Equal(t, "b", list[1].Name)

		require.NoError(t, s.DeleteBaseline("a"))
		_, ok, err = s.GetBaseline("a")
		require.NoError(t, err)
		assert.False(t, ok)
		after, err := s.ListBaselines()
		require.NoError(t, err)
		require.Len(t, after, 1)
		assert.Equal(t, "b", after[0].Name)
	})

	t.Run("regress results per baseline seq ascending", func(t *testing.T) {
		s := newStore(t)
		empty, err := s.RegressResultsFor("unknown")
		require.NoError(t, err)
		assert.Empty(t, empty)

		seq, err := s.AppendRegressResult("alpha", json.RawMessage(`{"n":1}`))
		require.NoError(t, err)
		assert.Equal(t, 1, seq)
		seq, err = s.AppendRegressResult("beta", json.RawMessage(`{"n":10}`))
		require.NoError(t, err)
		assert.Equal(t, 1, seq)
		seq, err = s.AppendRegressResult("alpha", json.RawMessage(`{"n":2}`))
		require.NoError(t, err)
		assert.Equal(t, 2, seq)
		seq, err = s.AppendRegressResult("beta", json.RawMessage(`{"n":20}`))
		require.NoError(t, err)
		assert.Equal(t, 2, seq)
		seq, err = s.AppendRegressResult("alpha", json.RawMessage(`{"n":3}`))
		require.NoError(t, err)
		assert.Equal(t, 3, seq)

		alpha, err := s.RegressResultsFor("alpha")
		require.NoError(t, err)
		require.Len(t, alpha, 3)
		assert.Equal(t, "alpha", alpha[0].Baseline)
		assert.Equal(t, 1, alpha[0].Seq)
		assert.Equal(t, 2, alpha[1].Seq)
		assert.Equal(t, 3, alpha[2].Seq)
		assert.JSONEq(t, `{"n":1}`, string(alpha[0].Body))
		assert.JSONEq(t, `{"n":3}`, string(alpha[2].Body))

		beta, err := s.RegressResultsFor("beta")
		require.NoError(t, err)
		require.Len(t, beta, 2)
		assert.Equal(t, "beta", beta[0].Baseline)
		assert.Equal(t, 1, beta[0].Seq)
		assert.Equal(t, 2, beta[1].Seq)
		assert.JSONEq(t, `{"n":10}`, string(beta[0].Body))
		assert.JSONEq(t, `{"n":20}`, string(beta[1].Body))
	})

	t.Run("close", func(t *testing.T) {
		require.NoError(t, newStore(t).Close())
	})
}

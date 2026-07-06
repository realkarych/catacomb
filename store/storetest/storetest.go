package storetest

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

func RunStoreContract(t *testing.T, newStore func(t *testing.T) store.Store) {
	t.Helper()
	t.Run("observations seq and exec filters", func(t *testing.T) {
		s := newStore(t)
		max0, err := s.MaxSeq()
		require.NoError(t, err)
		assert.Equal(t, uint64(0), max0)
		require.NoError(t, s.Persist([]model.Observation{
			{ObsID: "o1", RunID: "r1", ExecutionID: "e1", Seq: 1, Kind: "a"},
			{ObsID: "o3", RunID: "r1", ExecutionID: "e1", Seq: 3, Kind: "c"},
		}, nil, nil))
		require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "o2", RunID: "r1", ExecutionID: "e2", Seq: 2, Kind: "b"}, nil))
		maxN, err := s.MaxSeq()
		require.NoError(t, err)
		assert.Equal(t, uint64(3), maxN)
		since, err := s.ObservationsSince(1)
		require.NoError(t, err)
		require.Len(t, since, 2)
		assert.Equal(t, uint64(2), since[0].Seq)
		assert.Equal(t, uint64(3), since[1].Seq)
		exec, err := s.ObservationsForExecution("e1")
		require.NoError(t, err)
		require.Len(t, exec, 2)
		assert.Equal(t, uint64(1), exec[0].Seq)
		assert.Equal(t, uint64(3), exec[1].Seq)
	})

	t.Run("delta kinds", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "d1", RunID: "r1", ExecutionID: "e1", Seq: 1}, []reduce.GraphDelta{
			{Kind: reduce.DeltaNodeUpsert, Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSession}},
			{Kind: reduce.DeltaNodeStatus, Node: &model.Node{ID: "n1", RunID: "r1", Type: model.NodeSession, Status: model.StatusOK}},
			{Kind: reduce.DeltaEdgeUpsert, Edge: &model.Edge{ID: "x1", RunID: "r1", Src: "n1", Dst: "n2"}},
			{Kind: reduce.DeltaEdgeDelete, Edge: &model.Edge{ID: "x1"}},
			{Kind: reduce.DeltaNodeMerge, OldID: "n1", NewID: "n2", Node: &model.Node{ID: "n2", RunID: "r1", Type: model.NodeSession}},
		}))
		require.NoError(t, s.AppendDeltas(model.Observation{ObsID: "d2", RunID: "r1", ExecutionID: "e1", Seq: 2}, []reduce.GraphDelta{
			{Kind: reduce.DeltaNodeUpsert},
			{Kind: reduce.DeltaNodeMerge},
			{Kind: reduce.DeltaEdgeUpsert},
			{Kind: reduce.DeltaEdgeDelete},
			{Kind: reduce.DeltaNodeMerge, Node: &model.Node{ID: "n3", RunID: "r1", Type: model.NodeToolCall}},
			{Kind: reduce.DeltaRunStarted, RunID: "r1"},
		}))
		obs, err := s.ObservationsSince(0)
		require.NoError(t, err)
		assert.Len(t, obs, 2)
	})

	t.Run("runs upsert filter order", func(t *testing.T) {
		s := newStore(t)
		empty, err := s.Runs()
		require.NoError(t, err)
		assert.Empty(t, empty)
		require.NoError(t, s.UpsertRun(model.Run{ID: "b", Status: model.StatusRunning, LastSeq: 1}))
		require.NoError(t, s.UpsertRun(model.Run{ID: "a", Status: model.StatusOK}))
		require.NoError(t, s.UpsertRun(model.Run{ID: "b", Status: model.StatusOK, LastSeq: 9}))
		all, err := s.Runs()
		require.NoError(t, err)
		require.Len(t, all, 2)
		assert.Equal(t, "a", all[0].ID)
		assert.Equal(t, model.StatusOK, all[1].Status)
		assert.Equal(t, uint64(9), all[1].LastSeq)
		require.NoError(t, s.UpsertRun(model.Run{ID: "c", Status: model.StatusRunning}))
		open, err := s.ListOpenRuns()
		require.NoError(t, err)
		require.Len(t, open, 1)
		assert.Equal(t, "c", open[0].ID)
	})

	t.Run("quarantine counter", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.Quarantine(model.QuarantineRecord{HookType: "PreToolUse"}))
		require.NoError(t, s.Quarantine(model.QuarantineRecord{HookType: "Stop"}))
		n, err := s.QuarantineCount()
		require.NoError(t, err)
		assert.Equal(t, int64(2), n)
	})

	t.Run("tail cursors upsert order", func(t *testing.T) {
		s := newStore(t)
		none, err := s.LoadTailCursors()
		require.NoError(t, err)
		assert.Empty(t, none)
		require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/b", Offset: 1}))
		require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/a", Offset: 2}))
		require.NoError(t, s.UpsertTailCursor(model.TailCursor{Path: "/a", Offset: 5, Fingerprint: "f"}))
		got, err := s.LoadTailCursors()
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "/a", got[0].Path)
		assert.Equal(t, int64(5), got[0].Offset)
		assert.Equal(t, "f", got[0].Fingerprint)
		assert.Equal(t, "/b", got[1].Path)
	})

	t.Run("annotations lww order step move", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", StepKey: "st", Owner: "eval", Key: "score", Value: json.RawMessage(`5`), WriteSeq: 5}))
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", Owner: "eval", Key: "score", Value: json.RawMessage(`9`), WriteSeq: 7}))
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", Owner: "eval", Key: "score", Value: json.RawMessage(`1`), WriteSeq: 4}))
		require.NoError(t, s.UpsertAnnotation(model.Annotation{ExecutionID: "e1", SourceKey: "k", Owner: "aaa", Key: "note", Value: json.RawMessage(`2`), WriteSeq: 2}))
		anns, err := s.AnnotationsForExecution("e1")
		require.NoError(t, err)
		require.Len(t, anns, 2)
		assert.Equal(t, "aaa", anns[0].Owner)
		assert.Equal(t, "eval", anns[1].Owner)
		assert.Equal(t, "st", anns[1].StepKey)
		assert.Equal(t, "9", string(anns[1].Value))
		require.NoError(t, s.MoveAnnotations("e1", "k", "k2"))
		moved, err := s.AnnotationsForExecution("e1")
		require.NoError(t, err)
		require.Len(t, moved, 2)
		for _, a := range moved {
			assert.Equal(t, "k2", a.SourceKey)
		}
	})

	t.Run("persist nodes and edges", func(t *testing.T) {
		s := newStore(t)
		require.NoError(t, s.Persist(
			[]model.Observation{{ObsID: "p1", RunID: "r1", ExecutionID: "e1", Seq: 1}},
			[]*model.Node{{ID: "n1", RunID: "r1", Type: model.NodeSession}},
			[]*model.Edge{{ID: "x1", RunID: "r1", Src: "n1", Dst: "n2"}},
		))
		obs, err := s.ObservationsSince(0)
		require.NoError(t, err)
		require.Len(t, obs, 1)
	})

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

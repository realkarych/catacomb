package store

import (
	"encoding/json"

	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
)

type Store interface {
	Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error
	AppendDeltas(o model.Observation, deltas []reduce.GraphDelta) error
	MaxSeq() (uint64, error)
	ObservationsSince(seq uint64) ([]model.Observation, error)
	ObservationsForExecution(executionID string) ([]model.Observation, error)
	UpsertRun(r model.Run) error
	ListOpenRuns() ([]model.Run, error)
	Runs() ([]model.Run, error)
	Quarantine(rec model.QuarantineRecord) error
	QuarantineCount() (int64, error)
	UpsertTailCursor(c model.TailCursor) error
	LoadTailCursors() ([]model.TailCursor, error)
	UpsertAnnotation(a model.Annotation) error
	AnnotationsForExecution(executionID string) ([]model.Annotation, error)
	MoveAnnotations(executionID, fromKey, toKey string) error
	UpsertBaseline(b model.Baseline) error
	GetBaseline(name string) (model.Baseline, bool, error)
	ListBaselines() ([]model.Baseline, error)
	DeleteBaseline(name string) error
	AppendRegressResult(baseline string, body json.RawMessage) (int, error)
	RegressResultsFor(baseline string) ([]model.RegressResult, error)
	Close() error
}

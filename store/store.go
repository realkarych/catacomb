package store

import "github.com/realkarych/catacomb/model"

type Store interface {
	Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error
	AppendAndApply(obs model.Observation, nodes []*model.Node, edges []*model.Edge) error
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
	Close() error
}

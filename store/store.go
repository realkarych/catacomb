package store

import "github.com/realkarych/catacomb/model"

type Store interface {
	Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error
	AppendAndApply(obs model.Observation, nodes []*model.Node, edges []*model.Edge) error
	MaxSeq() (uint64, error)
	ObservationsSince(seq uint64) ([]model.Observation, error)
	Close() error
}

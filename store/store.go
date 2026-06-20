package store

import "github.com/realkarych/catacomb/model"

type Store interface {
	Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error
	Close() error
}

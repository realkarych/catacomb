package store

import (
	"encoding/json"

	"github.com/realkarych/catacomb/model"
)

type Store interface {
	UpsertBaseline(b model.Baseline) error
	GetBaseline(name string) (model.Baseline, bool, error)
	ListBaselines() ([]model.Baseline, error)
	DeleteBaseline(name string) error
	AppendRegressResult(baseline string, body json.RawMessage) (int, error)
	RegressResultsFor(baseline string) ([]model.RegressResult, error)
	Close() error
}

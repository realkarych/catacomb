package memory

import (
	"cmp"
	"encoding/json"
	"slices"
	"sync"

	"github.com/realkarych/catacomb/model"
)

type Store struct {
	mu        sync.Mutex
	baselines map[string]model.Baseline
	regress   map[string][]model.RegressResult
}

func New() *Store {
	return &Store{
		baselines: map[string]model.Baseline{},
		regress:   map[string][]model.RegressResult{},
	}
}

func (s *Store) UpsertBaseline(b model.Baseline) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.baselines[b.Name] = b
	return nil
}

func (s *Store) GetBaseline(name string) (model.Baseline, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, ok := s.baselines[name]
	return b, ok, nil
}

func (s *Store) ListBaselines() ([]model.Baseline, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Baseline, 0, len(s.baselines))
	for _, b := range s.baselines {
		out = append(out, b)
	}
	slices.SortFunc(out, func(a, b model.Baseline) int { return cmp.Compare(a.Name, b.Name) })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (s *Store) DeleteBaseline(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.baselines, name)
	return nil
}

func (s *Store) AppendRegressResult(baseline string, body json.RawMessage) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	seq := len(s.regress[baseline]) + 1
	s.regress[baseline] = append(s.regress[baseline], model.RegressResult{Baseline: baseline, Seq: seq, Body: body})
	return seq, nil
}

func (s *Store) RegressResultsFor(baseline string) ([]model.RegressResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.regress[baseline]
	if len(stored) == 0 {
		return nil, nil
	}
	out := make([]model.RegressResult, len(stored))
	copy(out, stored)
	return out, nil
}

func (s *Store) Close() error { return nil }

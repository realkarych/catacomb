package memory

import (
	"cmp"
	"slices"
	"sync"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type annKey struct {
	exec   string
	source string
	owner  string
	key    string
}

type Store struct {
	mu          sync.Mutex
	obs         []model.Observation
	nodes       map[string]*model.Node
	edges       map[string]*model.Edge
	runs        map[string]model.Run
	annotations map[annKey]model.Annotation
	cursors     map[string]model.TailCursor
	baselines   map[string]model.Baseline
	quarantine  int64
}

func New() *Store {
	return &Store{
		nodes:       map[string]*model.Node{},
		edges:       map[string]*model.Edge{},
		runs:        map[string]model.Run{},
		annotations: map[annKey]model.Annotation{},
		cursors:     map[string]model.TailCursor{},
		baselines:   map[string]model.Baseline{},
	}
}

func (s *Store) Persist(obs []model.Observation, nodes []*model.Node, edges []*model.Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obs = append(s.obs, obs...)
	for _, n := range nodes {
		s.nodes[n.ID] = n
	}
	for _, e := range edges {
		s.edges[e.ID] = e
	}
	return nil
}

func (s *Store) AppendDeltas(o model.Observation, deltas []cdc.GraphDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.obs = append(s.obs, o)
	for _, d := range deltas {
		s.applyDelta(d)
	}
	return nil
}

func (s *Store) applyDelta(d cdc.GraphDelta) {
	switch d.Kind {
	case cdc.DeltaNodeUpsert, cdc.DeltaNodeStatus:
		if d.Node != nil {
			s.nodes[d.Node.ID] = d.Node
		}
	case cdc.DeltaNodeMerge:
		if d.Node != nil {
			if d.OldID != "" {
				delete(s.nodes, d.OldID)
			}
			s.nodes[d.Node.ID] = d.Node
		}
	case cdc.DeltaEdgeUpsert:
		if d.Edge != nil {
			s.edges[d.Edge.ID] = d.Edge
		}
	case cdc.DeltaEdgeDelete:
		if d.Edge != nil {
			delete(s.edges, d.Edge.ID)
		}
	}
}

func (s *Store) MaxSeq() (uint64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var maxSeq uint64
	for _, o := range s.obs {
		if o.Seq > maxSeq {
			maxSeq = o.Seq
		}
	}
	return maxSeq, nil
}

func (s *Store) ObservationsSince(seq uint64) ([]model.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Observation
	for _, o := range s.obs {
		if o.Seq > seq {
			out = append(out, o)
		}
	}
	sortObservations(out)
	return out, nil
}

func (s *Store) ObservationsForExecution(executionID string) ([]model.Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Observation
	for _, o := range s.obs {
		if o.ExecutionID == executionID {
			out = append(out, o)
		}
	}
	sortObservations(out)
	return out, nil
}

func sortObservations(o []model.Observation) {
	slices.SortFunc(o, func(a, b model.Observation) int { return cmp.Compare(a.Seq, b.Seq) })
}

func (s *Store) UpsertRun(r model.Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[r.ID] = r
	return nil
}

func (s *Store) ListOpenRuns() ([]model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Run
	for _, r := range s.runs {
		if r.Status == model.StatusRunning {
			out = append(out, r)
		}
	}
	sortRuns(out)
	return out, nil
}

func (s *Store) Runs() ([]model.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.Run, 0, len(s.runs))
	for _, r := range s.runs {
		out = append(out, r)
	}
	sortRuns(out)
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func sortRuns(r []model.Run) {
	slices.SortFunc(r, func(a, b model.Run) int { return cmp.Compare(a.ID, b.ID) })
}

func (s *Store) Quarantine(model.QuarantineRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quarantine++
	return nil
}

func (s *Store) QuarantineCount() (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quarantine, nil
}

func (s *Store) UpsertTailCursor(c model.TailCursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cursors[c.Path] = c
	return nil
}

func (s *Store) LoadTailCursors() ([]model.TailCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.TailCursor, 0, len(s.cursors))
	for _, c := range s.cursors {
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b model.TailCursor) int { return cmp.Compare(a.Path, b.Path) })
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (s *Store) UpsertAnnotation(a model.Annotation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := annKey{exec: a.ExecutionID, source: a.SourceKey, owner: a.Owner, key: a.Key}
	existing, ok := s.annotations[k]
	if !ok || a.WriteSeq >= existing.WriteSeq {
		if ok && a.StepKey == "" {
			a.StepKey = existing.StepKey
		}
		s.annotations[k] = a
	}
	return nil
}

func (s *Store) AnnotationsForExecution(executionID string) ([]model.Annotation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []model.Annotation
	for k, a := range s.annotations {
		if k.exec == executionID {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b model.Annotation) int {
		return cmp.Or(cmp.Compare(a.SourceKey, b.SourceKey), cmp.Compare(a.Owner, b.Owner), cmp.Compare(a.Key, b.Key))
	})
	return out, nil
}

func (s *Store) MoveAnnotations(executionID, fromKey, toKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	moved := map[annKey]model.Annotation{}
	for k, a := range s.annotations {
		if k.exec == executionID && k.source == fromKey {
			a.SourceKey = toKey
			moved[annKey{exec: executionID, source: toKey, owner: k.owner, key: k.key}] = a
			delete(s.annotations, k)
		}
	}
	for k, a := range moved {
		s.annotations[k] = a
	}
	return nil
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

func (s *Store) Close() error { return nil }

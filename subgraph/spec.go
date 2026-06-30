package subgraph

import (
	"errors"
	"fmt"

	"github.com/realkarych/catacomb/model"
)

var ErrPhaseNotFound = errors.New("subgraph: phase not found")

type Spec struct {
	Phase string
	From  string
	To    string
}

func (s Spec) Empty() bool {
	return s.Phase == "" && s.From == "" && s.To == ""
}

type Parsed struct {
	isRange  bool
	name     string
	occ      int
	fromName string
	fromOcc  int
	toName   string
	toOcc    int
}

func ParseSpec(s Spec) (Parsed, error) {
	hasFrom := s.From != ""
	hasTo := s.To != ""
	if hasFrom != hasTo {
		return Parsed{}, fmt.Errorf("%w: from and to must both be set", ErrInvalidSelector)
	}
	if hasFrom {
		if s.Phase != "" {
			return Parsed{}, fmt.Errorf("%w: phase and from/to are mutually exclusive", ErrInvalidSelector)
		}
		fn, fo, err := ParseSelector(s.From)
		if err != nil {
			return Parsed{}, err
		}
		tn, to, err := ParseSelector(s.To)
		if err != nil {
			return Parsed{}, err
		}
		return Parsed{isRange: true, fromName: fn, fromOcc: fo, toName: tn, toOcc: to}, nil
	}
	n, o, err := ParseSelector(s.Phase)
	if err != nil {
		return Parsed{}, err
	}
	return Parsed{name: n, occ: o}, nil
}

func RangeWindow(nodes []*model.Node, execID, fromName string, fromOcc int, toName string, toOcc int) (Window, bool) {
	from, ok := PhaseWindow(nodes, execID, fromName, fromOcc)
	if !ok {
		return Window{}, false
	}
	to, ok := PhaseWindow(nodes, execID, toName, toOcc)
	if !ok {
		return Window{}, false
	}
	end := to.Start
	return Window{Start: from.Start, End: &end}, true
}

func ScopeExecutionParsed(nodes []*model.Node, edges []*model.Edge, execID string, p Parsed) ([]*model.Node, []*model.Edge, bool) {
	var w Window
	var ok bool
	if p.isRange {
		w, ok = RangeWindow(nodes, execID, p.fromName, p.fromOcc, p.toName, p.toOcc)
	} else {
		w, ok = PhaseWindow(nodes, execID, p.name, p.occ)
	}
	if !ok {
		return nil, nil, false
	}
	sn, se := Subgraph(nodes, edges, w)
	return sn, se, true
}

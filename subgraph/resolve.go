package subgraph

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/realkarych/catacomb/model"
)

var ErrInvalidSelector = errors.New("subgraph: invalid phase selector")

func ParseSelector(val string) (string, int, error) {
	name, occStr, hasOcc := strings.Cut(val, ",")
	if !hasOcc {
		return name, 0, nil
	}
	occ, err := strconv.Atoi(occStr)
	if err != nil {
		return "", 0, fmt.Errorf("%w: %q", ErrInvalidSelector, val)
	}
	return name, occ, nil
}

func PhaseWindow(nodes []*model.Node, execID, name string, occ int) (Window, bool) {
	id := model.PhaseMarkerID(execID, name, occ)
	for _, n := range nodes {
		if n.ID != id {
			continue
		}
		if n.TStart == nil {
			return Window{}, false
		}
		return Window{Start: *n.TStart, End: n.TEnd}, true
	}
	return Window{}, false
}

func ScopeExecution(nodes []*model.Node, edges []*model.Edge, execID, name string, occ int) ([]*model.Node, []*model.Edge, bool) {
	w, ok := PhaseWindow(nodes, execID, name, occ)
	if !ok {
		return nil, nil, false
	}
	sn, se := Subgraph(nodes, edges, w)
	return sn, se, true
}

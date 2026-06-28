package reduce

import "github.com/realkarych/catacomb/cdc"

func (g *Graph) EmitDelta(d cdc.GraphDelta) {
	g.emit(d)
}

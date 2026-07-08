package reduce

import "github.com/realkarych/catacomb/model"

type GraphDeltaKind string

const (
	DeltaNodeUpsert   GraphDeltaKind = "node_upsert"
	DeltaEdgeUpsert   GraphDeltaKind = "edge_upsert"
	DeltaNodeStatus   GraphDeltaKind = "node_status"
	DeltaNodeMerge    GraphDeltaKind = "node_merge"
	DeltaEdgeDelete   GraphDeltaKind = "edge_delete"
	DeltaRunStarted   GraphDeltaKind = "run_started"
	DeltaSessionEnded GraphDeltaKind = "session_ended"
	DeltaRunEnded     GraphDeltaKind = "run_ended"
)

type GraphDelta struct {
	Kind        GraphDeltaKind
	Rev         uint64
	Node        *model.Node
	Edge        *model.Edge
	OldID       string
	NewID       string
	RunID       string
	ExecutionID string
	Run         *model.Run
}

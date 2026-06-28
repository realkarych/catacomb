package export

import (
	"context"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type Exporter interface {
	Name() string
	ApplyDelta(ctx context.Context, d cdc.GraphDelta) error
	SnapshotState(ctx context.Context, nodes []*model.Node, edges []*model.Edge) error
	FlushRun(ctx context.Context, runID string) error
	Shutdown(ctx context.Context) error
}

type RunExporter interface {
	SnapshotRuns(ctx context.Context, runs []model.Run) error
}

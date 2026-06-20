package jsonl

import (
	"encoding/json"
	"io"

	"github.com/realkarych/catacomb/model"
)

func Snapshot(w io.Writer, nodes []*model.Node, edges []*model.Edge) error {
	enc := json.NewEncoder(w)
	for _, n := range nodes {
		if err := enc.Encode(struct {
			Kind string `json:"kind"`
			*model.Node
		}{"node", n}); err != nil {
			return err
		}
	}
	for _, e := range edges {
		if err := enc.Encode(struct {
			Kind string `json:"kind"`
			*model.Edge
		}{"edge", e}); err != nil {
			return err
		}
	}
	return nil
}

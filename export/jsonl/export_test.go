package jsonl

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestSnapshotOrderAndKinds(t *testing.T) {
	nodes := []*model.Node{{ID: "session:s1", RunID: "s1", Type: model.NodeSession}}
	edges := []*model.Edge{{ID: "e1", RunID: "s1", Type: model.EdgeParentChild, Src: "session:s1", Dst: "n2"}}

	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nodes, edges))

	var kinds []string
	sc := bufio.NewScanner(&buf)
	for sc.Scan() {
		var rec struct {
			Kind string `json:"kind"`
		}
		require.NoError(t, json.Unmarshal(sc.Bytes(), &rec))
		kinds = append(kinds, rec.Kind)
	}
	assert.Equal(t, []string{"node", "edge"}, kinds)
}

func TestSnapshotWriterErrorNode(t *testing.T) {
	err := Snapshot(failWriter{}, []*model.Node{{ID: "n"}}, nil)
	require.Error(t, err)
}

func TestSnapshotWriterErrorEdge(t *testing.T) {
	err := Snapshot(failWriter{}, nil, []*model.Edge{{ID: "e"}})
	require.Error(t, err)
}

func TestSnapshotEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, Snapshot(&buf, nil, nil))
	assert.Empty(t, buf.String())
}

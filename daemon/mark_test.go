package daemon

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/reduce"
	"github.com/realkarych/catacomb/store"
)

type markErrReader struct{}

func (markErrReader) Read([]byte) (int, error) { return 0, errors.New("read error") }

type obsForExecErrStore struct{ store.Store }

func (s *obsForExecErrStore) ObservationsForExecution(string) ([]model.Observation, error) {
	return nil, errors.New("load fail")
}

type markAppendDeltasErrStore struct{ store.Store }

func (s *markAppendDeltasErrStore) AppendDeltas(_ model.Observation, _ []cdc.GraphDelta) error {
	return errors.New("write fail")
}

func TestIngestMarkStartEndSynthesized(t *testing.T) {
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return ts }
	t.Cleanup(func() { nowFn = time.Now })

	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	require.NoError(t, d.IngestMark(MarkInput{
		SessionID: "s1",
		Name:      "phase1",
		Boundary:  "start",
	}))
	require.NoError(t, d.IngestMark(MarkInput{
		SessionID: "s1",
		Name:      "phase1",
		Boundary:  "end",
	}))

	d.mu.Lock()
	execID := d.execBySession["s1"]
	g := d.graphs[execID]
	d.mu.Unlock()

	nodes, _ := g.Snapshot()
	markerID := model.PhaseMarkerID(execID, "phase1", 0)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found, "phase marker should be synthesized after IngestMark start+end")
}

func TestIngestMarkUnknownSessionCreatesExec(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	err := d.IngestMark(MarkInput{
		SessionID: "new_session",
		Name:      "phase1",
		Boundary:  "start",
	})
	require.NoError(t, err)

	d.mu.Lock()
	_, known := d.execBySession["new_session"]
	d.mu.Unlock()
	assert.True(t, known)
}

func TestIngestMarkWithOccurrenceAndStateRef(t *testing.T) {
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return ts }
	t.Cleanup(func() { nowFn = time.Now })

	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	occ := 2
	require.NoError(t, d.IngestMark(MarkInput{
		SessionID:  "s1",
		Name:       "phase1",
		Boundary:   "start",
		Occurrence: &occ,
		StateRef:   "ckpt_xyz",
	}))
	require.NoError(t, d.IngestMark(MarkInput{
		SessionID: "s1",
		Name:      "phase1",
		Boundary:  "end",
	}))

	d.mu.Lock()
	execID := d.execBySession["s1"]
	g := d.graphs[execID]
	d.mu.Unlock()

	nodes, _ := g.Snapshot()
	markerID := model.PhaseMarkerID(execID, "phase1", 2)
	var found *model.Node
	for _, n := range nodes {
		if n.ID == markerID {
			found = n
			break
		}
	}
	require.NotNil(t, found, "explicit occurrence=2 should produce PhaseMarkerID occ=2")
	assert.Equal(t, "ckpt_xyz", found.Attrs["state_ref"])
}

func TestIngestMarkKnownSessionNoLoad(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "p", Boundary: "start"}))
	require.NoError(t, d.IngestMark(MarkInput{SessionID: "s1", Name: "p", Boundary: "end"}))
}

func TestMarkHTTPEndpoint(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	body, _ := json.Marshal(MarkInput{
		SessionID: "s1",
		Name:      "p1",
		Boundary:  "start",
	})
	req := httptest.NewRequest(http.MethodPost, "/v1/mark", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestMarkHTTPEndpointUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/mark", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestMarkHTTPEndpointBadJSON(t *testing.T) {
	d := New(tempStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/mark", bytes.NewReader([]byte(`not json`)))
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestMarkHTTPEndpointReadBodyError(t *testing.T) {
	d := New(tempStore(t))
	req := httptest.NewRequest(http.MethodPost, "/v1/mark", io.NopCloser(&markErrReader{}))
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestIngestMarkEvictedGraphReloadsFromStore(t *testing.T) {
	ts := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return ts }
	t.Cleanup(func() { nowFn = time.Now })

	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	execID := d.execBySession["s1"]
	delete(d.graphs, execID)
	d.mu.Unlock()

	err := d.IngestMark(MarkInput{SessionID: "s1", Name: "p", Boundary: "start"})
	require.NoError(t, err)

	d.mu.Lock()
	g := d.graphs[execID]
	d.mu.Unlock()
	assert.NotNil(t, g)
}

func TestIngestMarkEvictedGraphLoadError(t *testing.T) {
	base := tempStore(t)
	d := New(base)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	d.mu.Lock()
	execID := d.execBySession["s1"]
	delete(d.graphs, execID)
	d.store = &obsForExecErrStore{Store: base}
	d.mu.Unlock()

	err := d.IngestMark(MarkInput{SessionID: "s1", Name: "p", Boundary: "start"})
	require.NoError(t, err)
}

func TestIngestMarkStoreWriteError(t *testing.T) {
	base := tempStore(t)
	d := New(base)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	d.store = &markAppendDeltasErrStore{Store: base}
	err := d.IngestMark(MarkInput{SessionID: "s1", Name: "p", Boundary: "start"})
	require.NoError(t, err)
}

func TestIngestMarkPanicRecovery(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	orig := applyFn
	applyFn = func(_ *reduce.Graph, _ model.Observation) { panic("boom") }
	t.Cleanup(func() { applyFn = orig })
	err := d.IngestMark(MarkInput{SessionID: "s1", Name: "p", Boundary: "start"})
	require.NoError(t, err)
	assert.Greater(t, d.QuarantinedForTest(), int64(0))
}

func TestIngestMarkEmptyNameReturnsError(t *testing.T) {
	d := New(tempStore(t))
	err := d.IngestMark(MarkInput{SessionID: "s1", Name: "", Boundary: "start"})
	require.Error(t, err)
}

func TestIngestMarkInvalidBoundaryReturnsError(t *testing.T) {
	d := New(tempStore(t))
	err := d.IngestMark(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "bogus"})
	require.Error(t, err)
}

func TestHandleMarkEmptyNameReturns400(t *testing.T) {
	d := New(tempStore(t))
	body, _ := json.Marshal(MarkInput{SessionID: "s1", Name: "", Boundary: "start"})
	req := httptest.NewRequest(http.MethodPost, "/v1/mark", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestHandleMarkInvalidBoundaryReturns400(t *testing.T) {
	d := New(tempStore(t))
	body, _ := json.Marshal(MarkInput{SessionID: "s1", Name: "phase1", Boundary: "invalid"})
	req := httptest.NewRequest(http.MethodPost, "/v1/mark", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

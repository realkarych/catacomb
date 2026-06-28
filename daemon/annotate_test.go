package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/store"
)

func TestAnnotateDisabledByDefault(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	execID := d.execForTest("s1")
	sourceKey := model.NodeSourceKey(model.SessionNodeID(execID))
	err := d.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`9`))
	require.ErrorIs(t, err, ErrAnnotationsDisabled)
}

func TestAnnotateValidation(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowAnnotations(true)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	execID := d.execForTest("s1")
	sourceKey := model.NodeSourceKey(model.SessionNodeID(execID))
	tests := []struct {
		name  string
		owner string
		key   string
		value json.RawMessage
	}{
		{"empty_owner", "", "score", json.RawMessage(`9`)},
		{"dot_owner", "eval.bad", "score", json.RawMessage(`9`)},
		{"empty_key", "eval", "", json.RawMessage(`9`)},
		{"dot_key", "eval", "bad.key", json.RawMessage(`9`)},
		{"bad_json", "eval", "score", json.RawMessage(`{bad}`)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := d.Annotate(execID, sourceKey, tc.owner, tc.key, tc.value)
			require.ErrorIs(t, err, ErrInvalidAnnotation)
		})
	}
}

func TestAnnotateUnknownTarget(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowAnnotations(true)
	err := d.Annotate("noexec", "nokey", "eval", "score", json.RawMessage(`9`))
	require.ErrorIs(t, err, ErrAnnotationTarget)
}

func TestAnnotateAttachesUnionLWWAndEmits(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowAnnotations(true)
	fixedExecID(d)
	sub := d.Subscribe(16)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	execID := d.execForTest("s1")
	sourceKey := model.NodeSourceKey(model.SessionNodeID(execID))
	require.NoError(t, d.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`5`)))
	require.NoError(t, d.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`9`)))
	require.NoError(t, d.Annotate(execID, sourceKey, "other", "score", json.RawMessage(`2`)))

	n := d.graphs[execID].Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n)
	assert.Equal(t, json.RawMessage(`9`), n.Annotations["eval.score"])
	assert.Equal(t, json.RawMessage(`2`), n.Annotations["other.score"])
	assert.NotZero(t, n.Rev)

	s, err := d.store.AnnotationsForExecution(execID)
	require.NoError(t, err)
	assert.Len(t, s, 2)

	var upserts int
	for i := 0; i < 10; i++ {
		select {
		case delta := <-sub.C:
			if delta.Kind == cdc.DeltaNodeUpsert && delta.Node != nil && delta.Node.ID == model.SessionNodeID(execID) {
				upserts++
			}
		default:
		}
	}
	assert.GreaterOrEqual(t, upserts, 3)
	d.bus.Unsubscribe(sub)
}

func TestAnnotationsSurviveRebuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "g.db")
	s1, err := store.OpenSQLite(path)
	require.NoError(t, err)
	d1 := New(s1)
	d1.SetAllowAnnotations(true)
	fixedExecID(d1)
	require.NoError(t, d1.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	execID := d1.execForTest("s1")
	sourceKey := model.NodeSourceKey(model.SessionNodeID(execID))
	require.NoError(t, d1.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`42`)))
	require.NoError(t, s1.Close())

	s2, err := store.OpenSQLite(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	d2 := New(s2)
	require.NoError(t, d2.Recover())
	n := d2.graphs[execID].Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n)
	assert.Equal(t, json.RawMessage(`42`), n.Annotations["eval.score"])
}

func TestAnnotationSurvivesStatusChange(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowAnnotations(true)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	execID := d.execForTest("s1")
	sourceKey := model.NodeSourceKey(model.SessionNodeID(execID))
	require.NoError(t, d.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`7`)))
	require.NoError(t, d.Ingest("SessionEnd", []byte(`{"session_id":"s1"}`)))
	n := d.graphs[execID].Nodes[model.SessionNodeID(execID)]
	require.NotNil(t, n)
	assert.Equal(t, json.RawMessage(`7`), n.Annotations["eval.score"])
}

func TestCarryOverMergeMovesAnnotations(t *testing.T) {
	d := New(tempStore(t))
	d.SetAllowAnnotations(true)
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	execID := d.execForTest("s1")
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	oldID := model.ToolCallID(execID, "t1")
	sourceKey := model.NodeSourceKey(oldID)
	require.NoError(t, d.Annotate(execID, sourceKey, "eval", "score", json.RawMessage(`5`)))

	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t2","tool_input":{}}`)))
	newID := model.ToolCallID(execID, "t2")

	d.mu.Lock()
	d.carryOverMergeLocked(execID, oldID, newID)
	d.mu.Unlock()

	newNode := d.graphs[execID].Nodes[newID]
	require.NotNil(t, newNode)
	assert.Equal(t, json.RawMessage(`5`), newNode.Annotations["eval.score"])

	anns, err := d.store.AnnotationsForExecution(execID)
	require.NoError(t, err)
	for _, a := range anns {
		if a.Owner == "eval" && a.Key == "score" {
			assert.Equal(t, model.NodeSourceKey(newID), a.SourceKey)
		}
	}
}

func TestHandleNodeAnnotateGating(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	token := "testtoken"
	h := d.Handler(token)

	execID := d.execForTest("s1")
	sessionNodeID := model.SessionNodeID(execID)
	d.mu.Lock()
	var sessionHash string
	for sid := range d.execBySession {
		sessionHash = sid
		break
	}
	d.mu.Unlock()

	url := "/v1/sessions/" + sessionHash + "/nodes/" + sessionNodeID + "/annotations"

	req := httptest.NewRequest("POST", url, strings.NewReader(`{"owner":"eval","key":"score","value":9}`))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	req = httptest.NewRequest("POST", url, strings.NewReader(`{"owner":"eval","key":"score","value":9}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)

	d.SetAllowAnnotations(true)

	ghostURL := "/v1/sessions/" + sessionHash + "/nodes/noexist:tool:xyz/annotations"
	req = httptest.NewRequest("POST", ghostURL, strings.NewReader(`{"owner":"eval","key":"score","value":9}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	req = httptest.NewRequest("POST", url, strings.NewReader(`{badjson`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	req = httptest.NewRequest("POST", url, strings.NewReader(`{"owner":"eval","key":"score","value":9}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	n := d.graphs[execID].Nodes[sessionNodeID]
	require.NotNil(t, n)
	assert.NotNil(t, n.Annotations["eval.score"])
}

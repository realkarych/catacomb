package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getPhase(t *testing.T, srv *httptest.Server, hash, phaseSel string) *http.Response {
	t.Helper()
	resp, err := http.Get(srv.URL + "/v1/sessions/" + hash + "/phase/" + phaseSel + "?token=testtoken")
	require.NoError(t, err)
	return resp
}

func TestHandlePhaseFocus_OK(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	var evs []sseEvent
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&evs))
}

func TestHandlePhaseFocus_PhaseNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "ghost")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePhaseFocus_SessionNotFound(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "nope", "phase1")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlePhaseFocus_InvalidSelector(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	advancingClock(t)
	phaseSession(t, d)
	srv := httptest.NewServer(d.Handler("testtoken"))
	t.Cleanup(srv.Close)

	resp := getPhase(t, srv, "s1", "phase1,x")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

const transcriptLine = `{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}]}}`

func TestTranscriptHTTPSuccess(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	body := strings.NewReader(transcriptLine + "\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, time.Second, 10*time.Millisecond)
}

func TestTranscriptHTTPUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", strings.NewReader(""))
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestTranscriptHTTPBlankLinesSkipped(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	body := strings.NewReader("\n\n" + transcriptLine + "\n\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return len(d.GraphsForTest()) == 1 }, time.Second, 10*time.Millisecond)
}

func TestTranscriptHTTPBodyReadError(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", errReadCloser{})
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscriptHTTPNonJSONLineSessionID(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader("not-json\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestTranscriptHTTPHandlerPanicRecovered(t *testing.T) {
	d := New(tempStore(t))
	body := strings.NewReader("")
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", body)
	r.Header.Set("Authorization", "Bearer tok")
	pw := panicWriter{httptest.NewRecorder()}
	d.handleTranscript(pw, r)
}

func TestTranscriptSessionIDExtraction(t *testing.T) {
	got := transcriptSessionID([]byte(transcriptLine))
	assert.Equal(t, "s1", got)

	got2 := transcriptSessionID([]byte("not-json"))
	assert.Equal(t, "", got2)
}

func TestTranscriptHTTPThreadsSessionAcrossLines(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	body := strings.NewReader(transcriptLine + "\n")
	r := httptest.NewRequest(http.MethodPost, "/v1/transcript", body)
	r.Header.Set("Authorization", "Bearer tok")
	rec := httptest.NewRecorder()
	d.Handler("tok").ServeHTTP(rec, r)
	assert.Equal(t, http.StatusOK, rec.Code)
	require.Eventually(t, func() bool { return d.execForTest("s1") != "" }, time.Second, 10*time.Millisecond)
	execID := d.execForTest("s1")
	require.Eventually(t, func() bool {
		g := d.GraphsForTest()[execID]
		return g != nil && g.Nodes[model.ToolCallID(execID, "toolu_1")] != nil
	}, time.Second, 10*time.Millisecond)
}

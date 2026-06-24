package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/cdc"
	"github.com/realkarych/catacomb/model"
)

type noFlushWriter struct {
	header http.Header
	status int
}

func (w *noFlushWriter) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}
func (w *noFlushWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *noFlushWriter) WriteHeader(s int)           { w.status = s }

func TestSSENoFlusher500(t *testing.T) {
	d := New(tempStore(t))
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	w := &noFlushWriter{}
	d.handleSSE(w, r)
	assert.Equal(t, http.StatusInternalServerError, w.status)
}

func TestSSEUnauthorized(t *testing.T) {
	d := New(tempStore(t))
	srv := httptest.NewServer(d.Handler("secrettoken"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/subscribe")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSSESnapshotAndLiveDelta(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	addr := httpLn.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/v1/subscribe", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	events := make(chan map[string]any, 32)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			events <- ev
		}
	}()

	var snapshotKinds []string
	deadline := time.After(2 * time.Second)
	for len(snapshotKinds) < 1 {
		select {
		case ev := <-events:
			snapshotKinds = append(snapshotKinds, ev["kind"].(string))
		case <-deadline:
			t.Fatal("timed out waiting for snapshot event")
		}
	}
	assert.Contains(t, snapshotKinds, "node_upsert")

	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{}}`)))

	var liveDelta map[string]any
	deadline = time.After(3 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev["kind"].(string) == "node_upsert" {
				liveDelta = ev
				goto gotLive
			}
		case <-deadline:
			t.Fatal("timed out waiting for live delta")
		}
	}
gotLive:
	require.NotNil(t, liveDelta)
	_, hasNode := liveDelta["node"]
	assert.True(t, hasNode, "live node_upsert must contain 'node' field")

	cancel()
	<-errc
}

func TestSSEFilterDropsNonMatchingRun(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	addr := httpLn.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/v1/subscribe?run=run-A", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s-other"}`)))

	received := make(chan map[string]any, 8)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			received <- ev
		}
	}()

	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case ev := <-received:
		t.Fatalf("expected no events for filtered run, got: %v", ev)
	case <-timer.C:
	}

	cancel()
	<-errc
}

func TestSSEClientDisconnectUnsubscribes(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	addr := httpLn.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	baseline := d.busConsumerCountForTest()

	clientCtx, clientCancel := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(clientCtx, http.MethodGet,
		"http://"+addr+"/v1/subscribe", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	require.Eventually(t, func() bool {
		return d.busConsumerCountForTest() > baseline
	}, 2*time.Second, 10*time.Millisecond)

	clientCancel()
	_ = resp.Body.Close()

	require.Eventually(t, func() bool {
		return d.busConsumerCountForTest() == baseline
	}, 3*time.Second, 10*time.Millisecond)

	cancel()
	<-errc
}

func TestSSEQueryParamParsing(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet,
		"/v1/subscribe?run=r1&type=session&type=tool_call&tier=core", nil)
	f := parseSubFilter(r)
	assert.Equal(t, "r1", f.RunID)
	assert.ElementsMatch(t, []string{"session", "tool_call"}, f.NodeTypes)
	assert.Equal(t, []string{"core"}, f.Tiers)
}

func TestSSEQueryParamCommaList(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet,
		"/v1/subscribe?type=session,tool_call", nil)
	f := parseSubFilter(r)
	assert.ElementsMatch(t, []string{"session", "tool_call"}, f.NodeTypes)
}

func TestSSEIDField(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	addr := httpLn.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/v1/subscribe", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer tok")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })

	found := make(chan bool, 1)
	sc := bufio.NewScanner(resp.Body)
	go func() {
		for sc.Scan() {
			line := sc.Text()
			if strings.HasPrefix(line, "id: ") {
				found <- true
				return
			}
		}
		found <- false
	}()

	deadline := time.After(2 * time.Second)
	select {
	case ok := <-found:
		assert.True(t, ok, "SSE events must include id: field")
	case <-deadline:
		t.Fatal("timed out waiting for id: field in SSE stream")
	}

	cancel()
	<-errc
}

type countingFlusher struct {
	header        http.Header
	status        int
	succeedWrites int32
	writeCount    int32
}

func (w *countingFlusher) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *countingFlusher) Write(b []byte) (int, error) {
	n := atomic.AddInt32(&w.writeCount, 1)
	if n > atomic.LoadInt32(&w.succeedWrites) {
		return 0, fmt.Errorf("write error on call %d", n)
	}
	return len(b), nil
}

func (w *countingFlusher) WriteHeader(s int) { w.status = s }
func (w *countingFlusher) Flush()            {}

func TestSSEWriteEventIDError(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r = r.WithContext(ctx)

	w := &countingFlusher{}
	atomic.StoreInt32(&w.succeedWrites, 0)
	d.handleSSE(w, r)
}

func TestSSEWriteEventDataError(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r = r.WithContext(ctx)

	w := &countingFlusher{}
	atomic.StoreInt32(&w.succeedWrites, 1)
	d.handleSSE(w, r)
}

func TestSSEKeepAlivePing(t *testing.T) {
	d := New(tempStore(t))

	orig := sseTickerFn
	sseTickerFn = func() *time.Ticker {
		return time.NewTicker(10 * time.Millisecond)
	}
	t.Cleanup(func() { sseTickerFn = orig })

	pingReceived := make(chan struct{}, 1)
	w := &pingCaptureFlusher{
		onWrite: func(b []byte) {
			if strings.Contains(string(b), ": ping") {
				select {
				case pingReceived <- struct{}{}:
				default:
				}
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r = r.WithContext(ctx)

	go func() {
		select {
		case <-pingReceived:
			cancel()
		case <-time.After(2 * time.Second):
			cancel()
		}
	}()

	d.handleSSE(w, r)
}

func TestSSEKeepAlivePingWriteError(t *testing.T) {
	d := New(tempStore(t))

	orig := sseTickerFn
	sseTickerFn = func() *time.Ticker {
		return time.NewTicker(10 * time.Millisecond)
	}
	t.Cleanup(func() { sseTickerFn = orig })

	w := &pingErrorFlusher{succeedWrites: 0}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r = r.WithContext(ctx)

	d.handleSSE(w, r)
}

func TestSSEJSONMarshalError(t *testing.T) {
	d := New(tempStore(t))

	orig := sseJSONMarshal
	sseJSONMarshal = func(v any) ([]byte, error) {
		return nil, fmt.Errorf("marshal error")
	}
	t.Cleanup(func() { sseJSONMarshal = orig })

	origTicker := sseTickerFn
	sseTickerFn = func() *time.Ticker {
		return time.NewTicker(10 * time.Millisecond)
	}
	t.Cleanup(func() { sseTickerFn = origTicker })

	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	pinged := make(chan struct{}, 1)
	w := &pingCaptureFlusher{
		onWrite: func(b []byte) {
			if strings.Contains(string(b), ": ping") {
				select {
				case pinged <- struct{}{}:
				default:
				}
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r = r.WithContext(ctx)

	go func() {
		select {
		case <-pinged:
			cancel()
		case <-time.After(2 * time.Second):
			cancel()
		}
	}()

	d.handleSSE(w, r)
}

func TestSSEConsumerChannelClosed(t *testing.T) {
	d := New(tempStore(t))

	origTicker := sseTickerFn
	sseTickerFn = func() *time.Ticker {
		return time.NewTicker(15 * time.Second)
	}
	t.Cleanup(func() { sseTickerFn = origTicker })

	sub := d.SubscribeFiltered(SubFilter{}, 4)
	d.Unsubscribe(sub)

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		d.streamSSE(context.Background(), rec, rec, sub, func(cdc.GraphDelta) bool { return true })
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("streamSSE did not exit after channel close")
	}
}

func TestSSELiveDeltaWriteError(t *testing.T) {
	d := New(tempStore(t))

	origTicker := sseTickerFn
	sseTickerFn = func() *time.Ticker {
		return time.NewTicker(15 * time.Second)
	}
	t.Cleanup(func() { sseTickerFn = origTicker })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe", nil)
	r = r.WithContext(ctx)

	w := &countingFlusher{}
	atomic.StoreInt32(&w.succeedWrites, 0)

	handlerDone := make(chan struct{})
	go func() {
		defer close(handlerDone)
		d.handleSSE(w, r)
	}()

	require.Eventually(t, func() bool {
		return d.busConsumerCountForTest() > 0
	}, 2*time.Second, 10*time.Millisecond)

	d.bus.Publish(cdc.GraphDelta{
		Kind:  cdc.DeltaNodeUpsert,
		Rev:   1,
		RunID: "r1",
	})

	select {
	case <-handlerDone:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after live delta write error")
	}
}

func TestSSEPayloadOmittedFromWire(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t1","tool_input":{"cmd":"ls"}}`)))

	d.mu.Lock()
	g := d.graphs["exec1"]
	nodeID := model.ToolCallID("exec1", "t1")
	g.Nodes[nodeID].Payload = &model.Payload{Input: []byte(`{"cmd":"ls"}`), Hash: "abc123"}
	g.Nodes[nodeID].PayloadHash = "abc123"
	d.mu.Unlock()

	sub := d.SubscribeFiltered(SubFilter{}, 64)
	defer d.Unsubscribe(sub)

	var nodeEvent *cdc.GraphDelta
	for i := range sub.Snapshot {
		if sub.Snapshot[i].Node != nil && sub.Snapshot[i].Node.ID == nodeID {
			nodeEvent = &sub.Snapshot[i]
			break
		}
	}
	require.NotNil(t, nodeEvent, "snapshot must contain the tool-call node")

	ev := deltaToSSE(*nodeEvent)
	b, err := json.Marshal(ev)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))

	nodeMap, ok := m["node"].(map[string]any)
	require.True(t, ok, "SSE event must have a 'node' field")

	_, hasPayload := nodeMap["payload"]
	assert.False(t, hasPayload, "SSE wire must not include raw payload")

	_, hasHash := nodeMap["payload_hash"]
	assert.True(t, hasHash, "SSE wire must include payload_hash fingerprint")
}

type pingCaptureFlusher struct {
	header  http.Header
	status  int
	onWrite func([]byte)
}

func (w *pingCaptureFlusher) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *pingCaptureFlusher) Write(b []byte) (int, error) {
	if w.onWrite != nil {
		w.onWrite(b)
	}
	return len(b), nil
}

func (w *pingCaptureFlusher) WriteHeader(s int) { w.status = s }
func (w *pingCaptureFlusher) Flush()            {}

type pingErrorFlusher struct {
	header        http.Header
	status        int
	writeCount    int32
	succeedWrites int32
}

func (w *pingErrorFlusher) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *pingErrorFlusher) Write(b []byte) (int, error) {
	n := atomic.AddInt32(&w.writeCount, 1)
	succeed := atomic.LoadInt32(&w.succeedWrites)
	if n > succeed {
		return 0, fmt.Errorf("ping write error")
	}
	return len(b), nil
}

func (w *pingErrorFlusher) WriteHeader(s int) { w.status = s }
func (w *pingErrorFlusher) Flush()            {}

func TestParseSubFilterSession(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/v1/subscribe?session=s1", nil)
	f := parseSubFilter(r)
	assert.Equal(t, "s1", f.SessionID)
}

func TestSSEFilterDropsNonMatchingSession(t *testing.T) {
	d := New(tempStore(t))
	fixedExecID(d)
	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s1"}`)))

	httpLn := loopbackListener(t)
	grpcLn := loopbackListener(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	errc := make(chan error, 1)
	go func() { errc <- d.Serve(ctx, httpLn, grpcLn, "tok") }()

	addr := httpLn.Addr().String()
	require.Eventually(t, func() bool {
		resp, err := http.Get("http://" + addr + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 2*time.Second, 10*time.Millisecond)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+addr+"/v1/subscribe?session=s1&token=tok", nil)
	require.NoError(t, err)

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	events := make(chan map[string]any, 16)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			line := sc.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev map[string]any
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				continue
			}
			events <- ev
		}
	}()

	var snapshotKinds []string
	deadline := time.After(2 * time.Second)
	for len(snapshotKinds) < 1 {
		select {
		case ev := <-events:
			snapshotKinds = append(snapshotKinds, ev["kind"].(string))
		case <-deadline:
			t.Fatal("timed out waiting for s1 snapshot event")
		}
	}
	assert.Contains(t, snapshotKinds, "node_upsert")

	require.NoError(t, d.Ingest("SessionStart", []byte(`{"session_id":"s-other"}`)))
	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s-other","tool_name":"Bash","tool_use_id":"t2","tool_input":{}}`)))

	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case ev := <-events:
			execID, _ := ev["execution_id"].(string)
			d2 := d
			d2.mu.Lock()
			s1Execs := d2.executionsForSession("s1")
			d2.mu.Unlock()
			inS1 := false
			for _, e := range s1Execs {
				if e == execID {
					inS1 = true
				}
			}
			if !inS1 {
				t.Fatalf("received event for execution outside s1: execution_id=%q", execID)
			}
		case <-timer.C:
			goto doneWaiting
		}
	}
doneWaiting:

	require.NoError(t, d.Ingest("PreToolUse", []byte(`{"session_id":"s1","tool_name":"Bash","tool_use_id":"t3","tool_input":{}}`)))

	received := make(chan map[string]any, 1)
	deadline2 := time.After(2 * time.Second)
	for {
		select {
		case ev := <-events:
			received <- ev
			goto gotInSession
		case <-deadline2:
			t.Fatal("timed out waiting for in-session s1 live event")
		}
	}
gotInSession:
	inSessionEv := <-received
	assert.NotNil(t, inSessionEv)

	cancel()
	<-errc
}

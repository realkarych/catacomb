package tui

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/daemon"
)

func newClientFor(t *testing.T, h http.Handler) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewHTTPClient(daemon.Discovery{Addr: strings.TrimPrefix(srv.URL, "http://"), Token: "tok"})
}

func TestSessionsDecodes(t *testing.T) {
	want := []SessionSummary{{Session: "abc", Status: "ok"}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	})
	c := newClientFor(t, h)
	got, err := c.Sessions(t.Context())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestSessions401IsDaemonRestarted(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := newClientFor(t, h)
	_, err := c.Sessions(t.Context())
	require.ErrorIs(t, err, ErrDaemonRestarted)
}

func TestSessionsBadAddr(t *testing.T) {
	c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"})
	_, err := c.Sessions(t.Context())
	require.ErrorIs(t, err, ErrDaemonUnreachable)
}

func TestSessionsNon2xx(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c := newClientFor(t, h)
	_, err := c.Sessions(t.Context())
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDaemonRestarted)
	require.NotErrorIs(t, err, ErrDaemonUnreachable)
}

func TestSessionsBadJSON(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	})
	c := newClientFor(t, h)
	_, err := c.Sessions(t.Context())
	require.Error(t, err)
}

func TestGraphDecodes(t *testing.T) {
	want := []SseEvent{{Kind: "node_upsert", Rev: 1}}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	})
	c := newClientFor(t, h)
	got, err := c.Graph(t.Context(), "myhash")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestGraph401IsDaemonRestarted(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := newClientFor(t, h)
	_, err := c.Graph(t.Context(), "h")
	require.ErrorIs(t, err, ErrDaemonRestarted)
}

func TestGraph404IsSessionNotFound(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := newClientFor(t, h)
	_, err := c.Graph(t.Context(), "missing")
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestGraphNon2xx(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c := newClientFor(t, h)
	_, err := c.Graph(t.Context(), "h")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDaemonRestarted)
	require.NotErrorIs(t, err, ErrSessionNotFound)
}

func TestPayloadDecodes(t *testing.T) {
	want := PayloadView{NodeID: "n1", Redacted: false}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	})
	c := newClientFor(t, h)
	got, err := c.Payload(t.Context(), "hash", "n1")
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestPayload401IsDaemonRestarted(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := newClientFor(t, h)
	_, err := c.Payload(t.Context(), "h", "n")
	require.ErrorIs(t, err, ErrDaemonRestarted)
}

func TestPayload403IsContentDisabled(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	c := newClientFor(t, h)
	_, err := c.Payload(t.Context(), "h", "n")
	require.ErrorIs(t, err, ErrContentDisabled)
}

func TestPayload404IsPayloadNotFound(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	c := newClientFor(t, h)
	_, err := c.Payload(t.Context(), "h", "n")
	require.ErrorIs(t, err, ErrPayloadNotFound)
}

func TestSubscribeStreamsThenCloses(t *testing.T) {
	ev := SseEvent{Kind: "node_upsert", Rev: 1}
	evBytes, _ := json.Marshal(ev)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(evBytes)
		_, _ = w.Write([]byte("\n\n"))
		fl.Flush()
		<-r.Context().Done()
	})
	c := newClientFor(t, h)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	ch, err := c.Subscribe(ctx, "h", 0)
	require.NoError(t, err)

	msg := <-ch
	require.NoError(t, msg.Err)
	require.False(t, msg.Done)
	require.Equal(t, ev.Kind, msg.Event.Kind)

	cancel()

	var terminal StreamMsg
	for m := range ch {
		terminal = m
	}
	require.True(t, terminal.Done || terminal.Err != nil)
}

func TestSubscribeSendsLastEventID(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "42", r.Header.Get("Last-Event-ID"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})
	c := newClientFor(t, h)
	ch, err := c.Subscribe(t.Context(), "h", 42)
	require.NoError(t, err)
	for range ch {
	}
}

func TestSubscribeNoLastEventIDWhenZero(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "", r.Header.Get("Last-Event-ID"))
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})
	c := newClientFor(t, h)
	ch, err := c.Subscribe(t.Context(), "h", 0)
	require.NoError(t, err)
	for range ch {
	}
}

func TestSubscribe401IsDaemonRestarted(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	c := newClientFor(t, h)
	_, err := c.Subscribe(t.Context(), "h", 0)
	require.ErrorIs(t, err, ErrDaemonRestarted)
}

func TestSubscribeBadJSON(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: not-json\n\n"))
		fl.Flush()
		<-r.Context().Done()
	})
	c := newClientFor(t, h)
	ch, err := c.Subscribe(t.Context(), "h", 0)
	require.NoError(t, err)
	var errMsg StreamMsg
	for m := range ch {
		if m.Err != nil {
			errMsg = m
		}
	}
	require.Error(t, errMsg.Err)
}

func TestSubscribeEOF(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
	})
	c := newClientFor(t, h)
	ch, err := c.Subscribe(t.Context(), "h", 0)
	require.NoError(t, err)
	var last StreamMsg
	for m := range ch {
		last = m
	}
	require.True(t, last.Done)
}

func TestSubscribeNon2xx(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	c := newClientFor(t, h)
	_, err := c.Subscribe(t.Context(), "h", 0)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDaemonRestarted)
	require.NotErrorIs(t, err, ErrDaemonUnreachable)
}

func TestSubscribeSkipsNonDataLines(t *testing.T) {
	ev := SseEvent{Kind: "node_upsert", Rev: 2}
	evBytes, _ := json.Marshal(ev)
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(": ping\n"))
		_, _ = w.Write([]byte("id: 1\n"))
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(evBytes)
		_, _ = w.Write([]byte("\n\n"))
	})
	c := newClientFor(t, h)
	ch, err := c.Subscribe(t.Context(), "h", 0)
	require.NoError(t, err)
	got := <-ch
	require.Equal(t, ev.Kind, got.Event.Kind)
	for range ch {
	}
}

func TestGraphBadAddr(t *testing.T) {
	c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"})
	_, err := c.Graph(t.Context(), "h")
	require.ErrorIs(t, err, ErrDaemonUnreachable)
}

func TestSubscribeBadAddr(t *testing.T) {
	c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"})
	_, err := c.Subscribe(t.Context(), "h", 0)
	require.ErrorIs(t, err, ErrDaemonUnreachable)
}

func TestPayloadBadAddr(t *testing.T) {
	c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:1", Token: "tok"})
	_, err := c.Payload(t.Context(), "h", "n")
	require.ErrorIs(t, err, ErrDaemonUnreachable)
}

func TestGraphBadJSON(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	})
	c := newClientFor(t, h)
	_, err := c.Graph(t.Context(), "h")
	require.Error(t, err)
}

func TestPayloadNon2xx(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	c := newClientFor(t, h)
	_, err := c.Payload(t.Context(), "h", "n")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrDaemonRestarted)
	require.NotErrorIs(t, err, ErrContentDisabled)
	require.NotErrorIs(t, err, ErrPayloadNotFound)
}

func TestPayloadBadJSON(t *testing.T) {
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-json"))
	})
	c := newClientFor(t, h)
	_, err := c.Payload(t.Context(), "h", "n")
	require.Error(t, err)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

var (
	errFakeStreamBreak = errors.New("client_test: fake stream break")
	errFakeDialErr     = errors.New("client_test: fake dial error")
)

func TestSubscribeAppliesLineLargerThan64KB(t *testing.T) {
	big := strings.Repeat("x", 100*1024)
	ev := SseEvent{Kind: "node_upsert", Rev: 5, Node: &Node{ID: "n1", Type: "tool_call", Name: big}}
	evBytes, err := json.Marshal(ev)
	require.NoError(t, err)
	require.Greater(t, len(evBytes), 64*1024)

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fl := w.(http.Flusher)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: "))
		_, _ = w.Write(evBytes)
		_, _ = w.Write([]byte("\n\n"))
		fl.Flush()
		<-r.Context().Done()
	})
	c := newClientFor(t, h)
	ctx, cancel := context.WithCancel(t.Context())
	ch, err := c.Subscribe(ctx, "h", 0)
	require.NoError(t, err)

	msg := <-ch
	require.NoError(t, msg.Err)
	require.False(t, msg.Done)
	require.NotNil(t, msg.Event.Node)
	require.Equal(t, "n1", msg.Event.Node.ID)
	require.Equal(t, big, msg.Event.Node.Name)

	cancel()
	for range ch {
	}
}

func TestSubscribeReconnectsAndResumesFromLastRev(t *testing.T) {
	synctest.Test(t, func(st *testing.T) {
		ev9 := SseEvent{Kind: "node_upsert", Rev: 9}
		ev9Bytes, _ := json.Marshal(ev9)
		ev10 := SseEvent{Kind: "node_upsert", Rev: 10}
		ev10Bytes, _ := json.Marshal(ev10)

		type observedAttempt struct{ lastEventID string }
		attempts := make(chan observedAttempt, 8)

		var calls int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			attempts <- observedAttempt{lastEventID: req.Header.Get("Last-Event-ID")}
			switch calls {
			case 1:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(errReader{err: errFakeStreamBreak}),
				}, nil
			case 2:
				body := io.MultiReader(
					strings.NewReader("data: "+string(ev9Bytes)+"\n\n"),
					errReader{err: errFakeStreamBreak},
				)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(body),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("data: " + string(ev10Bytes) + "\n\n")),
				}, nil
			}
		})

		c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:0", Token: "tok"})
		c.sseHTTP.Transport = rt

		ch, err := c.Subscribe(st.Context(), "h", 7)
		require.NoError(st, err)

		a1 := <-attempts
		require.Equal(st, "7", a1.lastEventID)

		a2 := <-attempts
		require.Equal(st, "7", a2.lastEventID)

		msg9 := <-ch
		require.NoError(st, msg9.Err)
		require.Equal(st, uint64(9), msg9.Event.Rev)

		a3 := <-attempts
		require.Equal(st, "9", a3.lastEventID)

		msg10 := <-ch
		require.NoError(st, msg10.Err)
		require.Equal(st, uint64(10), msg10.Event.Rev)

		for range ch {
		}
	})
}

func TestSubscribeReconnectBackoffPacedAndExitsOnCancel(t *testing.T) {
	synctest.Test(t, func(st *testing.T) {
		attemptTimes := make(chan time.Time, 32)
		var calls int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			attemptTimes <- time.Now()
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(errReader{err: errFakeStreamBreak}),
				}, nil
			}
			return nil, errFakeDialErr
		})

		c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:0", Token: "tok"})
		c.sseHTTP.Transport = rt

		ctx, cancel := context.WithCancel(st.Context())
		ch, err := c.Subscribe(ctx, "h", 0)
		require.NoError(st, err)

		t1 := <-attemptTimes
		t2 := <-attemptTimes
		require.GreaterOrEqual(st, t2.Sub(t1), reconnectBaseDelay)

		t3 := <-attemptTimes
		require.GreaterOrEqual(st, t3.Sub(t2), reconnectBaseDelay)

		cancel()
		for range ch {
		}
	})
}

func TestSubscribeReconnect401StopsWithDaemonRestarted(t *testing.T) {
	synctest.Test(t, func(st *testing.T) {
		var calls int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if calls == 1 {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(errReader{err: errFakeStreamBreak}),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusUnauthorized,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		})

		c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:0", Token: "tok"})
		c.sseHTTP.Transport = rt

		ch, err := c.Subscribe(st.Context(), "h", 0)
		require.NoError(st, err)

		var last StreamMsg
		for m := range ch {
			last = m
		}
		require.ErrorIs(st, last.Err, ErrDaemonRestarted)
	})
}

func TestSubscribeMidLineErrorReconnectsNotFatal(t *testing.T) {
	synctest.Test(t, func(st *testing.T) {
		ev5 := SseEvent{Kind: "node_upsert", Rev: 5}
		ev5Bytes, _ := json.Marshal(ev5)
		ev10 := SseEvent{Kind: "node_upsert", Rev: 10}
		ev10Bytes, _ := json.Marshal(ev10)

		lastEventIDs := make(chan string, 8)
		var calls int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			lastEventIDs <- req.Header.Get("Last-Event-ID")
			if calls == 1 {
				body := io.MultiReader(
					strings.NewReader("data: "+string(ev5Bytes)+"\n\ndata: {\"kind\":\"node_upsert\",\"rev\":10"),
					errReader{err: errFakeStreamBreak},
				)
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(body),
				}, nil
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader("data: " + string(ev10Bytes) + "\n\n")),
			}, nil
		})

		c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:0", Token: "tok"})
		c.sseHTTP.Transport = rt

		ch, err := c.Subscribe(st.Context(), "h", 0)
		require.NoError(st, err)

		id1 := <-lastEventIDs
		require.Equal(st, "", id1)

		var got []uint64
		var sawErr error
		for m := range ch {
			if m.Err != nil {
				sawErr = m.Err
			}
			if m.Event.Rev > 0 {
				got = append(got, m.Event.Rev)
			}
		}
		require.NoError(st, sawErr)
		require.Equal(st, []uint64{5, 10}, got)

		id2 := <-lastEventIDs
		require.Equal(st, "5", id2)
	})
}

func TestSubscribeReconnectRetriesOnNon2xxThenSucceeds(t *testing.T) {
	synctest.Test(t, func(st *testing.T) {
		ev := SseEvent{Kind: "node_upsert", Rev: 3}
		evBytes, _ := json.Marshal(ev)
		var calls int
		rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			switch calls {
			case 1:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(errReader{err: errFakeStreamBreak}),
				}, nil
			case 2:
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("")),
				}, nil
			default:
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     make(http.Header),
					Body:       io.NopCloser(strings.NewReader("data: " + string(evBytes) + "\n\n")),
				}, nil
			}
		})

		c := NewHTTPClient(daemon.Discovery{Addr: "127.0.0.1:0", Token: "tok"})
		c.sseHTTP.Transport = rt

		ch, err := c.Subscribe(st.Context(), "h", 0)
		require.NoError(st, err)

		msg := <-ch
		require.NoError(st, msg.Err)
		require.Equal(st, uint64(3), msg.Event.Rev)

		for range ch {
		}
	})
}

func TestReconnectDelayGrowsThenCaps(t *testing.T) {
	require.Equal(t, 250*time.Millisecond, reconnectDelay(1))
	require.Equal(t, 500*time.Millisecond, reconnectDelay(2))
	require.Equal(t, 1*time.Second, reconnectDelay(3))
	require.Equal(t, 5*time.Second, reconnectDelay(10))
	require.Equal(t, 5*time.Second, reconnectDelay(1000))
}

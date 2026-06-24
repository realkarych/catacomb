package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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

package daemon

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"
)

func (d *Daemon) Handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook/{type}", d.authed(token, d.handleHook))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.metricsSnapshot())
	})
	return mux
}

func (d *Daemon) authed(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (d *Daemon) handleHook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = d.Ingest(r.PathValue("type"), payload)
	w.WriteHeader(http.StatusNoContent)
}

func (d *Daemon) reapLoop(ctx context.Context) {
	d.mu.Lock()
	w := d.reaperWindow
	d.mu.Unlock()
	ticker := time.NewTicker(w)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := d.reapIdle(nowFn()); err != nil {
				log.Printf("catacomb: reaper: %v", err)
			}
			d.evictTerminal(nowFn())
		}
	}
}

func (d *Daemon) Serve(ctx context.Context, ln net.Listener, token string) error {
	srv := &http.Server{Handler: d.Handler(token)}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go d.reapLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

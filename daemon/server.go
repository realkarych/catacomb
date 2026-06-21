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

	collectorv1 "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"github.com/realkarych/catacomb/export/otlp"
)

var newExporterFn = otlp.New

func (d *Daemon) Handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook/{type}", d.authed(token, d.handleHook))
	mux.HandleFunc("POST /v1/traces", d.authed(token, d.handleOTLP))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.metricsSnapshot())
	})
	return mux
}

func (d *Daemon) handleOTLP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	var req collectorv1.ExportTraceServiceRequest
	if err := proto.Unmarshal(body, &req); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	_ = d.IngestOTLP(&req)
	resp, _ := proto.Marshal(&collectorv1.ExportTraceServiceResponse{})
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(resp)
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

const exporterBufSize = 1024

var consumerLoopExitHook func()

func (d *Daemon) startExporter(ctx context.Context, httpAddr, grpcAddr string) {
	d.mu.Lock()
	endpoint := d.otlpEndpoint
	d.mu.Unlock()
	if endpoint == "" {
		return
	}
	exp, err := newExporterFn(ctx, endpoint, grpcAddr, httpAddr)
	if err != nil {
		log.Printf("catacomb: otlp exporter disabled: %v", err)
		return
	}
	d.mu.Lock()
	for _, g := range d.graphs {
		nodes, edges := g.Snapshot()
		_ = exp.SnapshotState(ctx, nodes, edges)
	}
	for _, g := range d.graphs {
		for _, r := range g.RunsSnapshot() {
			if r.EndedAt != nil {
				_ = exp.FlushRun(ctx, r.ID)
			}
		}
	}
	consumer := d.bus.Subscribe(exporterBufSize)
	d.exporterConsumer = consumer
	d.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				d.bus.Unsubscribe(consumer)
				_ = exp.Shutdown(ctx)
				return
			case delta, ok := <-consumer.C:
				if !ok {
					if consumerLoopExitHook != nil {
						consumerLoopExitHook()
					}
					return
				}
				_ = exp.ApplyDelta(ctx, delta)
			}
		}
	}()
}

func (d *Daemon) Serve(ctx context.Context, httpLn, grpcLn net.Listener, token string) error {
	srv := &http.Server{Handler: d.Handler(token)}
	grpcSrv := d.newGRPCServer(token)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())
	go d.reapLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = srv.Close()
		grpcSrv.GracefulStop()
	}()
	go d.serveGRPC(ctx, grpcSrv, grpcLn, func(s *grpc.Server, l net.Listener) error {
		return s.Serve(l)
	}, defaultWaitFn)
	if err := srv.Serve(httpLn); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

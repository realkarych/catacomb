package daemon

import (
	"bufio"
	"bytes"
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

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	neo4jexport "github.com/realkarych/catacomb/export/neo4j"
	"github.com/realkarych/catacomb/export/otlp"
	pgexport "github.com/realkarych/catacomb/export/postgres"
	tailingest "github.com/realkarych/catacomb/ingest/tail"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/webui"
)

var newExporterFn = otlp.New

var newPostgresFn = func(ctx context.Context, dsn string) (exportiface.Exporter, error) {
	return pgexport.New(ctx, dsn)
}

var newNeo4jFn = func(ctx context.Context, uri, user, password string) (exportiface.Exporter, error) {
	return neo4jexport.New(ctx, uri, user, password)
}

var tailTick = 500 * time.Millisecond

func (d *Daemon) Handler(token string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /hook/{type}", d.authed(token, d.handleHook))
	mux.HandleFunc("POST /v1/traces", d.authed(token, d.handleOTLP))
	mux.HandleFunc("POST /v1/stream-json", d.authed(token, d.handleStreamJSON))
	mux.HandleFunc("POST /v1/transcript", d.authed(token, d.handleTranscript))
	mux.HandleFunc("POST /v1/mark", d.authed(token, d.handleMark))
	mux.HandleFunc("GET /v1/subscribe", d.authedAllowQuery(token, d.handleSSE))
	mux.HandleFunc("GET /v1/sessions", d.authedAllowQuery(token, d.handleSessions))
	mux.HandleFunc("GET /v1/sessions/{hash}/graph", d.authedAllowQuery(token, d.handleSessionGraph))
	mux.HandleFunc("GET /v1/diff", d.authedAllowQuery(token, d.handleDiff))
	mux.HandleFunc("GET /v1/sessions/{hash}/nodes/{nodeId}/payload", d.authedAllowQuery(token, d.handleNodePayload))
	mux.HandleFunc("GET /v1/sessions/{hash}/subagent/{agentId}", d.authedAllowQuery(token, d.handleSubagentSubtree))
	mux.HandleFunc("POST /v1/sessions/{hash}/nodes/{nodeId}/annotations", d.authed(token, d.handleNodeAnnotate))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(d.metricsSnapshot())
	})
	mux.Handle("GET /", webui.Handler())
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

func (d *Daemon) handleStreamJSON(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("catacomb: stream-json handler recovered: %v", rec)
		}
	}()
	sc := bufio.NewScanner(r.Body)
	sc.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	var currentSession string
	for sc.Scan() {
		line := sc.Bytes()
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		buf := make([]byte, len(trimmed))
		copy(buf, trimmed)
		if s := streamSessionID(buf); s != "" {
			currentSession = s
		}
		_ = d.IngestStreamJSON(buf, currentSession)
	}
	if err := sc.Err(); err != nil {
		log.Printf("catacomb: stream-json scan: %v", err)
	}
	w.WriteHeader(http.StatusOK)
}

func streamSessionID(line []byte) string {
	var e struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return ""
	}
	return e.SessionID
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

func (d *Daemon) authedAllowQuery(token string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		headerOK := subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token))
		queryOK := subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("token")), []byte(token))
		if headerOK != 1 && queryOK != 1 {
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

func (d *Daemon) tailLoop(ctx context.Context) {
	d.mu.Lock()
	dir := d.transcriptDir
	excludes := append([]string{d.dbPath, cwdTranscriptExclude()}, d.transcriptExclude...)
	d.mu.Unlock()
	if dir == "" {
		return
	}
	tl := tailingest.New(dir, excludes, d.store, d)
	if err := tl.Load(); err != nil {
		log.Printf("catacomb: tailer load: %v", err)
		return
	}
	tl.Run(ctx, tailTick)
}

const exporterBufSize = 1024

var consumerLoopExitHook func()

func (d *Daemon) startExporter(ctx context.Context, httpAddr, grpcAddr string) {
	d.mu.Lock()
	otlpEndpoint := d.otlpEndpoint
	otlpProject := d.otlpProject
	postgresDSN := d.postgresDSN
	neo4jURI := d.neo4jURI
	neo4jUser := d.neo4jUser
	neo4jPassword := d.neo4jPassword
	d.mu.Unlock()

	var entries []exportiface.Exporter

	if otlpEndpoint != "" {
		exp, err := newExporterFn(ctx, otlpEndpoint, grpcAddr, httpAddr)
		if err != nil {
			log.Printf("catacomb: otlp exporter disabled: %v", err)
		} else {
			exp.SetProject(otlpProject)
			entries = append(entries, exp)
		}
	}

	if postgresDSN != "" && newPostgresFn != nil {
		exp, err := newPostgresFn(ctx, postgresDSN)
		if err != nil {
			log.Printf("catacomb: postgres exporter disabled: %v", err)
		} else {
			entries = append(entries, exp)
		}
	}

	if neo4jURI != "" && newNeo4jFn != nil {
		exp, err := newNeo4jFn(ctx, neo4jURI, neo4jUser, neo4jPassword)
		if err != nil {
			log.Printf("catacomb: neo4j exporter disabled: %v", err)
		} else {
			entries = append(entries, exp)
		}
	}

	if len(entries) == 0 {
		return
	}

	d.mu.Lock()
	for _, exp := range entries {
		for _, g := range d.graphs {
			nodes, edges := g.Snapshot()
			cp := make([]*model.Node, len(nodes))
			for i, n := range nodes {
				cp[i] = copyNode(n)
			}
			_ = exp.SnapshotState(ctx, cp, edges)
		}
		for _, g := range d.graphs {
			for _, r := range g.RunsSnapshot() {
				if r.EndedAt != nil {
					_ = exp.FlushRun(ctx, r.ID)
				}
			}
		}
		consumer := d.bus.Subscribe(exporterBufSize)
		d.exporterConsumers = append(d.exporterConsumers, consumer)
		go func(c *cdc.Consumer, ex exportiface.Exporter) {
			for {
				select {
				case <-ctx.Done():
					d.bus.Unsubscribe(c)
					_ = ex.Shutdown(ctx)
					return
				case delta, ok := <-c.C:
					if !ok {
						if consumerLoopExitHook != nil {
							consumerLoopExitHook()
						}
						return
					}
					_ = ex.ApplyDelta(ctx, delta)
				}
			}
		}(consumer, exp)
	}
	d.mu.Unlock()
}

func (d *Daemon) handleSessions(w http.ResponseWriter, _ *http.Request) {
	d.mu.Lock()
	sums := d.sessionSummaries()
	d.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sums)
}

func (d *Daemon) handleSessionGraph(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	d.mu.Lock()
	evs, err := d.sessionGraphDeltas(hash)
	d.mu.Unlock()
	if errors.Is(err, ErrSessionNotFound) {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(evs)
}

func (d *Daemon) Serve(ctx context.Context, httpLn, grpcLn net.Listener, token string) error {
	srv := &http.Server{Handler: d.Handler(token)}
	grpcSrv := d.newGRPCServer(token)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	d.startExporter(ctx, httpLn.Addr().String(), grpcLn.Addr().String())
	go d.reapLoop(ctx)
	go d.tailLoop(ctx)
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

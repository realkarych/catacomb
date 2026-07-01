package jsonl

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/realkarych/catacomb/cdc"
	exportiface "github.com/realkarych/catacomb/export"
	"github.com/realkarych/catacomb/model"
	"github.com/realkarych/catacomb/redact"
)

var ErrClosed = errors.New("jsonl: streamer closed")

var _ exportiface.Exporter = (*Streamer)(nil)

type Streamer struct {
	mu  sync.Mutex
	f   *os.File
	enc *json.Encoder
}

func NewStreamer(path string) (*Streamer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("jsonl.NewStreamer: %w", err)
	}
	return &Streamer{f: f, enc: json.NewEncoder(f)}, nil
}

func (s *Streamer) Name() string { return "jsonl" }

func (s *Streamer) ApplyDelta(_ context.Context, d cdc.GraphDelta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return fmt.Errorf("jsonl.Streamer.ApplyDelta: %w", ErrClosed)
	}
	if d.Node != nil {
		d.Node = redact.Node(d.Node)
	}
	if err := s.enc.Encode(d); err != nil {
		return fmt.Errorf("jsonl.Streamer.ApplyDelta: %w", err)
	}
	return nil
}

func (s *Streamer) SnapshotState(_ context.Context, nodes []*model.Node, edges []*model.Edge) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return fmt.Errorf("jsonl.Streamer.SnapshotState: %w", ErrClosed)
	}
	for _, n := range nodes {
		if err := s.enc.Encode(redact.Node(n)); err != nil {
			return fmt.Errorf("jsonl.Streamer.SnapshotState node: %w", err)
		}
	}
	for _, e := range edges {
		if err := s.enc.Encode(e); err != nil {
			return fmt.Errorf("jsonl.Streamer.SnapshotState edge: %w", err)
		}
	}
	return nil
}

func (s *Streamer) FlushRun(_ context.Context, _ string) error { return nil }

func (s *Streamer) Shutdown(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return nil
	}
	err := s.f.Close()
	s.f = nil
	s.enc = nil
	if err != nil {
		return fmt.Errorf("jsonl.Streamer.Shutdown: %w", err)
	}
	return nil
}

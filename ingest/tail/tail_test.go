package tail

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/realkarych/catacomb/model"
)

type fakeSink struct {
	mu    sync.Mutex
	lines []string
	sess  []string
	lossy []string
}

func (s *fakeSink) IngestTranscript(line []byte, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, string(line))
	s.sess = append(s.sess, sessionID)
	return nil
}

func (s *fakeSink) MarkLossy(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lossy = append(s.lossy, sessionID)
}

type memStore struct {
	mu sync.Mutex
	m  map[string]model.TailCursor
}

func newMemStore() *memStore { return &memStore{m: map[string]model.TailCursor{}} }

func (s *memStore) LoadTailCursors() ([]model.TailCursor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]model.TailCursor, 0, len(s.m))
	for _, c := range s.m {
		out = append(out, c)
	}
	return out, nil
}

func (s *memStore) UpsertTailCursor(c model.TailCursor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[c.Path] = c
	return nil
}

type errStore struct{}

func (errStore) LoadTailCursors() ([]model.TailCursor, error) { return nil, errors.New("load") }
func (errStore) UpsertTailCursor(model.TailCursor) error      { return nil }

type upsertErrStore struct{ *memStore }

func (upsertErrStore) UpsertTailCursor(model.TailCursor) error { return errors.New("upsert") }

type errSink struct{ lossy []string }

func (s *errSink) IngestTranscript([]byte, string) error { return errors.New("boom") }
func (s *errSink) MarkLossy(id string)                   { s.lossy = append(s.lossy, id) }

func write(t *testing.T, path, s string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(s)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

func TestPollOnceAppendOnlyNewLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s1.jsonl")
	line := `{"type":"assistant","sessionId":"s1","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}` + "\n"
	write(t, p, line)
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Len(t, sink.lines, 1)

	write(t, p, line)
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Len(t, sink.lines, 2)
	assert.Equal(t, []string{"s1", "s1"}, sink.sess)
}

func TestPollOnceBuffersPartialLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s2.jsonl")
	half := `{"type":"assistant","sessionId":"s2","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"x"}]}}`
	write(t, p, half)
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)

	write(t, p, "}\n")
	require.NoError(t, tl.PollOnce(context.Background()))
	require.Len(t, sink.lines, 1)
	assert.Equal(t, half+"}", sink.lines[0])
}

func TestPollOnceTruncationResetsAndMarksLossy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s3.jsonl")
	line := `{"type":"assistant","sessionId":"s3","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}` + "\n"
	write(t, p, line+line)
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	require.Len(t, sink.lines, 2)

	require.NoError(t, os.WriteFile(p, []byte(line), 0o600))
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Contains(t, sink.lossy, "s3")
	assert.Len(t, sink.lines, 3)
}

func TestPollOnceRotationDifferentHeadResetsAndMarksLossy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s4.jsonl")
	a := `{"type":"assistant","sessionId":"s4","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"mA","content":[{"type":"text","text":"a"}]}}` + "\n"
	write(t, p, a+a)
	sink := &fakeSink{}
	st := newMemStore()
	tl := New(dir, nil, st, sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	require.Len(t, sink.lines, 2)

	b := `{"type":"assistant","sessionId":"s4","timestamp":"2026-06-22T10:00:05Z","message":{"role":"assistant","id":"mB","content":[{"type":"text","text":"b"}]}}` + "\n"
	bigB := b + b + b + b + b + b
	require.NoError(t, os.WriteFile(p, []byte(bigB), 0o600))
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Contains(t, sink.lossy, "s4")
	assert.Equal(t, 8, len(sink.lines))
}

func TestRestartResumesFromPersistedCursor(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "s5.jsonl")
	line := `{"type":"assistant","sessionId":"s5","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}` + "\n"
	write(t, p, line)
	st := newMemStore()
	tl1 := New(dir, nil, st, &fakeSink{})
	require.NoError(t, tl1.Load())
	require.NoError(t, tl1.PollOnce(context.Background()))

	sink2 := &fakeSink{}
	tl2 := New(dir, nil, st, sink2)
	require.NoError(t, tl2.Load())
	require.NoError(t, tl2.PollOnce(context.Background()))
	assert.Empty(t, sink2.lines)

	write(t, p, line)
	require.NoError(t, tl2.PollOnce(context.Background()))
	assert.Len(t, sink2.lines, 1)
}

func TestPollOnceDiscoversNewFile(t *testing.T) {
	dir := t.TempDir()
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)

	p := filepath.Join(dir, "late.jsonl")
	write(t, p, `{"type":"assistant","sessionId":"late","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}`+"\n")
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Len(t, sink.lines, 1)
	assert.Equal(t, "late", sink.sess[0])
}

func TestPollOnceSkipsExcluded(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "self.jsonl")
	write(t, p, `{"type":"assistant","sessionId":"self","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"a"}]}}`+"\n")
	sink := &fakeSink{}
	tl := New(dir, []string{"self.jsonl"}, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)
}

func TestPollOnceCtxCancelled(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "s.jsonl"), `{"type":"assistant","sessionId":"s","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"a"}]}}`+"\n")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tl := New(dir, nil, newMemStore(), &fakeSink{})
	require.NoError(t, tl.Load())
	assert.Error(t, tl.PollOnce(ctx))
}

func TestPollOnceSinkErrorMarksLossy(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "s6.jsonl"), `{"type":"assistant","sessionId":"s6","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"a"}]}}`+"\n")
	sink := &errSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Contains(t, sink.lossy, "s6")
}

func TestLoadStoreError(t *testing.T) {
	tl := New(t.TempDir(), nil, errStore{}, &fakeSink{})
	assert.Error(t, tl.Load())
}

func TestPersistStoreErrorPropagates(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "s7.jsonl"), `{"type":"assistant","sessionId":"s7","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"a"}]}}`+"\n")
	tl := New(dir, nil, upsertErrStore{newMemStore()}, &fakeSink{})
	require.NoError(t, tl.Load())
	assert.Error(t, tl.PollOnce(context.Background()))
}

func TestRunStopsOnCtxCancel(t *testing.T) {
	dir := t.TempDir()
	tl := New(dir, nil, newMemStore(), &fakeSink{})
	require.NoError(t, tl.Load())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.Run(ctx, time.Millisecond); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop on ctx cancel")
	}
}

func TestNowFnDefault(t *testing.T) {
	assert.False(t, nowFn().Before(time.Now().Add(-time.Second)))
}

func withOpen(t *testing.T, fn func(string) (fileHandle, error)) {
	t.Helper()
	orig := openFile
	openFile = fn
	t.Cleanup(func() { openFile = orig })
}

func withStat(t *testing.T, fn func(string) (os.FileInfo, error)) {
	t.Helper()
	orig := statFn
	statFn = fn
	t.Cleanup(func() { statFn = orig })
}

type fakeHandle struct {
	data      []byte
	readErr   error
	readAtErr error
}

func (h *fakeHandle) Read(p []byte) (int, error) {
	if h.readErr != nil {
		return 0, h.readErr
	}
	return 0, io.EOF
}

func (h *fakeHandle) ReadAt(p []byte, _ int64) (int, error) {
	if h.readAtErr != nil {
		return 0, h.readAtErr
	}
	return copy(p, h.data), nil
}

func (h *fakeHandle) Close() error { return nil }

const tickLine = `{"type":"assistant","sessionId":"rt","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m","content":[{"type":"text","text":"a"}]}}` + "\n"

func TestRunPollsOnTick(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "rt.jsonl"), tickLine)
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { tl.Run(ctx, time.Millisecond); close(done) }()
	require.Eventually(t, func() bool {
		sink.mu.Lock()
		defer sink.mu.Unlock()
		return len(sink.lines) > 0
	}, 2*time.Second, time.Millisecond)
	cancel()
	<-done
}

func TestPollOnceExcludeEmptyAndGlob(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "g.jsonl"), tickLine)
	sink := &fakeSink{}
	tl := New(dir, []string{"", "*.jsonl"}, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)
}

func TestHeadFingerprintNEdgeCases(t *testing.T) {
	s, err := headFingerprintN("anything", 0)
	require.NoError(t, err)
	assert.Empty(t, s)

	withOpen(t, func(string) (fileHandle, error) { return nil, errors.New("open") })
	_, err = headFingerprintN("x", 10)
	assert.Error(t, err)

	withOpen(t, func(string) (fileHandle, error) { return &fakeHandle{readErr: errors.New("read")}, nil })
	_, err = headFingerprintN("x", 10)
	assert.Error(t, err)
}

func TestPollFileStatError(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "se.jsonl"), tickLine)
	withStat(t, func(string) (os.FileInfo, error) { return nil, errors.New("stat") })
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)
}

func TestPollFileOpenError(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "oe.jsonl"), tickLine)
	withOpen(t, func(string) (fileHandle, error) { return nil, errors.New("open") })
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)
}

func TestPollFileReadAtError(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "rae.jsonl"), tickLine)
	withOpen(t, func(string) (fileHandle, error) { return &fakeHandle{readAtErr: errors.New("readat")}, nil })
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)
}

package tail

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
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
	metas []model.SubagentMeta
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

func (s *fakeSink) IngestSubagentMeta(m model.SubagentMeta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metas = append(s.metas, m)
	return nil
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

func (s *errSink) IngestTranscript([]byte, string) error       { return errors.New("boom") }
func (s *errSink) MarkLossy(id string)                         { s.lossy = append(s.lossy, id) }
func (s *errSink) IngestSubagentMeta(model.SubagentMeta) error { return nil }

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

type countingStore struct {
	*memStore
	mu      sync.Mutex
	upserts int
}

func (s *countingStore) UpsertTailCursor(c model.TailCursor) error {
	s.mu.Lock()
	s.upserts++
	s.mu.Unlock()
	return s.memStore.UpsertTailCursor(c)
}

func (s *countingStore) upsertCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.upserts
}

func TestPollFileSkipsUnchangedSizeMtime(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "idle.jsonl")
	write(t, p, tickLine)

	st := &countingStore{memStore: newMemStore()}
	sink := &fakeSink{}
	tl := New(dir, nil, st, sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	firstUpserts := st.upsertCount()
	require.Greater(t, firstUpserts, 0)

	openCalls := 0
	withOpen(t, func(path string) (fileHandle, error) {
		openCalls++
		return os.Open(path)
	})

	upsertsBefore := st.upsertCount()
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Equal(t, 0, openCalls)
	assert.Equal(t, upsertsBefore, st.upsertCount())
}

func TestPollFileNewFileNotSkipped(t *testing.T) {
	dir := t.TempDir()
	st := &countingStore{memStore: newMemStore()}
	sink := &fakeSink{}
	tl := New(dir, nil, st, sink)
	require.NoError(t, tl.Load())

	openCalls := 0
	withOpen(t, func(path string) (fileHandle, error) {
		openCalls++
		return os.Open(path)
	})

	p := filepath.Join(dir, "newf.jsonl")
	write(t, p, tickLine)
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Greater(t, openCalls, 0)
	assert.Greater(t, st.upsertCount(), 0)
	assert.Len(t, sink.lines, 1)
}

func TestPollFileGrownFileIngested(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "grow.jsonl")
	write(t, p, tickLine)

	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	require.Len(t, sink.lines, 1)

	write(t, p, tickLine)
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Len(t, sink.lines, 2)
}

func TestPollFileMtimeChangedNotSizeStillPolled(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "mtime.jsonl")
	write(t, p, tickLine)

	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	require.Len(t, sink.lines, 1)

	info, err := os.Stat(p)
	require.NoError(t, err)
	origSize := info.Size()
	origMtime := info.ModTime().UnixNano()

	fakeMtime := origMtime + int64(time.Second)
	withStat(t, func(path string) (os.FileInfo, error) {
		real, serr := os.Stat(path)
		if serr != nil {
			return nil, serr
		}
		return &fakeSizeModTime{FileInfo: real, size: origSize, mtime: time.Unix(0, fakeMtime)}, nil
	})

	openCalls := 0
	withOpen(t, func(path string) (fileHandle, error) {
		openCalls++
		return os.Open(path)
	})

	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Greater(t, openCalls, 0)
}

func TestPollFileTruncationStillResetsAndLossy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "trunc2.jsonl")
	write(t, p, tickLine+tickLine)
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	require.Len(t, sink.lines, 2)

	require.NoError(t, os.WriteFile(p, []byte(tickLine), 0o600))
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Contains(t, sink.lossy, "trunc2")
}

type fakeSizeModTime struct {
	os.FileInfo
	size  int64
	mtime time.Time
}

func (f *fakeSizeModTime) Size() int64        { return f.size }
func (f *fakeSizeModTime) ModTime() time.Time { return f.mtime }

func TestAgentIDOf(t *testing.T) {
	assert.Equal(t, "abc123", agentIDOf("/some/path/agent-abc123.jsonl"))
	assert.Equal(t, "", agentIDOf("/some/path/main.jsonl"))
	assert.Equal(t, "", agentIDOf("/some/path/agent-.jsonl"))
}

func TestReadAgentMetaBadJSON(t *testing.T) {
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "agent-x.jsonl")
	metaPath := filepath.Join(dir, "agent-x.meta.json")
	require.NoError(t, os.WriteFile(metaPath, []byte("not-json"), 0o600))
	_, ok := readAgentMeta(agentPath)
	assert.False(t, ok)
}

func TestMainSessionOf(t *testing.T) {
	assert.Equal(t, "main-session-id", mainSessionOf("/some/dir/main-session-id/subagents/agent-agX.jsonl"))
	assert.Equal(t, "", mainSessionOf("/some/dir/main.jsonl"))
	assert.Equal(t, "", mainSessionOf("/some/dir/sess.jsonl"))
	assert.Equal(t, "", mainSessionOf("agent-x.jsonl"))
}

func TestPollOnceIngestsAgentTranscriptViaSymlink(t *testing.T) {
	real := t.TempDir()

	sessionID := "main-session-id"
	agentLine := `{"type":"assistant","sessionId":"` + sessionID + `","agentId":"agX","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"done"}]}}` + "\n"

	agentDir := filepath.Join(real, sessionID, "subagents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	agentFile := filepath.Join(agentDir, "agent-agX.jsonl")
	write(t, agentFile, agentLine)

	metaJSON := `{"agentType":"general-purpose","description":"Test agent","toolUseId":"toolu_abc"}`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "agent-agX.meta.json"), []byte(metaJSON), 0o600))

	symlinkDir := t.TempDir()
	mainFile := filepath.Join(symlinkDir, sessionID+".jsonl")
	target := filepath.Join(real, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(target, []byte(`{"type":"assistant","sessionId":"`+sessionID+`","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"main"}]}}`+"\n"), 0o600))
	require.NoError(t, os.Symlink(target, mainFile))

	sink := &fakeSink{}
	tl := New(symlinkDir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))

	sink.mu.Lock()
	metas := sink.metas
	sink.mu.Unlock()
	require.Len(t, metas, 1)
	assert.Equal(t, sessionID, metas[0].SessionID)
	assert.Equal(t, "agX", metas[0].AgentID)
	assert.Equal(t, "toolu_abc", metas[0].ToolUseID)
	assert.Equal(t, "general-purpose", metas[0].AgentType)
	assert.Equal(t, "Test agent", metas[0].Description)

	sink.mu.Lock()
	sessIDs := sink.sess
	sink.mu.Unlock()
	require.NotEmpty(t, sessIDs)
	for _, s := range sessIDs {
		assert.Equal(t, sessionID, s)
	}

	require.NoError(t, tl.PollOnce(context.Background()))
	sink.mu.Lock()
	assert.Len(t, sink.metas, 1, "meta should not be re-emitted")
	sink.mu.Unlock()
}

func TestPollOnceAgentLargeFirstLineUsesGrandparentSession(t *testing.T) {
	real := t.TempDir()

	sessionID := "157a2d02-8471-4c53-8612-4d87bd410ca0"
	bigTask := strings.Repeat("x", 8192)
	agentLine := `{"type":"user","sessionId":"` + sessionID + `","agentId":"agBig","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"user","content":[{"type":"text","text":"` + bigTask + `"}]}}` + "\n"
	require.Greater(t, len(agentLine), 4096)

	agentDir := filepath.Join(real, sessionID, "subagents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	agentFile := filepath.Join(agentDir, "agent-agBig.jsonl")
	write(t, agentFile, agentLine)

	metaJSON := `{"agentType":"general-purpose","description":"Big agent","toolUseId":"toolu_big"}`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "agent-agBig.meta.json"), []byte(metaJSON), 0o600))

	target := filepath.Join(real, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(target, []byte(`{"type":"assistant","sessionId":"`+sessionID+`","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"main"}]}}`+"\n"), 0o600))

	symlinkDir := t.TempDir()
	mainFile := filepath.Join(symlinkDir, sessionID+".jsonl")
	require.NoError(t, os.Symlink(target, mainFile))

	sink := &fakeSink{}
	tl := New(symlinkDir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))

	sink.mu.Lock()
	metas := sink.metas
	sessIDs := sink.sess
	lossy := sink.lossy
	sink.mu.Unlock()

	require.Len(t, metas, 1)
	assert.Equal(t, sessionID, metas[0].SessionID)
	assert.Equal(t, "agBig", metas[0].AgentID)
	assert.Equal(t, "toolu_big", metas[0].ToolUseID)

	require.NotEmpty(t, sessIDs)
	for _, s := range sessIDs {
		assert.Equal(t, sessionID, s)
		assert.NotContains(t, s, "agent-")
	}
	for _, m := range metas {
		assert.NotContains(t, m.SessionID, "agent-")
	}
	for _, l := range lossy {
		assert.NotContains(t, l, "agent-")
	}
}

func TestPollOnceAgentMissingMetaNoError(t *testing.T) {
	real := t.TempDir()
	sessionID := "sess-nometa"
	agentLine := `{"type":"assistant","sessionId":"` + sessionID + `","agentId":"agY","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"done"}]}}` + "\n"

	agentDir := filepath.Join(real, sessionID, "subagents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	agentFile := filepath.Join(agentDir, "agent-agY.jsonl")
	write(t, agentFile, agentLine)

	target := filepath.Join(real, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(target, []byte(`{"type":"assistant","sessionId":"`+sessionID+`","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"main"}]}}`+"\n"), 0o600))

	symlinkDir := t.TempDir()
	mainFile := filepath.Join(symlinkDir, sessionID+".jsonl")
	require.NoError(t, os.Symlink(target, mainFile))

	_ = agentFile
	sink := &fakeSink{}
	tl := New(symlinkDir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))

	sink.mu.Lock()
	assert.Empty(t, sink.metas)
	sink.mu.Unlock()
}

func TestPollOnceMissingMetaRetriedOnNextPoll(t *testing.T) {
	real := t.TempDir()

	sessionID := "main-sess-retry"
	agentLine := `{"type":"assistant","sessionId":"` + sessionID + `","agentId":"agZ","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"done"}]}}` + "\n"

	agentDir := filepath.Join(real, sessionID, "subagents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	agentFile := filepath.Join(agentDir, "agent-agZ.jsonl")
	write(t, agentFile, agentLine)

	target := filepath.Join(real, sessionID+".jsonl")
	require.NoError(t, os.WriteFile(target, []byte(`{"type":"assistant","sessionId":"`+sessionID+`","timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m2","content":[{"type":"text","text":"main"}]}}`+"\n"), 0o600))

	symlinkDir := t.TempDir()
	mainFile := filepath.Join(symlinkDir, sessionID+".jsonl")
	require.NoError(t, os.Symlink(target, mainFile))

	sink := &fakeSink{}
	tl := New(symlinkDir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))

	sink.mu.Lock()
	assert.NotEmpty(t, sink.lines, "lines must be ingested on first poll")
	assert.Empty(t, sink.metas, "no meta without .meta.json")
	sink.mu.Unlock()

	metaJSON := `{"agentType":"general-purpose","description":"Retry agent","toolUseId":"toolu_retry"}`
	require.NoError(t, os.WriteFile(filepath.Join(agentDir, "agent-agZ.meta.json"), []byte(metaJSON), 0o600))

	require.NoError(t, tl.PollOnce(context.Background()))

	sink.mu.Lock()
	assert.Len(t, sink.metas, 1, "meta must be emitted after .meta.json appears")
	sink.mu.Unlock()

	require.NoError(t, tl.PollOnce(context.Background()))

	sink.mu.Lock()
	assert.Len(t, sink.metas, 1, "meta must not be re-emitted on third poll")
	sink.mu.Unlock()
}

func TestGlobSubDirJSONL(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "subdir")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	p := filepath.Join(subDir, "s8.jsonl")
	write(t, p, tickLine)
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Len(t, sink.lines, 1)
}

func TestGlobBrokenSymlinkSkipped(t *testing.T) {
	dir := t.TempDir()
	brokenLink := filepath.Join(dir, "broken.jsonl")
	require.NoError(t, os.Symlink("/nonexistent/target.jsonl", brokenLink))
	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Empty(t, sink.lines)
}

func TestGlobDuplicatePathDeduped(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real.jsonl")
	require.NoError(t, os.WriteFile(real, []byte(tickLine), 0o600))

	link1 := filepath.Join(dir, "link1.jsonl")
	link2 := filepath.Join(dir, "link2.jsonl")
	require.NoError(t, os.Symlink(real, link1))
	require.NoError(t, os.Symlink(real, link2))

	tl := New(dir, nil, newMemStore(), &fakeSink{})
	require.NoError(t, tl.Load())
	paths := tl.glob()
	resolvedSeen := map[string]int{}
	for _, p := range paths {
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil {
			resolved = p
		}
		resolvedSeen[resolved]++
	}
	for rp, count := range resolvedSeen {
		assert.Equal(t, 1, count, "resolved path %s appears %d times", rp, count)
	}
}

func TestGlobSub2DirectAgentPath(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "level1", "level2", "subagents")
	require.NoError(t, os.MkdirAll(agentDir, 0o755))
	agentFile := filepath.Join(agentDir, "agent-xyz.jsonl")
	agentLine := `{"type":"assistant","sessionId":"sub2-sess","agentId":"xyz","isSidechain":true,"timestamp":"2026-06-22T10:00:00Z","message":{"role":"assistant","id":"m1","content":[{"type":"text","text":"sub2"}]}}` + "\n"
	write(t, agentFile, agentLine)

	sink := &fakeSink{}
	tl := New(dir, nil, newMemStore(), sink)
	require.NoError(t, tl.Load())
	require.NoError(t, tl.PollOnce(context.Background()))
	assert.Len(t, sink.lines, 1)
}

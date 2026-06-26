package tail

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/realkarych/catacomb/model"
)

var nowFn = time.Now

const fpBytes = 512

type fileHandle interface {
	io.Reader
	io.ReaderAt
	io.Closer
}

var (
	openFile = func(path string) (fileHandle, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	statFn = os.Stat
)

type Sink interface {
	IngestTranscript(line []byte, sessionID string) error
	MarkLossy(sessionID string)
}

type Store interface {
	LoadTailCursors() ([]model.TailCursor, error)
	UpsertTailCursor(c model.TailCursor) error
}

type fileState struct {
	cursor model.TailCursor
}

type Tailer struct {
	dir      string
	excludes []string
	store    Store
	sink     Sink
	files    map[string]*fileState
}

func New(dir string, excludes []string, st Store, sink Sink) *Tailer {
	return &Tailer{dir: dir, excludes: excludes, store: st, sink: sink, files: map[string]*fileState{}}
}

func (tl *Tailer) Load() error {
	cs, err := tl.store.LoadTailCursors()
	if err != nil {
		return fmt.Errorf("tail.Load: %w", err)
	}
	for _, c := range cs {
		tl.files[c.Path] = &fileState{cursor: c}
	}
	return nil
}

func (tl *Tailer) Run(ctx context.Context, tick time.Duration) {
	ticker := time.NewTicker(tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = tl.PollOnce(ctx)
		}
	}
}

func (tl *Tailer) glob() []string {
	var out []string
	main, _ := filepath.Glob(filepath.Join(tl.dir, "*.jsonl"))
	sub, _ := filepath.Glob(filepath.Join(tl.dir, "*", "*.jsonl"))
	sub2, _ := filepath.Glob(filepath.Join(tl.dir, "*", "*", "subagents", "agent-*.jsonl"))
	out = append(out, main...)
	out = append(out, sub...)
	out = append(out, sub2...)
	return out
}

func (tl *Tailer) excluded(path string) bool {
	for _, ex := range tl.excludes {
		if ex == "" {
			continue
		}
		if strings.Contains(path, ex) {
			return true
		}
		if ok, _ := filepath.Match(ex, filepath.Base(path)); ok {
			return true
		}
	}
	return false
}

func sessionOf(path string) string {
	return strings.TrimSuffix(filepath.Base(path), ".jsonl")
}

func fingerprint(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func headFingerprintN(path string, n int64) (string, error) {
	if n <= 0 {
		return "", nil
	}
	if n > fpBytes {
		n = fpBytes
	}
	f, err := openFile(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, n)
	got, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", err
	}
	return fingerprint(buf[:got]), nil
}

func (tl *Tailer) PollOnce(ctx context.Context) error {
	for _, path := range tl.glob() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if tl.excluded(path) {
			continue
		}
		if err := tl.pollFile(path); err != nil {
			return err
		}
	}
	return nil
}

func (tl *Tailer) pollFile(path string) error {
	info, err := statFn(path)
	if err != nil {
		return nil
	}
	st, ok := tl.files[path]
	if !ok {
		st = &fileState{cursor: model.TailCursor{Path: path}}
		tl.files[path] = st
	}
	if ok && info.Size() == st.cursor.Size && info.ModTime().UnixNano() == st.cursor.Mtime {
		return nil
	}
	size := info.Size()
	session := sessionOf(path)

	if size < st.cursor.Offset {
		st.cursor.Offset = 0
		st.cursor.Fingerprint = ""
		tl.sink.MarkLossy(session)
	} else if st.cursor.Offset > 0 && st.cursor.Fingerprint != "" {
		head, herr := headFingerprintN(path, st.cursor.Offset)
		if herr == nil && head != st.cursor.Fingerprint {
			st.cursor.Offset = 0
			st.cursor.Fingerprint = ""
			tl.sink.MarkLossy(session)
		}
	}

	if size == st.cursor.Offset {
		return tl.persistFingerprint(st, info)
	}

	f, err := openFile(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, size-st.cursor.Offset)
	n, err := f.ReadAt(buf, st.cursor.Offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	data := buf[:n]
	advance := bytes.LastIndexByte(data, '\n')
	if advance < 0 {
		return tl.persistFingerprint(st, info)
	}
	for _, raw := range bytes.Split(data[:advance+1], []byte{'\n'}) {
		trimmed := bytes.TrimSpace(raw)
		if len(trimmed) == 0 {
			continue
		}
		if serr := tl.sink.IngestTranscript(trimmed, session); serr != nil {
			tl.sink.MarkLossy(session)
		}
	}
	st.cursor.Offset += int64(advance + 1)
	return tl.persistFingerprint(st, info)
}

func (tl *Tailer) persistFingerprint(st *fileState, info os.FileInfo) error {
	if st.cursor.Offset > 0 {
		fp, err := headFingerprintN(st.cursor.Path, st.cursor.Offset)
		if err == nil {
			st.cursor.Fingerprint = fp
		}
	}
	st.cursor.Size = info.Size()
	st.cursor.Mtime = info.ModTime().UnixNano()
	if err := tl.store.UpsertTailCursor(st.cursor); err != nil {
		return fmt.Errorf("tail.persist: %w", err)
	}
	return nil
}

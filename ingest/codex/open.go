package codex

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/klauspost/compress/zstd"
)

func Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("codex.Open: %w", err)
	}
	if !strings.HasSuffix(path, ".zst") {
		return f, nil
	}
	dec, _ := zstd.NewReader(f)
	return &zstReadCloser{file: f, dec: dec}, nil
}

type zstReadCloser struct {
	file *os.File
	dec  *zstd.Decoder
}

func (z *zstReadCloser) Read(p []byte) (int, error) {
	return z.dec.Read(p)
}

func (z *zstReadCloser) Close() error {
	z.dec.Close()
	return z.file.Close()
}

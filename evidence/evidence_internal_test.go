package evidence

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errWriteBoom = errors.New("write boom")

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errWriteBoom }

type failAfterNWriter struct {
	buf  bytes.Buffer
	left int
}

func (w *failAfterNWriter) Write(p []byte) (int, error) {
	if w.left == 0 {
		return 0, errWriteBoom
	}
	w.left--
	return w.buf.Write(p)
}

func TestRedactLinesPropagatesWriteErrorOnFirstLine(t *testing.T) {
	err := redactLines(strings.NewReader("{\"a\":1}\n"), failWriter{})
	require.ErrorIs(t, err, errWriteBoom)
}

func TestRedactLinesStopsAtFirstWriteErrorWithoutSwallowingIt(t *testing.T) {
	out := &failAfterNWriter{left: 1}
	err := redactLines(strings.NewReader("{\"a\":1}\n{\"b\":2}\n{\"c\":3}\n"), out)
	require.ErrorIs(t, err, errWriteBoom)
	assert.Equal(t, "{\"a\":1}\n", out.buf.String(), "redactLines must stop at the failing line, not keep writing past it")
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errWriteBoom }

func TestRedactLinesPropagatesReadError(t *testing.T) {
	var out bytes.Buffer
	require.ErrorIs(t, redactLines(failReader{}, &out), errWriteBoom)
	assert.Empty(t, out.String())
}

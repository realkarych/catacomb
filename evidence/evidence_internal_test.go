package evidence

import (
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("boom") }

func TestRedactLinesWriteError(t *testing.T) {
	require.Error(t, redactLines(strings.NewReader("{\"a\":1}\n"), failWriter{}))
}

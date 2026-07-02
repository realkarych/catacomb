package bench

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendResult(t *testing.T) {
	encErr := errors.New("enc boom")
	closeErr := errors.New("close boom")

	require.NoError(t, appendResult(nil, nil))

	err := appendResult(encErr, nil)
	require.Error(t, err)
	assert.ErrorIs(t, err, encErr)

	err = appendResult(nil, closeErr)
	require.Error(t, err)
	assert.ErrorIs(t, err, closeErr)

	err = appendResult(encErr, closeErr)
	require.Error(t, err)
	assert.ErrorIs(t, err, encErr)
}

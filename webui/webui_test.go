package webui

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandlerServesIndexHTML(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/index.html")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
}

func TestHandlerServesRootAsIndexHTML(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/html")
}

func TestHandlerMissingAsset404(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/does-not-exist.png")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlerIndexIsBuiltShell(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.Contains(t, string(body), `id="app"`)
	assert.Contains(t, string(body), "<title>Catacomb</title>")
	assert.Contains(t, string(body), `type="module"`)
}

func TestHandlerAssetsHaveCorrectContentType(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/assets/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.NotEqual(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestHandlerSubFSError500(t *testing.T) {
	orig := subFn
	subFn = func(_ fs.FS, _ string) (fs.FS, error) {
		return nil, errors.New("forced sub error")
	}
	t.Cleanup(func() { subFn = orig })

	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

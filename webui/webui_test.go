package webui

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestHandlerServesAppJS(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/app.js")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	ct := resp.Header.Get("Content-Type")
	assert.True(t, strings.Contains(ct, "javascript") || strings.Contains(ct, "text/plain"),
		"expected JS content-type, got: %s", ct)
}

func TestHandlerServesStyleCSS(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/style.css")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Contains(t, resp.Header.Get("Content-Type"), "text/css")
}

func TestHandlerMissingAsset404(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/does-not-exist.png")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHandlerIndexContainsAppMount(t *testing.T) {
	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var sb strings.Builder
	_, err = io.Copy(&sb, resp.Body)
	require.NoError(t, err)
	body := sb.String()
	assert.Contains(t, body, `id="app"`)
	assert.Contains(t, body, `app.js`)
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

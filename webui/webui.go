package webui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed web
var assets embed.FS

var subFn = fs.Sub

func Handler() http.Handler {
	sub, err := subFn(assets, "web")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		})
	}
	return http.FileServerFS(sub)
}

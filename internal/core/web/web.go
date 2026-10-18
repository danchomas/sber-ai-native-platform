package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assets embed.FS

func Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(sub, trimLeadingSlash(r.URL.Path)); err != nil {
			index, err := fs.ReadFile(sub, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(index)
			return
		}
		files.ServeHTTP(w, r)
	})
}

func trimLeadingSlash(path string) string {
	if path == "" || path == "/" {
		return "index.html"
	}
	return path[1:]
}

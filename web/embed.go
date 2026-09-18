// Package webui liefert die gebaute React-Oberfläche aus. Die Dateien aus web/dist
// werden zur Bauzeit per embed.FS in das Binary aufgenommen.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var assets embed.FS

// Handler liefert die WebUI als Single-Page-Application aus: vorhandene Dateien
// direkt, alles andere als index.html, damit clientseitiges Routing funktioniert.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		panic(err)
	}
	files := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			// index.html direkt ausliefern: der FileServer würde sonst auf "./"
			// umleiten.
			serveIndex(w, sub)
			return
		}
		if info, err := fs.Stat(sub, name); err != nil || info.IsDir() {
			serveIndex(w, sub)
			return
		}
		files.ServeHTTP(w, r)
	})
}

// serveIndex ist der SPA-Fallback.
func serveIndex(w http.ResponseWriter, sub fs.FS) {
	raw, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		http.Error(w, "WebUI ist nicht eingebettet — bitte zuerst `npm run build` in web/ ausführen", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(raw)
}

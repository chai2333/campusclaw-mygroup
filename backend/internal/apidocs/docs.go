// Package apidocs serves the API contract and self-contained Swagger UI.
package apidocs

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:generate go run ../../cmd/openapi -root ../..

//go:embed openapi.json swagger.html assets/*
var files embed.FS

func Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		data, _ := files.ReadFile("openapi.json")
		_, _ = w.Write(data)
	})
	assets, _ := fs.Sub(files, "assets")
	mux.Handle("GET /swagger/assets/", http.StripPrefix("/swagger/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /swagger/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		data, _ := files.ReadFile("swagger.html")
		_, _ = w.Write(data)
	})
}

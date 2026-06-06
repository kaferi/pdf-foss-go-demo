// Package server wires storage + renderer into an http.Handler and serves the
// static frontend. It performs no PDF work directly.
package server

import (
	"encoding/json"
	"net/http"

	"pdf-foss-demo/internal/renderer"
	"pdf-foss-demo/internal/storage"
)

type Server struct {
	store    *storage.Store
	renderer *renderer.Renderer
	mux      *http.ServeMux
}

// New builds the server. webDir is the path to the static frontend directory.
func New(s *storage.Store, r *renderer.Renderer, webDir string) *Server {
	srv := &Server{store: s, renderer: r, mux: http.NewServeMux()}
	srv.mux.HandleFunc("GET /api/files", srv.handleListFiles)
	srv.mux.Handle("GET /static/", http.StripPrefix("/static/",
		http.FileServer(http.Dir(webDir+"/static"))))
	return srv
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) { s.mux.ServeHTTP(w, r) }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	files, err := s.store.List()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if files == nil {
		files = []storage.Meta{}
	}
	writeJSON(w, http.StatusOK, files)
}

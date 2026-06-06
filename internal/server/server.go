// Package server wires storage + renderer into an http.Handler and serves the
// static frontend. It performs no PDF work directly.
package server

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
	srv.mux.HandleFunc("POST /api/upload", srv.handleUpload)
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

const maxUploadBytes = 50 << 20 // 50 MB

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		http.Error(w, "upload too large or malformed: "+err.Error(), http.StatusBadRequest)
		return
	}
	f, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing file field: "+err.Error(), http.StatusBadRequest)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		http.Error(w, "read failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	// PDF magic check only — we do NOT open the file with the library on upload.
	if len(data) < 5 || string(data[:5]) != "%PDF-" {
		http.Error(w, "not a PDF (missing %PDF- header)", http.StatusBadRequest)
		return
	}
	name := hdr.Filename
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	id, err := s.store.CreateUpload(name, data)
	if err != nil {
		http.Error(w, "save failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": id})
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

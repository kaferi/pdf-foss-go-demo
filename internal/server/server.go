// Package server wires storage + renderer into an http.Handler and serves the
// static frontend. It performs no PDF work directly.
package server

import (
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"pdf-foss-demo/internal/renderer"
	"pdf-foss-demo/internal/storage"
)

// hexID matches the 32-hex-character file IDs produced by storage.newID.
var hexID = regexp.MustCompile(`^[0-9a-f]{32}$`)

// validID reports whether id is a well-formed file ID. This is a security guard:
// IDs arrive from the URL path and are joined into filesystem paths, so anything
// that is not exactly a 32-char hex string (e.g. "..", a URL-encoded slash that
// net/http decoded into the path value) must be rejected to prevent traversal
// outside the data volume.
func validID(id string) bool { return hexID.MatchString(id) }

type Server struct {
	store    *storage.Store
	renderer *renderer.Renderer
	mux      *http.ServeMux
	webDir   string
}

// New builds the server. webDir is the path to the static frontend directory.
func New(s *storage.Store, r *renderer.Renderer, webDir string) *Server {
	srv := &Server{store: s, renderer: r, mux: http.NewServeMux(), webDir: webDir}
	// Single-page app: both "/" and "/view/{id}" serve the same index.html; the
	// frontend renders the two-pane master-detail UI and opens {id} client-side.
	srv.mux.HandleFunc("GET /{$}", srv.handleApp)
	srv.mux.HandleFunc("GET /view/{id}", srv.handleApp)
	srv.mux.HandleFunc("GET /api/files", srv.handleListFiles)
	srv.mux.HandleFunc("POST /api/upload", srv.handleUpload)
	srv.mux.HandleFunc("GET /api/files/{id}", srv.handleFileMeta)
	srv.mux.HandleFunc("POST /api/files/{id}/render", srv.handleRender)
	srv.mux.HandleFunc("POST /api/files/{id}/rerender", srv.handleRerender)
	srv.mux.HandleFunc("DELETE /api/files/{id}", srv.handleDelete)
	srv.mux.HandleFunc("GET /files/{id}/original.pdf", srv.handleOriginal)
	srv.mux.HandleFunc("GET /files/{id}/pages/{n}", srv.handlePagePNG)
	srv.mux.Handle("GET /static/", noCacheHandler(http.StripPrefix("/static/",
		http.FileServer(http.Dir(webDir+"/static")))))
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

func (s *Server) handleFileMeta(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		http.NotFound(w, r)
		return
	}
	m, err := s.store.ReadMeta(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.ReadMeta(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.renderer.Render(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m, _ := s.store.ReadMeta(id)
	writeJSON(w, http.StatusOK, m) // status may be "ready" or "error"
}

// handleRerender forces a fresh render even for an already-rendered file (used
// to re-check a file against a new library build).
func (s *Server) handleRerender(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.ReadMeta(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.renderer.Rerender(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	m, _ := s.store.ReadMeta(id)
	writeJSON(w, http.StatusOK, m) // status may be "ready" or "error"
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		http.NotFound(w, r)
		return
	}
	if _, err := s.store.ReadMeta(id); err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.store.Delete(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleOriginal(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		http.NotFound(w, r)
		return
	}
	m, err := s.store.ReadMeta(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	// "inline" so the browser opens the PDF in its built-in viewer (new tab)
	// instead of downloading it. The filename hint is still used if the user
	// saves from the viewer. FormatMediaType quotes/escapes the filename per
	// RFC 6266, preventing header injection via crafted names.
	disp := mime.FormatMediaType("inline", map[string]string{"filename": m.OriginalName})
	if disp == "" {
		disp = "inline"
	}
	w.Header().Set("Content-Disposition", disp)
	http.ServeFile(w, r, s.store.OriginalPath(id))
}

func (s *Server) handlePagePNG(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !validID(id) {
		http.NotFound(w, r)
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		http.NotFound(w, r)
		return
	}
	path := s.store.PagePNGPath(id, n)
	if _, err := os.Stat(path); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	http.ServeFile(w, r, path)
}

func (s *Server) handleApp(w http.ResponseWriter, r *http.Request) {
	noCache(w)
	http.ServeFile(w, r, s.webDir+"/index.html")
}

// noCache tells the browser it may store the response but must revalidate it
// before reuse. Combined with the ETag/Last-Modified that ServeFile/FileServer
// already emit, an unchanged asset returns a cheap 304, but a rebuilt index.html
// or app.js is picked up immediately — no manual hard-reload needed.
func noCache(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-cache")
}

// noCacheHandler wraps h so every response carries the no-cache header.
func noCacheHandler(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		noCache(w)
		h.ServeHTTP(w, r)
	})
}

# PDF Upload → Render → Viewer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A Dockerized Go web app that lets users upload PDFs (cheaply, no parsing), render all pages of a chosen file to PNG on demand using only aspose-pdf-foss-for-go, and view them in a vertically scrolling Acrobat-style viewer.

**Architecture:** Single Go binary on `net/http`. Three internal packages — `storage` (filesystem layout on the `/data` volume, no library), `renderer` (the only place that opens PDFs with the library; owns a capacity-1 semaphore and `recover()` wrapping), and `server` (HTTP handlers + static serving). Plain HTML/CSS/JS frontend, no build step. No database; the file list is a scan of `/data/*/meta.json`.

**Tech Stack:** Go 1.26 stdlib, `github.com/aspose-pdf-foss/aspose-pdf-foss-for-go` (pinned to `main`), plain HTML/CSS/JS, Docker multi-stage build.

---

## Hard constraint (applies to every task)

All PDF work — open, PageCount, render — goes ONLY through aspose-pdf-foss-for-go. No other third-party PDF/render library. Upload must NOT open the file with the library. `original.pdf` is written before any parse/render and is never auto-deleted.

## File structure

```
go.mod / go.sum                         # module pdf-foss-demo (exists)
main.go                                 # wires config + server, starts http
internal/storage/storage.go            # paths, meta read/write, list, delete
internal/storage/storage_test.go
internal/renderer/renderer.go          # Render(fileId) — opens lib, renders all pages, semaphore, recover
internal/renderer/renderer_test.go
internal/server/server.go              # http.Handler: routes, upload, list, render, status, delete, png, original, static
internal/server/server_test.go
web/index.html                          # upload form + file list
web/view.html                           # viewer / rendering-poll / error UI
web/static/app.css
web/static/app.js                       # upload, list render, poll, lazy viewer wiring
Dockerfile
.dockerignore
.gitignore
```

Library API facts (verified against the pinned commit):
- `asposepdf.Open(path string) (*Document, error)`
- `(*Document).PageCount() int`
- `(*Document).Page(n int) (*Page, error)` — **1-based**
- `(*Page).RenderPNG(w io.Writer, opts asposepdf.RenderOptions) error`
- `asposepdf.RenderOptions{ DPI float64; Background *asposepdf.Color }` — DPI 0 ⇒ default 150
- No `Close()` exists; release the `*Document` reference and let GC reclaim it. Render one file at a time.
- For test fixtures: `asposepdf.NewDocumentFromFormat(asposepdf.PageFormatA4)`, `(*Page).AddText(text, asposepdf.TextStyle{...}, asposepdf.Rectangle{...})`, `(*Document).Save(path)`.

Import alias used everywhere: `asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"`.

---

## Task 1: storage — meta types and paths

**Files:**
- Create: `internal/storage/storage.go`
- Test: `internal/storage/storage_test.go`

- [ ] **Step 1: Write the failing test**

```go
package storage

import (
	"path/filepath"
	"testing"
)

func TestPathsUnderRoot(t *testing.T) {
	s := New(t.TempDir())
	id := "abc123"
	if got := s.OriginalPath(id); got != filepath.Join(s.Root, id, "original.pdf") {
		t.Fatalf("OriginalPath = %q", got)
	}
	if got := s.MetaPath(id); got != filepath.Join(s.Root, id, "meta.json") {
		t.Fatalf("MetaPath = %q", got)
	}
	if got := s.PagePNGPath(id, 1); got != filepath.Join(s.Root, id, "pages", "0001.png") {
		t.Fatalf("PagePNGPath = %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestPathsUnderRoot -v`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Write minimal implementation**

```go
// Package storage owns the on-disk layout of the /data volume. It performs no
// PDF work and imports no PDF library.
package storage

import (
	"fmt"
	"path/filepath"
)

// Status values for a file's meta.json.
const (
	StatusUploaded  = "uploaded"
	StatusRendering = "rendering"
	StatusReady     = "ready"
	StatusError     = "error"
)

// RenderError captures a failure during parse/render so it survives in meta.json.
type RenderError struct {
	Stage   string `json:"stage"`             // "parse" | "render" | "write"
	Page    int    `json:"page,omitempty"`    // 1-based page, when applicable
	Message string `json:"message"`           // full library error or panic + stack
}

// Meta is the persisted record for one uploaded file.
type Meta struct {
	ID           string       `json:"id"`
	OriginalName string       `json:"originalName"`
	Size         int64        `json:"size"`
	UploadedAt   string       `json:"uploadedAt"`
	Status       string       `json:"status"`
	Pages        int          `json:"pages,omitempty"`
	RenderedAt   string       `json:"renderedAt,omitempty"`
	Error        *RenderError `json:"error,omitempty"`
}

// Store is rooted at the volume mount (e.g. /data).
type Store struct{ Root string }

func New(root string) *Store { return &Store{Root: root} }

func (s *Store) dir(id string) string          { return filepath.Join(s.Root, id) }
func (s *Store) OriginalPath(id string) string { return filepath.Join(s.dir(id), "original.pdf") }
func (s *Store) MetaPath(id string) string     { return filepath.Join(s.dir(id), "meta.json") }
func (s *Store) PagesDir(id string) string     { return filepath.Join(s.dir(id), "pages") }
func (s *Store) PagePNGPath(id string, n int) string {
	return filepath.Join(s.PagesDir(id), fmt.Sprintf("%04d.png", n))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -run TestPathsUnderRoot -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/storage/storage.go internal/storage/storage_test.go
git commit -m "feat(storage): meta types and volume paths"
```

---

## Task 2: storage — create, write/read meta, list, delete

**Files:**
- Modify: `internal/storage/storage.go`
- Test: `internal/storage/storage_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestCreateWriteReadListDelete(t *testing.T) {
	s := New(t.TempDir())
	id, err := s.CreateUpload("report.pdf", []byte("%PDF-1.7 fake"))
	if err != nil {
		t.Fatal(err)
	}
	// original.pdf written
	if b, err := os.ReadFile(s.OriginalPath(id)); err != nil || string(b) != "%PDF-1.7 fake" {
		t.Fatalf("original not written: %v %q", err, b)
	}
	// meta is uploaded
	m, err := s.ReadMeta(id)
	if err != nil {
		t.Fatal(err)
	}
	if m.Status != StatusUploaded || m.OriginalName != "report.pdf" || m.Size != 13 {
		t.Fatalf("bad meta: %+v", m)
	}
	// list returns it
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 1 || all[0].ID != id {
		t.Fatalf("list = %+v", all)
	}
	// update meta round-trips
	m.Status = StatusReady
	m.Pages = 3
	if err := s.WriteMeta(m); err != nil {
		t.Fatal(err)
	}
	if m2, _ := s.ReadMeta(id); m2.Status != StatusReady || m2.Pages != 3 {
		t.Fatalf("update lost: %+v", m2)
	}
	// delete removes the dir
	if err := s.Delete(id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadMeta(id); err == nil {
		t.Fatal("expected error after delete")
	}
}
```

Add imports `"os"` to the test file.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/storage/ -run TestCreateWriteReadListDelete -v`
Expected: FAIL — `undefined: s.CreateUpload`

- [ ] **Step 3: Write minimal implementation**

Append to `storage.go` (add imports `crypto/rand`, `encoding/hex`, `encoding/json`, `os`, `time`, `sort`):

```go
// newID returns a filesystem-safe random id.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// CreateUpload writes original.pdf FIRST, then a meta.json with status "uploaded".
// It never opens the PDF with any library.
func (s *Store) CreateUpload(originalName string, data []byte) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.dir(id), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.OriginalPath(id), data, 0o644); err != nil {
		return "", err
	}
	m := Meta{
		ID:           id,
		OriginalName: originalName,
		Size:         int64(len(data)),
		UploadedAt:   time.Now().UTC().Format(time.RFC3339),
		Status:       StatusUploaded,
	}
	if err := s.WriteMeta(m); err != nil {
		return "", err
	}
	return id, nil
}

func (s *Store) WriteMeta(m Meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.MetaPath(m.ID), b, 0o644)
}

func (s *Store) ReadMeta(id string) (Meta, error) {
	var m Meta
	b, err := os.ReadFile(s.MetaPath(id))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// List scans /data/*/meta.json, newest upload first.
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.ReadMeta(e.Name())
		if err != nil {
			continue // skip incomplete dirs
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UploadedAt > out[j].UploadedAt })
	return out, nil
}

func (s *Store) Delete(id string) error { return os.RemoveAll(s.dir(id)) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/storage/ -v`
Expected: PASS (both tests)

- [ ] **Step 5: Commit**

```bash
git add internal/storage/
git commit -m "feat(storage): create upload, meta IO, list, delete"
```

---

## Task 3: renderer — render all pages of a ready/uploaded file

**Files:**
- Create: `internal/renderer/renderer.go`
- Test: `internal/renderer/renderer_test.go`

- [ ] **Step 1: Write the failing test**

```go
package renderer

import (
	"os"
	"testing"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"
	"pdf-foss-demo/internal/storage"
)

// makePDF builds a real 2-page PDF using the library itself (allowed: this IS
// the library under test) and returns its bytes.
func makePDF(t *testing.T) []byte {
	t.Helper()
	doc := asposepdf.NewDocumentFromFormat(asposepdf.PageFormatA4)
	if err := doc.AddBlankPageFromFormat(asposepdf.PageFormatA4); err != nil {
		t.Fatal(err)
	}
	p, _ := doc.Page(1)
	if err := p.AddText("Hello page 1", asposepdf.TextStyle{Size: 24},
		asposepdf.Rectangle{LLX: 50, LLY: 700, URX: 545, URY: 760}); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir() + "/in.pdf"
	if err := doc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRenderReadyProducesPNGs(t *testing.T) {
	s := storage.New(t.TempDir())
	id, err := s.CreateUpload("ok.pdf", makePDF(t))
	if err != nil {
		t.Fatal(err)
	}
	r := New(s)
	if err := r.Render(id); err != nil {
		t.Fatalf("Render: %v", err)
	}
	m, _ := s.ReadMeta(id)
	if m.Status != storage.StatusReady {
		t.Fatalf("status = %q, err=%+v", m.Status, m.Error)
	}
	if m.Pages != 2 {
		t.Fatalf("pages = %d", m.Pages)
	}
	for n := 1; n <= 2; n++ {
		if _, err := os.Stat(s.PagePNGPath(id, n)); err != nil {
			t.Fatalf("missing png %d: %v", n, err)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/renderer/ -run TestRenderReadyProducesPNGs -v`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Write minimal implementation**

```go
// Package renderer is the ONLY place that opens PDFs with aspose-pdf-foss-for-go.
// It renders every page of a file to PNG, one file at a time (memory bound), and
// converts any error or panic into a persisted error status — never a crash.
package renderer

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"

	asposepdf "github.com/aspose-pdf-foss/aspose-pdf-foss-for-go"
	"pdf-foss-demo/internal/storage"
)

const dpi = 150.0

type Renderer struct {
	store *storage.Store
	sem   chan struct{} // capacity 1: one render at a time
}

func New(s *storage.Store) *Renderer {
	return &Renderer{store: s, sem: make(chan struct{}, 1)}
}

// Render renders all pages of the file to PNG. It is idempotent: a file already
// "ready" returns immediately. Errors/panics are recorded in meta (status
// "error") and the original.pdf is left intact. The returned error is non-nil
// only for storage failures unrelated to the PDF content; PDF failures are
// reported via meta status, not the return value, so callers can always proceed.
func (r *Renderer) Render(id string) error {
	m, err := r.store.ReadMeta(id)
	if err != nil {
		return err
	}
	if m.Status == storage.StatusReady {
		return nil
	}

	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	// Re-read after acquiring the semaphore in case another request rendered it.
	if m, err = r.store.ReadMeta(id); err != nil {
		return err
	}
	if m.Status == storage.StatusReady {
		return nil
	}

	m.Status = storage.StatusRendering
	m.Error = nil
	if err := r.store.WriteMeta(m); err != nil {
		return err
	}

	rerr := r.renderAllPages(id, &m)
	if rerr != nil {
		m.Status = storage.StatusError
		m.Error = rerr
	} else {
		m.Status = storage.StatusReady
		m.RenderedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return r.store.WriteMeta(m)
}

// renderAllPages opens the document and writes one PNG per page. Any panic from
// the library is recovered and returned as a *storage.RenderError with a stack.
func (r *Renderer) renderAllPages(id string, m *storage.Meta) (rerr *storage.RenderError) {
	defer func() {
		if rec := recover(); rec != nil {
			rerr = &storage.RenderError{
				Stage:   "render",
				Message: fmt.Sprintf("panic: %v\n%s", rec, debug.Stack()),
			}
		}
	}()

	doc, err := asposepdf.Open(r.store.OriginalPath(id))
	if err != nil {
		return &storage.RenderError{Stage: "parse", Message: err.Error()}
	}

	pages := doc.PageCount()
	m.Pages = pages

	if err := os.MkdirAll(r.store.PagesDir(id), 0o755); err != nil {
		return &storage.RenderError{Stage: "write", Message: err.Error()}
	}

	for n := 1; n <= pages; n++ {
		page, err := doc.Page(n)
		if err != nil {
			return &storage.RenderError{Stage: "render", Page: n, Message: err.Error()}
		}
		f, err := os.Create(r.store.PagePNGPath(id, n))
		if err != nil {
			return &storage.RenderError{Stage: "write", Page: n, Message: err.Error()}
		}
		if err := page.RenderPNG(f, asposepdf.RenderOptions{DPI: dpi}); err != nil {
			f.Close()
			return &storage.RenderError{Stage: "render", Page: n, Message: err.Error()}
		}
		if err := f.Close(); err != nil {
			return &storage.RenderError{Stage: "write", Page: n, Message: err.Error()}
		}
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/renderer/ -run TestRenderReadyProducesPNGs -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/renderer/
git commit -m "feat(renderer): render all pages to PNG with semaphore and recover"
```

---

## Task 4: renderer — error path preserves original and records error

**Files:**
- Test: `internal/renderer/renderer_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRenderCorruptRecordsErrorAndKeepsOriginal(t *testing.T) {
	s := storage.New(t.TempDir())
	id, err := s.CreateUpload("broken.pdf", []byte("%PDF-1.7 not really a pdf"))
	if err != nil {
		t.Fatal(err)
	}
	r := New(s)
	if err := r.Render(id); err != nil {
		t.Fatalf("Render returned storage error: %v", err)
	}
	m, _ := s.ReadMeta(id)
	if m.Status != storage.StatusError {
		t.Fatalf("status = %q, want error", m.Status)
	}
	if m.Error == nil || m.Error.Message == "" {
		t.Fatalf("expected error detail, got %+v", m.Error)
	}
	// original.pdf must still be present
	if _, err := os.Stat(s.OriginalPath(id)); err != nil {
		t.Fatalf("original was removed: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails or passes**

Run: `go test ./internal/renderer/ -run TestRenderCorruptRecordsErrorAndKeepsOriginal -v`
Expected: PASS if the library returns an error/panic on the corrupt input (the recover + parse-error paths in Task 3 already handle both). If it unexpectedly FAILS, inspect `m.Error` and adjust the `renderAllPages` error mapping — do not weaken the test.

- [ ] **Step 3: (only if needed)**

No new code expected; Task 3 already covers parse error and panic. If the library neither errors nor panics on this input (renders 0 pages "ready"), change the fixture to a clearly invalid PDF (e.g. `[]byte("not a pdf at all")`) and re-run.

- [ ] **Step 4: Run the full renderer suite**

Run: `go test ./internal/renderer/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/renderer/
git commit -m "test(renderer): corrupt input records error and keeps original"
```

---

## Task 5: server — handler struct, static, home list JSON

**Files:**
- Create: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pdf-foss-demo/internal/renderer"
	"pdf-foss-demo/internal/storage"
)

func newTestServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	s := storage.New(t.TempDir())
	srv := New(s, renderer.New(s), "../../web")
	return srv, s
}

func TestListFilesJSON(t *testing.T) {
	srv, s := newTestServer(t)
	if _, err := s.CreateUpload("a.pdf", []byte("%PDF-1.7 x")); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/files", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("code = %d", w.Code)
	}
	var got []storage.Meta
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].OriginalName != "a.pdf" {
		t.Fatalf("body = %s", w.Body.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestListFilesJSON -v`
Expected: FAIL — `undefined: New`

- [ ] **Step 3: Write minimal implementation**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestListFilesJSON -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat(server): handler skeleton, static serving, list files JSON"
```

---

## Task 6: server — upload endpoint (no library call)

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestUploadRejectsNonPDF(t *testing.T) {
	srv, _ := newTestServer(t)
	body, ct := multipartPDF(t, "x.txt", []byte("hello not pdf"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
}

func TestUploadAcceptsPDF(t *testing.T) {
	srv, s := newTestServer(t)
	body, ct := multipartPDF(t, "ok.pdf", []byte("%PDF-1.7 minimal"))
	req := httptest.NewRequest(http.MethodPost, "/api/upload", body)
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d body=%s", w.Code, w.Body.String())
	}
	var resp struct{ ID string }
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("resp = %s", w.Body.String())
	}
	m, err := s.ReadMeta(resp.ID)
	if err != nil || m.Status != storage.StatusUploaded {
		t.Fatalf("meta = %+v err=%v", m, err)
	}
}
```

Add this helper and imports (`bytes`, `mime/multipart`) to the test file:

```go
func multipartPDF(t *testing.T, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, err := mw.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(data); err != nil {
		t.Fatal(err)
	}
	mw.Close()
	return &buf, mw.FormDataContentType()
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestUpload -v`
Expected: FAIL — 404 (route not registered)

- [ ] **Step 3: Write minimal implementation**

Register the route in `New` after the list route:

```go
	srv.mux.HandleFunc("POST /api/upload", srv.handleUpload)
```

Add to `server.go` (imports: `io`, `strings`):

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestUpload -v`
Expected: PASS (both)

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat(server): upload endpoint (magic-byte check, no library call)"
```

---

## Task 7: server — render trigger, status, delete, png, original

**Files:**
- Modify: `internal/server/server.go`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestMetaStatusAndDelete(t *testing.T) {
	srv, s := newTestServer(t)
	id, _ := s.CreateUpload("a.pdf", []byte("%PDF-1.7 x"))

	// status endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/files/"+id, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status code = %d", w.Code)
	}
	var m storage.Meta
	if err := json.Unmarshal(w.Body.Bytes(), &m); err != nil || m.ID != id {
		t.Fatalf("meta body = %s", w.Body.String())
	}

	// original download
	req = httptest.NewRequest(http.MethodGet, "/files/"+id+"/original.pdf", nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.Len() == 0 {
		t.Fatalf("original code=%d len=%d", w.Code, w.Body.Len())
	}

	// delete
	req = httptest.NewRequest(http.MethodDelete, "/api/files/"+id, nil)
	w = httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Fatalf("delete code = %d", w.Code)
	}
	if _, err := s.ReadMeta(id); err == nil {
		t.Fatal("expected file gone")
	}
}

func TestUnknownFile404(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/files/doesnotexist", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("code = %d", w.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run "TestMetaStatusAndDelete|TestUnknownFile404" -v`
Expected: FAIL — 404 on routes not yet registered / wrong codes

- [ ] **Step 3: Write minimal implementation**

Register routes in `New`:

```go
	srv.mux.HandleFunc("GET /api/files/{id}", srv.handleFileMeta)
	srv.mux.HandleFunc("POST /api/files/{id}/render", srv.handleRender)
	srv.mux.HandleFunc("DELETE /api/files/{id}", srv.handleDelete)
	srv.mux.HandleFunc("GET /files/{id}/original.pdf", srv.handleOriginal)
	srv.mux.HandleFunc("GET /files/{id}/pages/{n}.png", srv.handlePagePNG)
```

Add handlers (imports: `os`, `strconv`):

```go
func (s *Server) handleFileMeta(w http.ResponseWriter, r *http.Request) {
	m, err := s.store.ReadMeta(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *Server) handleRender(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
	m, err := s.store.ReadMeta(id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+m.OriginalName+"\"")
	http.ServeFile(w, r, s.store.OriginalPath(id))
}

func (s *Server) handlePagePNG(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
```

Note: the PNG route pattern `{n}.png` — `r.PathValue("n")` yields the numeric part because the literal `.png` suffix is matched by the pattern.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -v`
Expected: PASS (all server tests)

- [ ] **Step 5: Commit**

```bash
git add internal/server/
git commit -m "feat(server): meta/status, render trigger, delete, original, page PNG"
```

---

## Task 8: server — HTML pages for home and viewer

**Files:**
- Modify: `internal/server/server.go`
- Create: `web/index.html`, `web/view.html`
- Test: `internal/server/server_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHomeAndViewServeHTML(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/", "/view/anything"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s code = %d", path, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct == "" {
			t.Fatalf("%s missing content-type", path)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestHomeAndViewServeHTML -v`
Expected: FAIL — 404

- [ ] **Step 3: Write minimal implementation**

Add fields to `Server` and set them in `New`:

```go
type Server struct {
	store    *storage.Store
	renderer *renderer.Renderer
	mux      *http.ServeMux
	webDir   string
}
```

In `New`, set `webDir: webDir` in the struct literal and register:

```go
	srv.mux.HandleFunc("GET /{$}", srv.handleHome)   // exact "/" only
	srv.mux.HandleFunc("GET /view/{id}", srv.handleView)
```

Handlers:

```go
func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.webDir+"/index.html")
}

func (s *Server) handleView(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, s.webDir+"/view.html")
}
```

Create `web/index.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>aspose-pdf-foss-for-go — PDF demo</title>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body>
  <header>
    <h1>PDF demo</h1>
    <p>Powered by <code>aspose-pdf-foss-for-go</code>. Upload a PDF, then open it to render every page to PNG.</p>
  </header>

  <section class="uploader">
    <form id="upload-form">
      <input type="file" id="file" name="file" accept="application/pdf" required>
      <button type="submit">Upload</button>
    </form>
    <p id="upload-msg" class="msg"></p>
  </section>

  <section>
    <h2>Files</h2>
    <ul id="file-list" class="file-list"></ul>
  </section>

  <script src="/static/app.js"></script>
  <script>initHome();</script>
</body>
</html>
```

Create `web/view.html`:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>Viewer — PDF demo</title>
  <link rel="stylesheet" href="/static/app.css">
</head>
<body class="viewer-body">
  <header class="viewer-header">
    <a href="/">&larr; Back</a>
    <span id="title"></span>
    <a id="download" href="#">Download original PDF</a>
  </header>

  <div id="status" class="status"></div>
  <div id="pages" class="pages"></div>

  <script src="/static/app.js"></script>
  <script>initViewer();</script>
</body>
</html>
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -v`
Expected: PASS (all)

- [ ] **Step 5: Commit**

```bash
git add internal/server/server.go web/index.html web/view.html
git commit -m "feat(server): serve home and viewer HTML pages"
```

---

## Task 9: frontend JS + CSS

**Files:**
- Create: `web/static/app.js`, `web/static/app.css`

No Go test (frontend is verified manually in Docker). This task is implementation + a manual smoke check.

- [ ] **Step 1: Write `web/static/app.js`**

```js
// Plain JS, no framework. Talks to the Go JSON/PNG API.

async function initHome() {
  const form = document.getElementById('upload-form');
  const msg = document.getElementById('upload-msg');
  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const input = document.getElementById('file');
    if (!input.files.length) return;
    const fd = new FormData();
    fd.append('file', input.files[0]);
    msg.textContent = 'Uploading…';
    msg.className = 'msg';
    const res = await fetch('/api/upload', { method: 'POST', body: fd });
    if (!res.ok) {
      msg.textContent = 'Upload failed: ' + (await res.text());
      msg.className = 'msg error';
      return;
    }
    msg.textContent = 'Uploaded.';
    input.value = '';
    await loadFiles();
  });
  await loadFiles();
}

async function loadFiles() {
  const list = document.getElementById('file-list');
  const res = await fetch('/api/files');
  const files = await res.json();
  list.innerHTML = '';
  if (!files.length) {
    list.innerHTML = '<li class="empty">No files yet.</li>';
    return;
  }
  for (const f of files) {
    const li = document.createElement('li');
    const pages = f.pages ? ` · ${f.pages} pages` : '';
    li.innerHTML =
      `<a href="/view/${f.id}">${escapeHtml(f.originalName)}</a>` +
      `<span class="badge ${f.status}">${f.status}</span>` +
      `<span class="meta">${(f.size/1024).toFixed(0)} KB${pages}</span>` +
      `<button data-id="${f.id}" class="del">Delete</button>`;
    li.querySelector('.del').addEventListener('click', async () => {
      await fetch('/api/files/' + f.id, { method: 'DELETE' });
      await loadFiles();
    });
    list.appendChild(li);
  }
}

function fileIdFromPath() {
  const m = location.pathname.match(/\/view\/([^/]+)/);
  return m ? m[1] : '';
}

async function initViewer() {
  const id = fileIdFromPath();
  document.getElementById('download').href = `/files/${id}/original.pdf`;
  const statusEl = document.getElementById('status');

  let meta = await (await fetch('/api/files/' + id)).json().catch(() => null);
  if (!meta || !meta.id) { statusEl.textContent = 'File not found.'; return; }
  document.getElementById('title').textContent = meta.originalName;

  if (meta.status === 'uploaded') {
    statusEl.textContent = 'Starting render…';
    fetch('/api/files/' + id + '/render', { method: 'POST' }); // fire and poll
    meta = await pollUntilDone(id, statusEl);
  } else if (meta.status === 'rendering') {
    meta = await pollUntilDone(id, statusEl);
  }

  if (meta.status === 'error') { showError(statusEl, meta); return; }
  if (meta.status === 'ready') { statusEl.textContent = ''; renderPages(id, meta.pages); }
}

async function pollUntilDone(id, statusEl) {
  for (;;) {
    await new Promise(r => setTimeout(r, 700));
    const m = await (await fetch('/api/files/' + id)).json();
    if (m.status === 'rendering' || m.status === 'uploaded') {
      statusEl.textContent = m.pages
        ? `Rendering… (${m.pages} pages)` : 'Rendering…';
      continue;
    }
    return m; // ready or error
  }
}

function renderPages(id, pages) {
  const wrap = document.getElementById('pages');
  for (let n = 1; n <= pages; n++) {
    const img = document.createElement('img');
    img.loading = 'lazy';
    img.className = 'page';
    img.alt = 'Page ' + n;
    img.src = `/files/${id}/pages/${String(n).padStart(4, '0')}.png`;
    wrap.appendChild(img);
  }
}

function showError(statusEl, meta) {
  const e = meta.error || {};
  statusEl.className = 'status error';
  statusEl.innerHTML =
    `<h2>Render failed</h2>` +
    `<p><b>Stage:</b> ${escapeHtml(e.stage || '?')}` +
    (e.page ? ` · <b>Page:</b> ${e.page}` : '') + `</p>` +
    `<pre>${escapeHtml(e.message || '(no message)')}</pre>` +
    `<p><a href="/files/${meta.id}/original.pdf">Download original PDF</a> to reproduce.</p>`;
}

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c =>
    ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
}
```

- [ ] **Step 2: Write `web/static/app.css`**

```css
:root { font-family: system-ui, sans-serif; color: #1a1a1a; }
body { margin: 0; }
header { padding: 1rem 1.5rem; border-bottom: 1px solid #e2e2e2; }
header h1 { margin: 0 0 .25rem; font-size: 1.3rem; }
header p { margin: 0; color: #555; font-size: .9rem; }
section { padding: 1rem 1.5rem; }
.uploader form { display: flex; gap: .5rem; align-items: center; }
button { cursor: pointer; padding: .4rem .8rem; border: 1px solid #bbb; border-radius: 6px; background: #f6f6f6; }
button:hover { background: #ececec; }
.msg { font-size: .9rem; color: #2a7; min-height: 1.2em; }
.msg.error { color: #c33; white-space: pre-wrap; }
.file-list { list-style: none; padding: 0; margin: 0; }
.file-list li { display: flex; gap: .75rem; align-items: center; padding: .5rem 0; border-bottom: 1px solid #eee; }
.file-list li.empty { color: #888; border: 0; }
.file-list a { font-weight: 600; text-decoration: none; color: #0366d6; }
.file-list .meta { color: #777; font-size: .85rem; }
.file-list .del { margin-left: auto; }
.badge { font-size: .72rem; padding: .1rem .45rem; border-radius: 999px; background: #eee; text-transform: uppercase; letter-spacing: .03em; }
.badge.ready { background: #d6f5e0; color: #1a7f4b; }
.badge.error { background: #fbdcdc; color: #c0392b; }
.badge.rendering, .badge.uploaded { background: #fdf0d0; color: #9a6a00; }

.viewer-body { background: #525659; }
.viewer-header { position: sticky; top: 0; display: flex; gap: 1rem; align-items: center; padding: .6rem 1rem; background: #333; color: #eee; }
.viewer-header a { color: #9cd; text-decoration: none; }
.viewer-header #title { font-weight: 600; }
.viewer-header #download { margin-left: auto; }
.status { color: #eee; padding: 1rem 1.5rem; }
.status.error { background: #fff; color: #1a1a1a; margin: 1rem; border-radius: 8px; }
.status.error pre { white-space: pre-wrap; background: #f6f6f6; padding: .75rem; border-radius: 6px; overflow: auto; }
.pages { display: flex; flex-direction: column; align-items: center; gap: 1rem; padding: 1rem; }
.page { max-width: 900px; width: 100%; height: auto; background: #fff; box-shadow: 0 1px 6px rgba(0,0,0,.4); }
```

- [ ] **Step 3: Build to ensure nothing broke**

Run: `go build ./...`
Expected: no output (success)

- [ ] **Step 4: Commit**

```bash
git add web/static/app.js web/static/app.css
git commit -m "feat(web): home + viewer frontend (upload, list, poll, lazy viewer)"
```

---

## Task 10: main.go entrypoint

**Files:**
- Create: `main.go`

- [ ] **Step 1: Write `main.go`**

```go
package main

import (
	"log"
	"net/http"
	"os"

	"pdf-foss-demo/internal/renderer"
	"pdf-foss-demo/internal/server"
	"pdf-foss-demo/internal/storage"
)

func main() {
	dataDir := getenv("DATA_DIR", "/data")
	webDir := getenv("WEB_DIR", "web")
	addr := getenv("ADDR", ":8080")

	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatalf("cannot create data dir %q: %v", dataDir, err)
	}

	store := storage.New(dataDir)
	rnd := renderer.New(store)
	srv := server.New(store, rnd, webDir)

	log.Printf("listening on %s (data=%s web=%s)", addr, dataDir, webDir)
	log.Fatal(http.ListenAndServe(addr, srv))
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 2: Build and run the full test suite**

Run: `go build ./... && go test ./...`
Expected: build succeeds; all tests PASS

- [ ] **Step 3: Manual smoke test (local)**

Run: `WEB_DIR=web DATA_DIR=./_data ADDR=:8080 go run .` (PowerShell: `$env:WEB_DIR='web'; $env:DATA_DIR='./_data'; $env:ADDR=':8080'; go run .`)
Then open http://localhost:8080, upload a small PDF, open it, confirm pages render and scroll.
Expected: pages appear; deleting works; an invalid PDF shows a full error.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: main entrypoint wiring storage, renderer, server"
```

---

## Task 11: Docker + ignore files

**Files:**
- Create: `Dockerfile`, `.dockerignore`, `.gitignore`

- [ ] **Step 1: Write `.gitignore`**

```gitignore
# Local data volume contents and build output
/_data/
/pdf-foss-demo
/pdf-foss-demo.exe
*.test
*.out
.DS_Store
```

- [ ] **Step 2: Write `.dockerignore`**

```dockerignore
.git
_data
docs
*.md
*.test
*.out
Dockerfile
.dockerignore
.gitignore
```

- [ ] **Step 3: Write `Dockerfile`**

```dockerfile
# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY web /app/web
ENV DATA_DIR=/data WEB_DIR=/app/web ADDR=:8080
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]
```

- [ ] **Step 4: Build and run the container**

Run:
```bash
docker build -t pdf-foss-demo .
docker run --rm -p 8080:8080 -v pdf_demo_data:/data pdf-foss-demo
```
Expected: container starts, logs `listening on :8080`. Open http://localhost:8080, upload a PDF, render, scroll. Stop with Ctrl-C; re-run and confirm the file list persists (volume).

- [ ] **Step 5: Commit**

```bash
git add Dockerfile .dockerignore .gitignore
git commit -m "build: distroless multi-stage Docker image + ignore files"
```

---

## Task 12: docs + CLAUDE.md run instructions

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Add a "Run / Build" section to `CLAUDE.md`**

```markdown
## Run / build

- Local: `go run .` (env: `DATA_DIR`, `WEB_DIR`, `ADDR`; defaults `/data`, `web`, `:8080`).
- Tests: `go test ./...`.
- Docker: `docker build -t pdf-foss-demo .` then
  `docker run --rm -p 8080:8080 -v pdf_demo_data:/data pdf-foss-demo`.

## Layout

- `internal/storage` — volume layout + meta.json IO (no PDF library).
- `internal/renderer` — the ONLY package that opens PDFs with the library.
- `internal/server` — net/http handlers + static serving.
- `web/` — plain HTML/CSS/JS frontend (no build step).
```

- [ ] **Step 2: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: run/build instructions and layout in CLAUDE.md"
```

---

## Self-review notes (addressed)

- **Spec coverage:** storage layout/meta (T1–2), upload-without-library (T6), on-demand render with semaphore+recover (T3), error preserves original (T4), all HTTP endpoints incl. status/render/delete/png/original (T5–7), home+viewer HTML (T8), lazy scrolling viewer + poll UI + full error display (T9), main (T10), Docker + ignore files (T11), docs (T12). DPI=150 constant in renderer. One-render-at-a-time semaphore in T3.
- **No library call on upload:** enforced in T6 (magic-byte check only).
- **Type consistency:** `storage.Meta`, `storage.RenderError{Stage,Page,Message}`, `storage.Status*` constants, `renderer.New(*storage.Store)`, `renderer.Render(id)`, `server.New(store, renderer, webDir)` used consistently across tasks.
- **1-based pages:** `doc.Page(n)` for `n := 1..PageCount` and `%04d.png` naming match the `PagePNGPath` and frontend `padStart(4,'0')`.

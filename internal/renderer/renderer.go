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

// maxRenderPages caps how many pages are rasterized per document. The full page
// count is still recorded in meta.Pages; only the first maxRenderPages are
// rendered to PNG (meta.RenderedPages), and the viewer notes the remainder.
const maxRenderPages = 10

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
	if m.Status == storage.StatusReady || m.Status == storage.StatusRendering {
		return nil
	}
	// A previous StatusError is intentionally NOT a skip-condition: re-triggering
	// a render on a file that failed before is useful for reproducing library bugs.

	r.sem <- struct{}{}
	defer func() { <-r.sem }()

	// Re-read after acquiring the semaphore in case another request rendered it.
	if m, err = r.store.ReadMeta(id); err != nil {
		return err
	}
	if m.Status == storage.StatusReady || m.Status == storage.StatusRendering {
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
	// stage tracks the current phase so a recovered panic is attributed to the
	// library call that actually faulted (parse vs render), not always "render".
	stage := "parse"
	panicPage := 0
	defer func() {
		if rec := recover(); rec != nil {
			rerr = &storage.RenderError{
				Stage:   stage,
				Page:    panicPage,
				Message: fmt.Sprintf("panic: %v\n%s", rec, debug.Stack()),
			}
		}
	}()

	doc, err := asposepdf.Open(r.store.OriginalPath(id))
	if err != nil {
		return &storage.RenderError{Stage: "parse", Message: err.Error()}
	}
	stage = "render"

	pages := doc.PageCount()
	m.Pages = pages

	// Cap rasterization to the first maxRenderPages; record how many we render.
	toRender := pages
	if toRender > maxRenderPages {
		toRender = maxRenderPages
	}
	m.RenderedPages = toRender

	if err := os.MkdirAll(r.store.PagesDir(id), 0o755); err != nil {
		return &storage.RenderError{Stage: "write", Message: err.Error()}
	}

	for n := 1; n <= toRender; n++ {
		panicPage = n // so a panic inside the loop is attributed to this page
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

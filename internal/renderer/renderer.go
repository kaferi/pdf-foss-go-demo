// Package renderer is the ONLY place that opens PDFs with aspose-pdf-foss-for-go.
// It renders every page of a file to PNG, one file at a time (memory bound), and
// converts any error or panic into a persisted error status — never a crash.
package renderer

import (
	"errors"
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

// testPasswords are tried in order when a PDF is encrypted. This is a demo/test
// harness, so we attempt a small fixed set rather than asking the user. The
// empty string is first: it also covers files encrypted with only an owner
// password (empty user password).
var testPasswords = []string{"", "testpassword", "password", "pass"}

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

// Rerender forces a fresh render even if the file is already "ready": it deletes
// the cached PNGs, clears the rendered state in meta, then renders again. This is
// for re-checking a file against a new library build while fixing bugs. Like
// Render, PDF failures are reported via meta status, not the return value.
func (r *Renderer) Rerender(id string) error {
	m, err := r.store.ReadMeta(id)
	if err != nil {
		return err
	}
	// Drop cached PNGs so a now-shorter render can't leave stale pages behind.
	if err := r.store.RemovePages(id); err != nil {
		return err
	}
	// Reset every render-derived field back to the pre-render state.
	m.Status = storage.StatusUploaded
	m.Pages = 0
	m.RenderedPages = 0
	m.RenderedAt = ""
	m.Encrypted = false
	m.UnlockedWith = ""
	m.Error = nil
	if err := r.store.WriteMeta(m); err != nil {
		return err
	}
	// Render re-acquires the semaphore and proceeds because status is "uploaded".
	return r.Render(id)
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

	doc, encrypted, unlockedWith, err := openDocument(r.store.OriginalPath(id))
	if err != nil {
		return &storage.RenderError{Stage: "parse", Message: err.Error()}
	}
	m.Encrypted = encrypted
	m.UnlockedWith = unlockedWith
	stage = "render"

	pages := doc.PageCount()
	m.Pages = pages

	// A document that opens but reports zero pages is treated as an error rather
	// than a silently-empty "ready". For an encrypted file this typically means
	// the library accepted a wrong/empty password and returned an empty document
	// instead of failing — exactly the kind of bug this harness exists to surface.
	if zerr := zeroPageError(pages, encrypted); zerr != nil {
		return zerr
	}

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

// zeroPageError returns a parse-stage RenderError when a document opened but
// reports no pages, and nil otherwise. Kept separate so the decision is unit
// testable without forging a (disallowed) zero-page PDF.
func zeroPageError(pages int, encrypted bool) *storage.RenderError {
	if pages > 0 {
		return nil
	}
	msg := "document opened but reports 0 pages"
	if encrypted {
		msg += " (encrypted: a test password may have been wrongly accepted)"
	}
	return &storage.RenderError{Stage: "parse", Message: msg}
}

// openDocument opens the PDF, transparently handling encryption. If the plain
// Open reports the file is encrypted, it retries with each testPasswords entry
// in order and uses the first that succeeds. Returns the document, whether it
// was encrypted, and which test password unlocked it ("" for the empty password
// or for non-encrypted files).
func openDocument(path string) (doc *asposepdf.Document, encrypted bool, unlockedWith string, err error) {
	doc, err = asposepdf.Open(path)
	if err == nil {
		return doc, false, "", nil
	}
	if !errors.Is(err, asposepdf.ErrEncrypted) {
		return nil, false, "", err
	}
	// Encrypted: try the known test passwords.
	for _, pw := range testPasswords {
		d, perr := asposepdf.OpenWithPassword(path, pw)
		if perr == nil {
			return d, true, pw, nil
		}
	}
	return nil, true, "", fmt.Errorf("PDF is encrypted; none of the %d test passwords matched", len(testPasswords))
}

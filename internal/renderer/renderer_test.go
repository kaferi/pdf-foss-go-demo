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
	p, err := doc.Page(1)
	if err != nil {
		t.Fatal(err)
	}
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

// makeEncryptedPDF builds a real 1-page PDF encrypted with the given user
// password and returns its bytes.
func makeEncryptedPDF(t *testing.T, userPassword string) []byte {
	t.Helper()
	doc := asposepdf.NewDocumentFromFormat(asposepdf.PageFormatA4)
	doc.SetPassword(userPassword, "owner-"+userPassword)
	tmp := t.TempDir() + "/enc.pdf"
	if err := doc.Save(tmp); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRenderEncryptedUnlocksWithTestPassword(t *testing.T) {
	s := storage.New(t.TempDir())
	// "testpassword" is in the test-password set, so the renderer should open it.
	id, err := s.CreateUpload("locked.pdf", makeEncryptedPDF(t, "testpassword"))
	if err != nil {
		t.Fatal(err)
	}
	if err := New(s).Render(id); err != nil {
		t.Fatalf("Render: %v", err)
	}
	m, _ := s.ReadMeta(id)
	if m.Status != storage.StatusReady {
		t.Fatalf("status = %q, err=%+v", m.Status, m.Error)
	}
	if !m.Encrypted {
		t.Fatalf("Encrypted = false, want true")
	}
	if m.UnlockedWith != "testpassword" {
		t.Fatalf("UnlockedWith = %q, want %q", m.UnlockedWith, "testpassword")
	}
	if _, err := os.Stat(s.PagePNGPath(id, 1)); err != nil {
		t.Fatalf("missing page 1 png: %v", err)
	}
}

func TestRenderEncryptedUnknownPasswordRecordsError(t *testing.T) {
	s := storage.New(t.TempDir())
	// A password NOT in the test set: render must fail cleanly, original kept.
	id, err := s.CreateUpload("locked.pdf", makeEncryptedPDF(t, "not-in-the-set-xyz"))
	if err != nil {
		t.Fatal(err)
	}
	if err := New(s).Render(id); err != nil {
		t.Fatalf("Render returned storage error: %v", err)
	}
	m, _ := s.ReadMeta(id)
	if m.Status != storage.StatusError {
		t.Fatalf("status = %q, want error", m.Status)
	}
	if m.Error == nil || m.Error.Stage != "parse" {
		t.Fatalf("expected parse-stage error, got %+v", m.Error)
	}
	if _, err := os.Stat(s.OriginalPath(id)); err != nil {
		t.Fatalf("original was removed: %v", err)
	}
}

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

// makePDFPages builds a real n-page PDF and returns its bytes.
func makePDFPages(t *testing.T, n int) []byte {
	t.Helper()
	doc := asposepdf.NewDocumentFromFormat(asposepdf.PageFormatA4)
	for i := 2; i <= n; i++ {
		if err := doc.AddBlankPageFromFormat(asposepdf.PageFormatA4); err != nil {
			t.Fatal(err)
		}
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

func TestRenderCapsAtMaxPages(t *testing.T) {
	s := storage.New(t.TempDir())
	id, err := s.CreateUpload("big.pdf", makePDFPages(t, 13))
	if err != nil {
		t.Fatal(err)
	}
	if err := New(s).Render(id); err != nil {
		t.Fatalf("Render: %v", err)
	}
	m, _ := s.ReadMeta(id)
	if m.Status != storage.StatusReady {
		t.Fatalf("status = %q, err=%+v", m.Status, m.Error)
	}
	// Full page count is recorded, but only the first maxRenderPages are rendered.
	if m.Pages != 13 {
		t.Fatalf("pages = %d, want 13", m.Pages)
	}
	if m.RenderedPages != maxRenderPages {
		t.Fatalf("renderedPages = %d, want %d", m.RenderedPages, maxRenderPages)
	}
	// PNGs 1..10 exist; 11 does not.
	if _, err := os.Stat(s.PagePNGPath(id, maxRenderPages)); err != nil {
		t.Fatalf("missing png %d: %v", maxRenderPages, err)
	}
	if _, err := os.Stat(s.PagePNGPath(id, maxRenderPages+1)); err == nil {
		t.Fatalf("png %d should not exist (over cap)", maxRenderPages+1)
	}
}

func TestRenderSmallSetsRenderedPagesToTotal(t *testing.T) {
	s := storage.New(t.TempDir())
	id, _ := s.CreateUpload("small.pdf", makePDFPages(t, 3))
	if err := New(s).Render(id); err != nil {
		t.Fatalf("Render: %v", err)
	}
	m, _ := s.ReadMeta(id)
	if m.Pages != 3 || m.RenderedPages != 3 {
		t.Fatalf("pages=%d renderedPages=%d, want 3/3", m.Pages, m.RenderedPages)
	}
}

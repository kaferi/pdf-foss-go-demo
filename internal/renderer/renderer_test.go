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

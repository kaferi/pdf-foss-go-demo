package storage

import (
	"os"
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

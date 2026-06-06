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

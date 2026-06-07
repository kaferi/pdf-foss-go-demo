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

// writeMetaDir creates a file dir with just a meta.json (no original.pdf needed
// for List, which only scans meta).
func writeMetaDir(t *testing.T, s *Store, m Meta) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(s.MetaPath(m.ID)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.WriteMeta(m); err != nil {
		t.Fatal(err)
	}
}

// TestListDeterministicOrderOnTimestampCollision verifies that files sharing the
// same UploadedAt come back in a stable, ID-ordered sequence, and that deleting
// one does not reshuffle the others (the UI-ordering bug).
func TestListDeterministicOrderOnTimestampCollision(t *testing.T) {
	s := New(t.TempDir())
	const ts = "2026-06-07T10:56:29Z"
	// Same timestamp, intentionally out-of-order IDs.
	for _, id := range []string{"ccc", "aaa", "bbb", "ddd"} {
		writeMetaDir(t, s, Meta{ID: id, OriginalName: id + ".pdf", UploadedAt: ts, Status: StatusUploaded})
	}

	ids := func() []string {
		all, err := s.List()
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, len(all))
		for i, m := range all {
			out[i] = m.ID
		}
		return out
	}

	// Equal timestamps → ascending ID is the deterministic tiebreaker.
	want := []string{"aaa", "bbb", "ccc", "ddd"}
	got := ids()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}

	// Calling List again returns the SAME order (stability).
	if got2 := ids(); !equalStrings(got, got2) {
		t.Fatalf("List not stable across calls: %v vs %v", got, got2)
	}

	// Deleting a middle file must not reorder the survivors.
	if err := s.Delete("bbb"); err != nil {
		t.Fatal(err)
	}
	if got3 := ids(); !equalStrings(got3, []string{"aaa", "ccc", "ddd"}) {
		t.Fatalf("order after delete = %v, want [aaa ccc ddd]", got3)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

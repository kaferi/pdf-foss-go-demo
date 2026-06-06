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

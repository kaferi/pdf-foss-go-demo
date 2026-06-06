package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestPagePNGMissing404(t *testing.T) {
	srv, s := newTestServer(t)
	id, _ := s.CreateUpload("a.pdf", []byte("%PDF-1.7 x"))
	req := httptest.NewRequest(http.MethodGet, "/files/"+id+"/pages/1", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("code=%d", w.Code)
	}
}

func TestHomeAndViewServeHTML(t *testing.T) {
	srv, _ := newTestServer(t)
	for _, path := range []string{"/", "/view/anything"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("%s code = %d", path, w.Code)
		}
		if !strings.Contains(w.Body.String(), "<html") {
			t.Fatalf("%s did not serve HTML: %s", path, w.Body.String())
		}
	}
}

// TestRenderUnknown404 covers the render trigger on a well-formed but
// nonexistent id (completing endpoint coverage parity).
func TestRenderUnknown404(t *testing.T) {
	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/files/"+strings.Repeat("a", 32)+"/render", nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("code = %d", w.Code)
	}
}

// TestPathTraversalRejected verifies that a crafted id containing an
// (URL-encoded) path separator cannot escape the data volume. The DELETE path
// is the dangerous one: without the validID guard it would remove an arbitrary
// directory. We assert the request is rejected and the sibling dir survives.
func TestPathTraversalRejected(t *testing.T) {
	srv, s := newTestServer(t)
	victim, _ := s.CreateUpload("victim.pdf", []byte("%PDF-1.7 keep me"))

	// "..%2F<victim>" decodes to "../<victim>"; validID must reject it.
	req := httptest.NewRequest(http.MethodDelete, "/api/files/..%2F"+victim, nil)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("traversal delete code = %d, want 404", w.Code)
	}
	if _, err := s.ReadMeta(victim); err != nil {
		t.Fatalf("victim dir was destroyed by traversal: %v", err)
	}
}

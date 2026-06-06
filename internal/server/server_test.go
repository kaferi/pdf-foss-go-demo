package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
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

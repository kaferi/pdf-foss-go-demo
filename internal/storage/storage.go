// Package storage owns the on-disk layout of the /data volume. It performs no
// PDF work and imports no PDF library.
package storage

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Status values for a file's meta.json.
const (
	StatusUploaded  = "uploaded"
	StatusRendering = "rendering"
	StatusReady     = "ready"
	StatusError     = "error"
)

// RenderError captures a failure during parse/render so it survives in meta.json.
type RenderError struct {
	Stage   string `json:"stage"`          // "parse" | "render" | "write"
	Page    int    `json:"page,omitempty"` // 1-based page, when applicable
	Message string `json:"message"`        // full library error or panic + stack
}

// Meta is the persisted record for one uploaded file.
type Meta struct {
	ID            string       `json:"id"`
	OriginalName  string       `json:"originalName"`
	Size          int64        `json:"size"`
	UploadedAt    string       `json:"uploadedAt"`
	Status        string       `json:"status"`
	Pages         int          `json:"pages,omitempty"`         // total pages in the document
	RenderedPages int          `json:"renderedPages,omitempty"` // pages actually rendered to PNG (capped)
	RenderedAt    string       `json:"renderedAt,omitempty"`
	Encrypted     bool         `json:"encrypted,omitempty"`    // the PDF was password-protected
	UnlockedWith  string       `json:"unlockedWith,omitempty"` // the test password that opened it ("" = empty password)
	Error         *RenderError `json:"error,omitempty"`
}

// Store is rooted at the volume mount (e.g. /data).
type Store struct{ Root string }

// New returns a Store rooted at the given directory (typically the /data volume mount).
func New(root string) *Store { return &Store{Root: root} }

func (s *Store) dir(id string) string { return filepath.Join(s.Root, id) }

// OriginalPath returns the path of the uploaded PDF for the given file ID.
func (s *Store) OriginalPath(id string) string { return filepath.Join(s.dir(id), "original.pdf") }

// MetaPath returns the path of the meta.json record for the given file ID.
func (s *Store) MetaPath(id string) string { return filepath.Join(s.dir(id), "meta.json") }

// PagesDir returns the directory holding the rendered page PNGs for the given file ID.
func (s *Store) PagesDir(id string) string { return filepath.Join(s.dir(id), "pages") }

// PagePNGPath returns the path of the rendered PNG for 1-based page n of the given file ID.
func (s *Store) PagePNGPath(id string, n int) string {
	return filepath.Join(s.PagesDir(id), fmt.Sprintf("%04d.png", n))
}

// newID returns a filesystem-safe random id.
func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// CreateUpload writes original.pdf FIRST, then a meta.json with status "uploaded".
// It never opens the PDF with any library.
func (s *Store) CreateUpload(originalName string, data []byte) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(s.dir(id), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(s.OriginalPath(id), data, 0o644); err != nil {
		return "", err
	}
	m := Meta{
		ID:           id,
		OriginalName: originalName,
		Size:         int64(len(data)),
		UploadedAt:   time.Now().UTC().Format(time.RFC3339),
		Status:       StatusUploaded,
	}
	if err := s.WriteMeta(m); err != nil {
		return "", err
	}
	return id, nil
}

// WriteMeta persists m to its meta.json (overwriting any existing record).
func (s *Store) WriteMeta(m Meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.MetaPath(m.ID), b, 0o644)
}

// ReadMeta loads the meta.json for the given file ID.
func (s *Store) ReadMeta(id string) (Meta, error) {
	var m Meta
	b, err := os.ReadFile(s.MetaPath(id))
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

// List scans /data/*/meta.json, newest upload first. Directories without a
// readable meta.json are skipped.
func (s *Store) List() ([]Meta, error) {
	entries, err := os.ReadDir(s.Root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.ReadMeta(e.Name())
		if err != nil {
			continue // skip incomplete dirs
		}
		out = append(out, m)
	}
	// Newest upload first, with ID as a deterministic tiebreaker. Many files can
	// share the same UploadedAt (a multi-file upload stamps them within the same
	// second), so without the tiebreaker their relative order would be undefined
	// and could shuffle when an unrelated file is deleted.
	sort.Slice(out, func(i, j int) bool {
		if out[i].UploadedAt != out[j].UploadedAt {
			return out[i].UploadedAt > out[j].UploadedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Delete removes the entire directory for the given file ID.
func (s *Store) Delete(id string) error { return os.RemoveAll(s.dir(id)) }

// RemovePages deletes the cached page PNGs for the given file ID (the original
// and meta.json are kept). Used before a forced re-render.
func (s *Store) RemovePages(id string) error { return os.RemoveAll(s.PagesDir(id)) }

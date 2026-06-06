// Package storage owns the on-disk layout of the /data volume. It performs no
// PDF work and imports no PDF library.
package storage

import (
	"fmt"
	"path/filepath"
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
	Stage   string `json:"stage"`             // "parse" | "render" | "write"
	Page    int    `json:"page,omitempty"`    // 1-based page, when applicable
	Message string `json:"message"`           // full library error or panic + stack
}

// Meta is the persisted record for one uploaded file.
type Meta struct {
	ID           string       `json:"id"`
	OriginalName string       `json:"originalName"`
	Size         int64        `json:"size"`
	UploadedAt   string       `json:"uploadedAt"`
	Status       string       `json:"status"`
	Pages        int          `json:"pages,omitempty"`
	RenderedAt   string       `json:"renderedAt,omitempty"`
	Error        *RenderError `json:"error,omitempty"`
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

# Design: PDF Upload → Render → Viewer demo

**Date:** 2026-06-06
**Status:** Approved (pending user review of this written spec)

## Purpose

A client-server web app that demonstrates the capabilities of the
[aspose-pdf-foss-for-go](https://github.com/aspose-pdf-foss/aspose-pdf-foss-for-go)
library. It doubles as a test harness for the library: it must surface and
preserve any error or panic so library bugs can be reproduced and reported.

Initial scope: upload PDFs, render their pages to PNG on demand, and view them
in a vertically scrolling Acrobat-style viewer.

## Hard constraint

Every PDF operation — parsing, rendering pages to PNG, reading fields,
text/image extraction, merging, encryption, anything — MUST go through
`aspose-pdf-foss-for-go`. No other third-party PDF or rendering library is
allowed. Only the Go standard library plus this one dependency.

The dependency is pinned to the latest `main` branch commit (not the `v0.2.0`
tag) because raster rendering (`Page.RenderPNG` / `Page.RenderImage`) exists in
`main` but not in the tagged release.

## Key decisions

- **Storage:** single Docker volume at `/data`. Global shared pool (no
  per-user/session separation).
- **Upload does NOT open the file with the library.** Files arrive by the
  hundreds; opening them all would exhaust memory. Upload only writes
  `original.pdf` and a `meta.json` with `status: "uploaded"`.
- **Render is a separate, on-demand operation** triggered by selecting a file
  from the list. It opens the file, gets `PageCount`, renders ALL pages to PNG
  @150 DPI, and caches them on the volume.
- **One render at a time** — global semaphore = 1; only one file is open in the
  library at any moment (memory is the bottleneck).
- **Render UX:** selecting a file shows a "Rendering… (N pages)" page that polls
  status, then transitions to the viewer when `ready`.
- **Viewer:** vertical scroll; page `<img>`s use native `loading="lazy"`.
- **Frontend:** plain HTML/CSS/JS, no framework, no build step, no Node.
- **DPI:** fixed 150 (single constant; raise later if needed).
- **List + delete** on the home page.

## Storage layout (volume mounted at `/data`)

```
/data/
  <fileId>/
    original.pdf        # the uploaded bytes, written FIRST, never auto-deleted
    meta.json           # see schema below
    pages/              # created only when rendering succeeds
      0001.png          # page 1 @150 DPI
      0002.png
      ...
```

- `fileId`: random, filesystem-safe (hex from crypto/rand). The original
  filename is kept only inside `meta.json`, never used as a path.
- The file list = scanning `/data/*/meta.json`. No database.
- Delete = `os.RemoveAll(/data/<fileId>)`.

### meta.json schema

```json
{
  "id": "a1b2c3...",
  "originalName": "report.pdf",
  "size": 123456,
  "uploadedAt": "2026-06-06T12:00:00Z",
  "status": "uploaded | rendering | ready | error",
  "pages": 12,
  "renderedAt": "2026-06-06T12:01:00Z",
  "error": {
    "stage": "parse | render | write",
    "page": 7,
    "message": "<full library error text or panic + stack trace>"
  }
}
```

- `pages`, `renderedAt` present once known (after a render attempt).
- `error` present only when `status == "error"`.

### Status lifecycle

`uploaded` → `rendering` → `ready` | `error`

A `ready` file with all PNGs present is served directly; selecting it again
skips rendering. An `error` record stays in the list until manually deleted.

## HTTP API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/` | Home: upload form + file list (HTML) |
| `GET` | `/view/{fileId}` | Viewer page (HTML); if not yet `ready`, shows the rendering/poll UI |
| `POST` | `/api/upload` | multipart PDF → save original + meta(`uploaded`) → JSON `{id}` |
| `GET` | `/api/files` | JSON list of all files (from meta.json scan) |
| `POST` | `/api/files/{fileId}/render` | trigger render (idempotent; no-op if `ready`/`rendering`) |
| `GET` | `/api/files/{fileId}` | JSON meta (status polling) |
| `DELETE` | `/api/files/{fileId}` | remove `/data/<fileId>` |
| `GET` | `/files/{fileId}/pages/{n}.png` | serve cached page PNG (404 if not rendered) |
| `GET` | `/files/{fileId}/original.pdf` | download original (works for errored files) |
| `GET` | `/static/*` | CSS/JS |

### Upload flow (`POST /api/upload`)

1. Enforce max size (50 MB constant). Reject non-PDF by `%PDF-` magic bytes.
2. Generate `fileId`, create `/data/<fileId>/`.
3. Write `original.pdf` **first**.
4. Write `meta.json` with `status:"uploaded"`.
5. Return `{id}`. (No library call. No PageCount.)

### Render flow (`POST /api/files/{id}/render`)

Guarded by a global semaphore (capacity 1).

1. If already `ready` (PNGs present) → return immediately.
2. Set `status:"rendering"`, write meta.
3. In a `recover()`-wrapped section:
   - `doc, err := asposepdf.Open(.../original.pdf)` (or `OpenStream`).
   - `n := doc.PageCount()`.
   - For each page: `page.RenderPNG(out, RenderOptions{DPI:150})` → `pages/NNNN.png`.
4. On success: `status:"ready"`, set `pages`, `renderedAt`.
5. On any error/panic: `status:"error"`, set `error.{stage,page,message}` (message
   includes the panic stack trace when applicable). `original.pdf` is left intact;
   the record is NOT deleted.

### Viewer / rendering UX

- `GET /view/{id}`:
  - status `ready` → render the scrollable page with one
    `<img loading="lazy" src="/files/{id}/pages/NNNN.png">` per page. Images carry
    intrinsic width/height (from the rendered PNG dimensions) so scroll position
    is stable.
  - status `uploaded`/`rendering` → show "Rendering… (N pages)" UI; JS POSTs to
    `/render` (if `uploaded`) and polls `GET /api/files/{id}` until `ready` or
    `error`, then reloads / shows the error.
  - status `error` → show the full error (stage, page, message) plus a "Download
    original PDF" button.

## Error handling & resilience

- `original.pdf` written before any parse/render; never auto-deleted.
- All library calls wrapped in `recover()`; panic → recorded error with stack
  trace, server stays up.
- Errors are persisted to meta and surfaced fully on screen. Errors are a
  feature here, not an edge case — this app helps catch library bugs.
- Limits/codes: size ≤ 50 MB, non-PDF → 400, unknown `fileId` → 404.

## Components (units, each independently testable)

- `storage` — volume layout: paths, read/write/list meta, delete. No HTTP, no
  library. Pure filesystem.
- `renderer` — given a fileId, open with the library and render all pages to
  PNG; the only place that imports the library for rendering; owns the
  semaphore and `recover()` wrapping.
- `server` — `net/http` handlers wiring storage + renderer, plus static serving.
- `web/` (static) — `index.html`, `view.html`, CSS, JS. No build step.

## Testing

- **storage (unit):** layout, meta round-trip, list, delete on a temp dir.
- **renderer (unit):** build a small valid PDF with the library itself
  (`NewDocumentFromFormat` + `AddText` + `Save`), run it through render → assert
  PNGs exist and `status:"ready"`. Feed a non-PDF and a corrupt PDF → assert
  `status:"error"` and `original.pdf` still present.
- **HTTP (httptest):** upload, list, render trigger, status poll, delete, PNG
  serving, 404s.
- **Frontend:** manual verification in Docker (visual scroll viewer).

## Docker

- Multi-stage: builder (`golang:1.26`) → slim runtime (distroless or alpine)
  with the single binary + `web/` static dir.
- One volume mounted at `/data`. Port 8080 exposed.
- `.dockerignore` and `.gitignore` at repo root.

## Out of scope (YAGNI for now)

- Auth / users / sessions.
- Database.
- Editing/manipulating PDFs (merge, encrypt, forms) — future demos.
- Re-render / DPI selection UI (DPI is a constant).
- Embedding static assets via `embed` (serve from disk for now).

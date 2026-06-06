# pdf-foss-go-demo

Client-server web app demonstrating the capabilities of the
[aspose-pdf-foss-for-go](https://github.com/aspose-pdf-foss/aspose-pdf-foss-for-go)
library. Runs entirely in Docker.

## Hard rule: only aspose-pdf-foss-for-go for PDF work

Every PDF operation — parsing, rendering pages to PNG, reading fields, text/image
extraction, merging, encryption, anything — MUST go through
`github.com/aspose-pdf-foss/aspose-pdf-foss-for-go`. No other third-party PDF or
rendering library is allowed (no pdfcpu, unidoc, poppler, mupdf, pdf.js, etc.).
Only the Go standard library plus this one dependency.

Reason: this is a demo built specifically to showcase aspose-pdf-foss-for-go.

The dependency is pinned to the latest `main` branch commit (not the `v0.2.0`
tag) because raster rendering (`Page.RenderPNG` / `Page.RenderImage`) exists in
`main` but not in the tagged release.

## Decisions so far

- **Storage:** single Docker volume mounted at `/data`. Global shared pool — all
  uploaded files are visible to everyone (no per-user/session separation).
- **Upload is cheap and does NOT open the file with the library.** Upload only
  writes `original.pdf` + a `meta.json` (`status: "uploaded"`). No parsing, no
  PageCount. This is required because files are uploaded by the hundreds — we
  cannot afford to open them all in the library (memory).
- **Rendering is a separate operation triggered by selecting a file from the
  list.** On selection the server opens the file with the library, gets
  PageCount, renders the first `maxRenderPages` (= 10) pages to PNG @150 DPI,
  caches them on the volume. `meta.Pages` holds the full page count;
  `meta.RenderedPages` holds how many were actually rasterized. The viewer shows
  "Showing the first N of M pages" when capped. Re-opening a `ready` file is
  instant (PNGs already on disk).
- **Upload is multi-file.** The home form accepts multiple PDFs; the frontend
  POSTs them to `/api/upload` one at a time (the endpoint still takes a single
  file), shows per-file progress, refreshes the list as each lands, and reports
  per-file failures without aborting the batch.
- **One render at a time** (global semaphore = 1) — memory is the bottleneck;
  only one file is open in the library at any moment.
- **Render UX:** selecting a file shows a "Rendering… (N pages)" page that polls
  status, then transitions to the viewer when `ready`.
- **Viewer:** vertically scrolling, Acrobat-style. Page `<img>`s use native
  `loading="lazy"` so the browser only fetches PNGs as they scroll into view.
- **Frontend:** plain HTML/CSS/JS, no framework, no build step, no Node. Go
  serves static files + a JSON/PNG API.
- **Errors are first-class** (this app also exists to catch library bugs):
  - `original.pdf` is written FIRST, before any parse/render, and is NEVER
    deleted automatically — it is the artifact for reproducing a bug.
  - All library calls (open, PageCount, RenderPNG) are wrapped in `recover()`;
    a panic becomes a recorded error with its stack trace, never a crash.
  - Any failure writes `meta.json` with `status:"error"` + `{stage, page,
    message}`. The record stays in the list forever (until manual delete).
  - `POST /api/upload` / render endpoints return full error detail to the screen.
  - File page has a "Download original PDF" button (works for errored files too).
- **meta.json status lifecycle:** `uploaded` → `rendering` → `ready` | `error`.
- **List + delete:** home page lists all files; delete removes the whole
  `/data/<fileId>` folder.

## Conventions

- Keep this file up to date as the design and code evolve.
- `.gitignore` and `.dockerignore` are maintained in the repo root.

## Run / build

- Local: `go run .` — env vars `DATA_DIR` (default `/data`), `WEB_DIR`
  (default `web`), `ADDR` (default `:8080`). On Windows for a local run use a
  writable data dir, e.g. PowerShell:
  `$env:DATA_DIR='./_data'; $env:ADDR=':8080'; go run .`
- Tests: `go test ./...`.
- Static linux build (what Docker uses): `CGO_ENABLED=0 GOOS=linux go build .`
  — the library is pure Go, no cgo.
- Docker:
  `docker build -t pdf-foss-demo .` then
  `docker run --rm -p 8080:8080 -v pdf_demo_data:/data pdf-foss-demo`.

## Layout

- `internal/storage` — volume layout + `meta.json` IO (no PDF library).
- `internal/renderer` — the ONLY package that opens PDFs with the library;
  renders pages to PNG, serialized by a capacity-1 semaphore, recovers panics
  into recorded errors.
- `internal/server` — `net/http` handlers + static serving. `validID` guards
  every `{id}` path against traversal.
- `web/` — plain HTML/CSS/JS frontend (no build step). Page PNGs are served at
  `/files/{id}/pages/{n}` (1-based, no `.png` extension).
- `main.go` — wires storage → renderer → server.

## Specs & plans

Design specs live in `docs/superpowers/specs/`; implementation plans in
`docs/superpowers/plans/`.

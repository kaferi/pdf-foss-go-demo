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
  PageCount, renders ALL pages to PNG @150 DPI, caches them on the volume.
  Re-opening a `ready` file is instant (PNGs already on disk).
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
- Git repo is created by the user — do not run `git init`.
- `.gitignore` and `.dockerignore` are maintained in the repo root.

## Specs

Design specs live in `docs/superpowers/specs/`.

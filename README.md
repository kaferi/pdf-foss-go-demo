# pdf-foss-go-demo

A small client-server web app that demonstrates the capabilities of the
[aspose-pdf-foss-for-go](https://github.com/aspose-pdf-foss/aspose-pdf-foss-for-go)
PDF library — and doubles as a visual harness for catching rendering bugs.

Upload PDFs, render their pages to PNG on demand, and browse them in a
two-pane, Acrobat-style viewer. Every PDF operation goes through
`aspose-pdf-foss-for-go` and nothing else.

## Features

- **Upload** PDFs (multi-file). Upload is cheap: it only stores the file and
  never opens it with the library, so hundreds of files can be queued.
- **On-demand render**: selecting a file rasterizes its pages to PNG @150 DPI
  (first 10 pages) and caches them on disk. Re-opening is instant.
- **Two-pane UI**: file list on the left, scrolling page viewer on the right.
  Single-page, no reload, deep-linkable (`/view/{id}`).
- **Open original** PDF inline in a new browser tab.
- **Errors are first-class**: a parse/render failure (or a recovered panic) is
  recorded with its stage, page, and full message/stack and shown on screen —
  the original file is always kept so the bug can be reproduced.

## Run

Requires Docker.

```sh
docker compose up -d --build
```

Then open <http://localhost:8080>. Uploaded data is bind-mounted to `./data`
in the repo, so it is directly visible on the host.

Stop with `docker compose down`.

### Local (without Docker)

```sh
# Windows PowerShell
$env:DATA_DIR='./_data'; $env:WEB_DIR='web'; $env:ADDR=':8080'; go run .
```

Env vars: `DATA_DIR` (default `/data`), `WEB_DIR` (default `web`), `ADDR`
(default `:8080`).

## Tests

```sh
go test ./...
```

## How it works

| Package | Responsibility |
|---|---|
| `internal/storage` | Volume layout + `meta.json` IO. No PDF library. |
| `internal/renderer` | The only package that opens PDFs with the library; renders pages to PNG, serialized by a capacity-1 semaphore, recovers panics into recorded errors. |
| `internal/server` | `net/http` handlers + static serving. |
| `web/` | Plain HTML/CSS/JS frontend, no build step. |

The dependency is pinned to the latest `main` branch commit of the library
(not the `v0.2.0` tag) because raster rendering (`Page.RenderPNG`) exists in
`main` but not in the tagged release.

## License

MIT

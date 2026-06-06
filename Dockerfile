# syntax=docker/dockerfile:1

FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server .
# Pre-create the data dir owned by the nonroot uid (65532). The distroless
# runtime has no shell/mkdir, so we stage the directory here and COPY it with
# the right ownership. A named volume mounted on an empty image dir inherits the
# image dir's owner, so this lets the nonroot process write to /data.
RUN mkdir -p /out/data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/server /app/server
COPY web /app/web
COPY --from=build --chown=65532:65532 /out/data /data
ENV DATA_DIR=/data WEB_DIR=/app/web ADDR=:8080
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot
ENTRYPOINT ["/app/server"]

# syntax=docker/dockerfile:1.7

FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
# No external deps yet; once we add any, also COPY go.sum and `go mod download`.
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
    -o /out/recorder ./cmd/recorder

FROM alpine:3.20
# tini reaps zombie ffmpeg processes when they exit between restarts;
# ffmpeg is the actual stream worker. ca-certificates lets RTSP-over-TLS
# (rtsps://) work if ever used.
RUN apk add --no-cache ffmpeg tini ca-certificates
COPY --from=build /out/recorder /usr/local/bin/recorder
ENTRYPOINT ["/sbin/tini", "--", "/usr/local/bin/recorder"]

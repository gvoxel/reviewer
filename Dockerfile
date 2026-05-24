# Build frontend
FROM node:20-alpine AS frontend
WORKDIR /frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build:all

# Build backend
FROM golang:1.25-alpine AS builder
RUN apk add --no-cache git
# VERSION is passed via `docker build --build-arg VERSION=...` (GH Actions sends
# github.ref_name on release). Falls back to `git describe` when not provided.
ARG VERSION=
COPY . /build
COPY --from=frontend /frontend/dist /build/frontend/dist
COPY --from=frontend /frontend/dist-vt /build/frontend/dist-vt
RUN cd /build \
    && VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}" \
    && echo "building version=$VERSION" \
    && LDFLAGS="-s -w -X main.version=$VERSION" \
    && go build -mod=vendor -ldflags "$LDFLAGS" -o /go/bin/reviewsrv ./cmd/reviewsrv \
    && CGO_ENABLED=0 go build -mod=vendor -ldflags "$LDFLAGS" -o /go/bin/reviewctl ./cmd/reviewctl \
    && mkdir -p /srv/download \
    && CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -mod=vendor -ldflags "$LDFLAGS" -o /srv/download/reviewctl-darwin-arm64      ./cmd/reviewctl \
    && CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -mod=vendor -ldflags "$LDFLAGS" -o /srv/download/reviewctl-darwin-amd64      ./cmd/reviewctl \
    && CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -mod=vendor -ldflags "$LDFLAGS" -o /srv/download/reviewctl-linux-amd64       ./cmd/reviewctl \
    && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -mod=vendor -ldflags "$LDFLAGS" -o /srv/download/reviewctl-windows-amd64.exe ./cmd/reviewctl \
    && (cd /srv/download && sha256sum reviewctl-* > SHA256SUMS)

# Final image
FROM alpine:latest

ENV TZ=Europe/Moscow
RUN apk --no-cache add ca-certificates tzdata && cp -r -f /usr/share/zoneinfo/$TZ /etc/localtime

COPY --from=builder /go/bin/reviewsrv .
COPY --from=builder /go/bin/reviewctl .
COPY --from=builder /srv/download /srv/download
COPY docs/patches/*.sql /patches/

ENTRYPOINT ["/reviewsrv"]
EXPOSE 8075

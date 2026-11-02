# syntax=docker/dockerfile:1

# ---- 1. Frontend build -------------------------------------------------------
FROM node:22-alpine AS web
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---- 2. Go build -------------------------------------------------------------
FROM golang:1.27-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY web/embed.go web/embed.go
COPY --from=web /web/dist web/dist
RUN go build -trimpath -ldflags="-s -w" -o /out/posterndb ./cmd/posterndb

# ---- 3. Runtime --------------------------------------------------------------
# distroless/static: no shell, no package manager, CA certificates included
# (needed for HTTPS calls to the Cloudflare API). Runs as non-root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/posterndb /posterndb
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=5 CMD ["/posterndb", "healthcheck"]
ENTRYPOINT ["/posterndb"]

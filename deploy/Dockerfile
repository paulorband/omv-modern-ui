# syntax=docker/dockerfile:1
# Build context: the repository root.  docker build -f deploy/Dockerfile -t omv-home .
FROM node:22-alpine AS web
WORKDIR /src/frontend
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.23-alpine AS api
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /src/frontend/dist/ ./internal/web/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/omv-home ./cmd/omv-home

# docker-cli + compose plugin are needed to run `docker compose` against the host daemon.
FROM alpine:3.20
RUN apk add --no-cache docker-cli docker-cli-compose ca-certificates tzdata
COPY --from=api /out/omv-home /usr/local/bin/omv-home
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/omv-home"]

# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
# Docker Hardened Images (DHI) are free and require authentication to dhi.io.
# Use Docker Hub credentials: `docker login dhi.io`.
FROM dhi.io/golang:1.26-debian13-dev AS builder

# git is needed by 'go build' to embed VCS info and by tests.
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Cache dependencies separately from source so they aren't re-downloaded on
# every source change.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X 'main.Version=${VERSION}'" \
      -o /out/ai-mr-comment .

# ── Runtime stage ─────────────────────────────────────────────────────────────
# Keep runtime small but include git since local-diff/commit workflows need it.
FROM dhi.io/alpine-base:3.23

USER root

# git: required for diff/commit/push commands
# ca-certificates: required for HTTPS calls to AI provider APIs and GitHub/GitLab
RUN apk add --no-cache git ca-certificates

COPY --from=builder /out/ai-mr-comment /usr/local/bin/ai-mr-comment

# Run as non-root inside the container.
# Files mounted from the host will still be accessible because Docker respects
# uid/gid mapping when the user passes --user or bind-mounts with correct perms.
RUN addgroup -S aiuser && adduser -S -G aiuser aiuser
USER aiuser

ENTRYPOINT ["ai-mr-comment"]
CMD ["--help"]

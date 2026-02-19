# syntax=docker/dockerfile:1

# ── Build stage ──────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

# git is needed by 'go build' to embed VCS info and by tests.
RUN apk add --no-cache git

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
# alpine gives us git (required for all local-diff commands) plus a small
# footprint. The final image is typically ~30 MB.
FROM alpine:3.21

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

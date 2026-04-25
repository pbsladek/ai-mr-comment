# syntax=docker/dockerfile:1

FROM dhi.io/golang:1.26-debian13-dev@sha256:086c893153f92793f3a1541793cd4a8e8b23bfd4ccaf70c8f4261f496080fb0e AS builder

RUN apt-get update && \
    apt-get upgrade -y --no-install-recommends && \
    apt-get install -y --no-install-recommends git ca-certificates && \
    rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api.go changelog.go config.go git.go main.go prompt.go token_estimator.go ./
COPY templates/ ./templates/

ARG VERSION=dev
ARG COMMIT=unknown
ARG COMMIT_FULL=unknown
ARG GOFIPS140=off
RUN CGO_ENABLED=0 GOFIPS140=${GOFIPS140} go build \
      -ldflags="-s -w -X 'main.Version=${VERSION}' -X 'main.Commit=${COMMIT}' -X 'main.CommitFull=${COMMIT_FULL}'" \
      -o /out/ai-mr-comment .

FROM dhi.io/debian-base:trixie-debian13-dev@sha256:9415967aa0ed8adea8b5c048994259d1982026dca143d0303c7bbe0e11ed67d3

ARG VERSION=dev
ARG COMMIT_FULL=unknown
ARG CREATED=""

LABEL org.opencontainers.image.title="ai-mr-comment" \
      org.opencontainers.image.description="AI-powered MR/PR comment and commit message generator for git diffs" \
      org.opencontainers.image.url="https://github.com/pbsladek/ai-mr-comment" \
      org.opencontainers.image.source="https://github.com/pbsladek/ai-mr-comment" \
      org.opencontainers.image.documentation="https://github.com/pbsladek/ai-mr-comment#readme" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="pbsladek" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT_FULL}" \
      org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.base.name="dhi.io/debian-base:trixie-debian13-dev"

RUN apt-get update && \
    apt-get upgrade -y --no-install-recommends && \
    apt-get install -y --no-install-recommends git ca-certificates && \
    rm -rf /var/lib/apt/lists/* && \
    sed -i '/^nonroot:/d' /etc/passwd && \
    sed -i '/^nonroot:/d' /etc/group && \
    printf 'ai-mr-comment:x:65532:65532::/home/nonroot:/bin/sh\n' >> /etc/passwd && \
    printf 'ai-mr-comment:x:65532:\n' >> /etc/group

COPY --from=builder /out/ai-mr-comment /usr/local/bin/ai-mr-comment

ENV HOME=/tmp
USER ai-mr-comment

ENTRYPOINT ["ai-mr-comment"]
CMD ["--help"]

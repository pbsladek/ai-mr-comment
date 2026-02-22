# syntax=docker/dockerfile:1

FROM dhi.io/golang:1.26-debian13-dev@sha256:7c7ee6a2db0fa9a332ba1c96f2cc11b53dc7535a899ce66e45391db4dfa26350 AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY api.go changelog.go config.go git.go main.go prompt.go token_estimator.go ./
COPY templates/default.tmpl ./templates/default.tmpl

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X 'main.Version=${VERSION}'" \
      -o /out/ai-mr-comment .

FROM dhi.io/debian-base:trixie-debian13-dev@sha256:2166e2eaef0651c9ad21de6ab5a34fda12541d89bccf7bcb0a94afceb1b1541b

RUN apt-get update && \
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

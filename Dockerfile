# syntax=docker/dockerfile:1

# Build stage: compile the Go binary
FROM dhi.io/golang:1.26-debian13-dev AS builder

RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
      -ldflags="-s -w -X 'main.Version=${VERSION}'" \
      -o /out/ai-mr-comment .

# Runtime prep stage: install dependencies and create user in standard Alpine
FROM alpine:3.23 AS runtime-prep

RUN apk add --no-cache git ca-certificates
RUN addgroup -S aiuser && adduser -S -G aiuser aiuser

# Runtime stage: use DHI base and copy prepared components
FROM dhi.io/alpine-base:3.23

COPY --from=runtime-prep /etc /etc
COPY --from=runtime-prep /usr/bin/git /usr/bin/git
COPY --from=runtime-prep /usr/libexec/git-core /usr/libexec/git-core
COPY --from=runtime-prep /usr/share/git-core /usr/share/git-core
COPY --from=runtime-prep /lib/libz.so.1 /lib/libz.so.1
COPY --from=runtime-prep /usr/lib/libpcre2-8.so.0 /usr/lib/libpcre2-8.so.0

COPY --from=builder /out/ai-mr-comment /usr/local/bin/ai-mr-comment

USER aiuser

ENTRYPOINT ["ai-mr-comment"]
CMD ["--help"]

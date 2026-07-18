FROM --platform=$BUILDPLATFORM golang:1.26-bookworm@sha256:1ecb7edf62a0408027bd5729dfd6b1b8766e578e8df93995b225dfd0944eb651 AS builder

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 \
    GOOS=$TARGETOS \
    GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w -X 'main.Version=${VERSION}'" -o /out/catacomb ./cmd/catacomb

FROM alpine:3.24@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/catacomb /usr/bin/catacomb

RUN adduser -D -u 65532 catacomb
USER catacomb

ENTRYPOINT ["/usr/bin/catacomb"]
CMD ["--help"]

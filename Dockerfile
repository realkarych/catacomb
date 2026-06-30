FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS builder

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

FROM alpine:3.23

RUN apk add --no-cache ca-certificates tzdata

COPY --from=builder /out/catacomb /usr/bin/catacomb

ENTRYPOINT ["/usr/bin/catacomb"]
CMD ["--help"]

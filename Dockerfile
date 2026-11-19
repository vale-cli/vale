# syntax=docker/dockerfile:1
ARG GOLANG_VER=1.27
ARG ALPINE_VER=3.23
ARG RUST_VER=1
ARG XX_VER=1.6.1

# The build runs on the host's platform and cross-compiles for the target.
FROM --platform=$BUILDPLATFORM golang:${GOLANG_VER}-alpine AS build

# See https://hub.docker.com/r/jdkato/vale

# TODO: DITA:
#    openjdk11 \
# COPY bin/dita-ot-3.6 /
#
# This currently isn't packaged because it makes the size 7x as big.

# Debug shell: $ docker run -it --entrypoint /bin/sh jdkato/vale -s

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG ltag

ENV CGO_ENABLED=0

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath \
    -ldflags "-s -w -X main.version=$ltag" \
    -o /out/vale ./cmd/vale

# typst2vast reads Typst. It cross-compiles too, with xx supplying the
# target's linker and C library.
FROM --platform=$BUILDPLATFORM tonistiigi/xx:${XX_VER} AS xx

FROM --platform=$BUILDPLATFORM rust:${RUST_VER}-alpine AS typst

COPY --from=xx / /
RUN apk add --no-cache clang lld

ARG TARGETPLATFORM

RUN xx-apk add --no-cache gcc musl-dev

RUN --mount=type=cache,target=/usr/local/cargo/registry \
    xx-cargo install --locked --root /out typst2vast && \
    xx-verify /out/bin/typst2vast

FROM alpine:${ALPINE_VER}

ARG ltag

LABEL org.opencontainers.image.title="Vale" \
      org.opencontainers.image.description="A linter for prose." \
      org.opencontainers.image.url="https://vale.sh" \
      org.opencontainers.image.source="https://github.com/vale-cli/vale" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.version="$ltag"

# xsltproc reads XML.
RUN apk add --no-cache libxslt

COPY --from=typst /out/bin/typst2vast /bin/typst2vast
COPY --from=build /out/vale /bin/vale

# ENV PATH="/bin:/dita-ot-3.6/bin:$PATH"
ENTRYPOINT ["/bin/vale"]

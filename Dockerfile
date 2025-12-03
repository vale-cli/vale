# syntax=docker/dockerfile:1
ARG GOLANG_VER=1.25
FROM golang:${GOLANG_VER}-alpine AS build

# See https://cloud.docker.com/repository/docker/jdkato/vale

# TODO: DITA / XML:
#    openjdk11 \
#    libxslt \
# COPY bin/dita-ot-3.6 /
#
# This currently isn't packaged because it makes the size 7x as big.

# Debug shell: $ docker run -it --entrypoint /bin/sh jdkato/vale -s

RUN apk add build-base

COPY . /app/
WORKDIR /app

ENV CGO_ENABLED=1

ARG ltag

RUN go build -ldflags "-s -w -X main.version=$ltag" -o /app/vale ./cmd/vale

FROM alpine

RUN apk add --no-cache \
    py3-docutils \
    asciidoctor \
    nodejs \
    npm

RUN npm install -g mdx2vast

COPY --from=build /app/vale /bin

# ENV PATH="/bin:/dita-ot-3.6/bin:$PATH"
ENTRYPOINT ["/bin/vale"]

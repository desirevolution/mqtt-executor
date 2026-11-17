# syntax=docker/dockerfile:1

FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS build
ARG TARGETOS
ARG TARGETARCH
# VERSION is injected into the binary (no VCS info inside the container)
ARG VERSION=dev
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -ldflags "-s -w -X main.versionTag=${VERSION}" \
    -o /out/mqtt-executor ./cmd/mqtt-executor/

# alpine (not scratch!): the executor runs configured commands - they need a
# userland (sh, echo, ...) inside the container. Extend this image with your
# own scripts/tools, mount them as volumes, or COPY --from this image the
# static /mqtt-executor binary into your own image.
FROM alpine:3.21
#ca-certificates: TLS broker URIs (ssl://) work out of the box with the
#system root store
RUN apk add --no-cache ca-certificates
WORKDIR /etc/mqtt-executor
COPY --from=build /out/mqtt-executor /usr/local/bin/mqtt-executor
# The webhook defaults to loopback. Set -web 0.0.0.0:8080 for published ports.
# The healthcheck probes GET /status from inside the container.
#Disable it (docker: --no-healthcheck, compose: healthcheck.disable: true)
#when running with -web="" or a non-default -web port
HEALTHCHECK --interval=30s --timeout=3s CMD wget -q --spider http://127.0.0.1:8080/status || exit 1
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/mqtt-executor"]

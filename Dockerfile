FROM golang:1.27.2-alpine AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOROOT_BOOTSTRAP=/usr/local/go
ARG ALPINE_MIRROR=https://dl-cdn.alpinelinux.org/alpine
RUN case "$ALPINE_MIRROR" in https://*) ;; *) echo "ALPINE_MIRROR must be an HTTPS URL" >&2; exit 1 ;; esac \
    && sed -i "s|https://dl-cdn.alpinelinux.org/alpine|${ALPINE_MIRROR%/}|g" /etc/apk/repositories \
    && apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "$(go run buildscripts/gen-ldflags.go)" -o /out/oc .
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -mod=readonly -trimpath -o /out/oc-console ./cmd/oc-console

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.10
LABEL org.opencontainers.image.title="OC" \
      org.opencontainers.image.source="https://github.com/soulteary/mc"
COPY --from=builder /out/oc /usr/bin/oc
COPY --from=builder /out/oc-console /usr/bin/oc-console
COPY --from=builder /src/CREDITS /licenses/CREDITS
COPY --from=builder /src/LICENSE /licenses/LICENSE
COPY --from=builder /src/NOTICE /licenses/NOTICE
COPY --from=builder /src/internal/notify/LICENSE /licenses/notify-LICENSE
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt
ENTRYPOINT ["oc"]

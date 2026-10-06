FROM golang:1.27.1-alpine AS builder
WORKDIR /src
ENV CGO_ENABLED=0 GOTOOLCHAIN=local
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS=linux
ARG TARGETARCH
RUN GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "$(go run buildscripts/gen-ldflags.go)" -o /out/oc .

FROM registry.access.redhat.com/ubi8/ubi-minimal:8.3
LABEL org.opencontainers.image.title="OC" \
      org.opencontainers.image.source="https://github.com/soulteary/mc"
COPY --from=builder /out/oc /usr/bin/oc
COPY --from=builder /src/CREDITS /licenses/CREDITS
COPY --from=builder /src/LICENSE /licenses/LICENSE
COPY --from=builder /src/NOTICE /licenses/NOTICE
RUN microdnf install ca-certificates --nodocs && microdnf clean all
ENTRYPOINT ["oc"]

# syntax=docker/dockerfile:1.7
# Build from the repository root: docker build -f deploy/direct-access/Dockerfile .
ARG GOLANG_IMAGE=golang:1.27.3-alpine
FROM --platform=$BUILDPLATFORM ${GOLANG_IMAGE} AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,id=moshu-direct-gomod,target=/go/pkg/mod go mod download github.com/lib/pq
COPY backend/cmd/direct-access/ ./cmd/direct-access/
RUN --mount=type=cache,id=moshu-direct-gomod,target=/go/pkg/mod \
    --mount=type=cache,id=moshu-direct-gobuild,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags='-s -w' -o /direct-access ./cmd/direct-access

FROM alpine:3.21
RUN apk add --no-cache ca-certificates && addgroup -S -g 10001 direct && adduser -S -D -H -u 10001 -G direct direct
COPY --from=build /direct-access /usr/local/bin/direct-access
USER 10001:10001
ENV DIRECT_ACCESS_LISTEN_ADDR=:8080
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=4s --start-period=5s --retries=3 CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/direct-access"]

FROM golang:1.26.5-alpine AS builder
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN VERSION_VALUE="$(./scripts/resolve-version.sh)" && \
    go build -tags="embed timetzdata" -ldflags="-s -w -X main.Version=${VERSION_VALUE}" -o main ./cmd/server

FROM alpine:3.21
RUN apk add --no-cache su-exec && addgroup -g 1000 sub2api && adduser -u 1000 -G sub2api -s /bin/sh -D sub2api
WORKDIR /app
COPY --from=builder /app/main /app/sub2api
COPY --from=builder /app/resources /app/resources
COPY scripts/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN mkdir -p /app/data && chown sub2api:sub2api /app/data && chmod +x /app/sub2api /app/docker-entrypoint.sh
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=10s --retries=5 CMD wget -q -T 5 -O /dev/null http://localhost:8080/health || exit 1
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/sub2api"]

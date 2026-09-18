FROM golang:1.27.0-alpine
ENV GOEXPERIMENT=jsonv2

WORKDIR /app

# Install the tools needed by the version resolver.
RUN apk add --no-cache git

# Copy module metadata first so dependency downloads stay cacheable.
COPY go.mod go.sum ./
RUN go mod download

# The embed build requires both backend source and internal/web/dist.
COPY . .

RUN VERSION_VALUE="$(./scripts/resolve-version.sh)" && \
    go build -tags="embed timetzdata" -ldflags="-s -w -X main.Version=${VERSION_VALUE}" -o main ./cmd/server

EXPOSE 8080

CMD ["./main"]

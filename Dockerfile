# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build with security flags
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
  go build \
  -ldflags="-s -w -extldflags '-static'" \
  -trimpath \
  -o /xget \
  ./src

# Runtime stage
FROM alpine:3.20

# Add metadata labels
LABEL maintainer="xget" \
  description="xget application" \
  version="1.0"

# Install runtime dependencies
RUN apk add --no-cache \
  ca-certificates \
  tzdata \
  && update-ca-certificates

# Create non-root user and group
RUN addgroup -g 1000 -S xget && \
  adduser -u 1000 -S xget -G xget -h /home/xget -s /sbin/nologin

# Create necessary directories with proper ownership
RUN mkdir -p /home/xget/.xget && \
  chown -R xget:xget /home/xget

# Copy binary with proper ownership
COPY --from=builder --chown=xget:xget /xget /usr/local/bin/xget

# Set working directory
WORKDIR /home/xget

# Switch to non-root user
USER xget

# Use exec form for better signal handling
ENTRYPOINT ["/usr/local/bin/xget"]
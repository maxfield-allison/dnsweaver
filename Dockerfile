# =============================================================================
# dnsweaver - Multi-Stage Dockerfile
# =============================================================================
#
# Image Strategy:
#   :dev     - Development/integration testing (develop branch)
#   :edge    - Bleeding edge from main branch
#   :latest  - Latest stable release (version tags)
#   :vX.Y.Z  - Specific version
#   :sha-XXX - Specific commit for debugging
#
# Build commands:
#   docker build -t dnsweaver:latest .
#   docker build --platform linux/amd64,linux/arm64 -t dnsweaver:latest .
#
# Multi-arch support: amd64 + arm64
# =============================================================================

ARG GO_VERSION=1.24
ARG ALPINE_VERSION=3.20

# -----------------------------------------------------------------------------
# Stage 1: Go Builder (Multi-Arch Cross-Compilation)
# -----------------------------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS builder

# Build arguments for multi-arch support
ARG TARGETPLATFORM
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Copy go mod files first for layer caching
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true

# Copy source
COPY . .

# Build with cross-compilation for target architecture
# CGO_ENABLED=0 ensures pure Go build (no C dependencies)
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH:-amd64} go build \
    -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o dnsweaver \
    ./cmd/dnsweaver

# Verify binary
RUN ls -la dnsweaver && file dnsweaver || true

# -----------------------------------------------------------------------------
# Stage 2: Runtime (Alpine)
# -----------------------------------------------------------------------------
FROM alpine:${ALPINE_VERSION}

# Labels
LABEL org.opencontainers.image.title="dnsweaver" \
    org.opencontainers.image.description="Automatic DNS record management for Docker containers" \
    org.opencontainers.image.source="https://gitlab.bluewillows.net/root/dnsweaver" \
    org.opencontainers.image.vendor="bluewillows.net"

# Install runtime dependencies
RUN apk add --no-cache ca-certificates tzdata wget

# Create non-root user
RUN addgroup -g 1000 dnsweaver && \
    adduser -u 1000 -G dnsweaver -s /bin/sh -D dnsweaver

# Copy binary from builder
COPY --from=builder /build/dnsweaver /usr/local/bin/dnsweaver

# Ensure binary is executable
RUN chmod +x /usr/local/bin/dnsweaver

# Default environment variables (can be overridden)
ENV DNSWEAVER_LOG_LEVEL="info" \
    DNSWEAVER_LOG_FORMAT="json" \
    DNSWEAVER_DRY_RUN="false" \
    DNSWEAVER_HEALTH_PORT="8080"

# Health check
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

# Run as non-root user
# Note: When mounting Docker socket, ensure socket has appropriate permissions
# or run as root if needed for Docker API access
USER dnsweaver

# Expose health port
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/dnsweaver"]

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

ARG GO_VERSION=1.26.4
ARG ALPINE_VERSION=3.23

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
# Stage 2: Minimal Runtime (Alpine)
# -----------------------------------------------------------------------------
FROM alpine:${ALPINE_VERSION}

# Labels
LABEL org.opencontainers.image.title="dnsweaver" \
    org.opencontainers.image.description="Automatic DNS record management for Docker and Kubernetes workloads" \
    org.opencontainers.image.source="https://github.com/maxfield-allison/dnsweaver" \
    org.opencontainers.image.vendor="maxfield-allison" \
    org.opencontainers.image.base.name="alpine:3.23"

# Changing CACHE_BUST invalidates Docker layer cache for apk upgrade.
# CI passes --build-arg CACHE_BUST=$CI_PIPELINE_ID so every pipeline
# runs a fresh apk upgrade, even if the base image hash is unchanged.
ARG CACHE_BUST=dev

# Install runtime dependencies (no wget/curl — reduces attack surface)
# Upgrade base packages first to pick up security fixes
# su-exec: drops privileges from root to dnsweaver in the entrypoint after
#         performing one-time docker socket GID detection.
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata su-exec

# Create non-root user
RUN addgroup -g 1000 dnsweaver && \
    adduser -u 1000 -G dnsweaver -s /bin/sh -D dnsweaver

# Copy binary from builder
COPY --from=builder /build/dnsweaver /usr/local/bin/dnsweaver

# Copy entrypoint script (handles Docker socket GID auto-detection)
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh

# Ensure binary and entrypoint are executable
RUN chmod +x /usr/local/bin/dnsweaver /usr/local/bin/entrypoint.sh

# Default environment variables (can be overridden)
ENV DNSWEAVER_LOG_LEVEL="info" \
    DNSWEAVER_LOG_FORMAT="json" \
    DNSWEAVER_DRY_RUN="false" \
    DNSWEAVER_HEALTH_PORT="8080"

# Health check (using busybox nc — no wget needed)
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD ["/bin/sh", "-c", "echo -e 'GET /health HTTP/1.0\r\nHost: localhost\r\n\r\n' | nc localhost 8080 | grep -q '200 OK' || exit 1"]

# Note: container starts as root so the entrypoint can detect the docker
# socket GID and add the dnsweaver user to the matching group. The entrypoint
# then drops privileges via su-exec before invoking the binary, so the
# dnsweaver process always runs unprivileged.
#
# If you don't mount the docker socket (k8s-only, socket proxy), the entrypoint
# skips the GID logic entirely.

# Expose health port
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]

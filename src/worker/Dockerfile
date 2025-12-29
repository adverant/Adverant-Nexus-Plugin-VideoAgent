# ============================================================================
# VideoAgent Go Worker - Multi-stage Dockerfile
# Includes FFmpeg for video processing (CPU-only)
# ============================================================================

# ============================================================================
# Stage 1: Build Go binary
# ============================================================================
FROM --platform=linux/amd64 golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

WORKDIR /build

# Copy go mod file first for better caching (context is parent dir, so use relative path)
COPY services/nexus-videoagent/worker/go.mod ./

# Copy source code (needed for go mod tidy to resolve imports)
COPY services/nexus-videoagent/worker/ .

# Download dependencies and generate go.sum
RUN go mod download && go mod tidy

# Build binary with optimizations
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o videoagent-worker \
    ./cmd/worker

# ============================================================================
# Stage 2: Runtime image with FFmpeg
# ============================================================================
FROM --platform=linux/amd64 alpine:3.20

# Install runtime dependencies
# - FFmpeg for video processing (includes ffprobe, CPU-only, no GPU acceleration)
# - yt-dlp for YouTube video downloading
# - python3 (required by yt-dlp)
# - ca-certificates for HTTPS
# - tzdata for timezone support
RUN apk add --no-cache \
    ffmpeg \
    yt-dlp \
    python3 \
    ca-certificates \
    tzdata

# Create non-root user
RUN addgroup -g 1000 videoagent && \
    adduser -D -u 1000 -G videoagent videoagent

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/videoagent-worker /app/videoagent-worker

# Create directories for temporary storage
RUN mkdir -p /app/temp /app/logs && \
    chown -R videoagent:videoagent /app

# Switch to non-root user
USER videoagent

# Environment variables (override in docker-compose)
ENV REDIS_URL=redis://nexus-redis:6379 \
    POSTGRES_HOST=nexus-postgres \
    POSTGRES_PORT=5432 \
    POSTGRES_USER=unified_brain \
    POSTGRES_PASSWORD=graphrag123 \
    POSTGRES_DB=nexus_graphrag \
    QDRANT_URL=http://nexus-qdrant:6333 \
    MAGEAGENT_URL=http://nexus-mageagent:8080 \
    TEMP_DIR=/app/temp \
    LOG_LEVEL=info \
    MAX_CONCURRENT_JOBS=3 \
    QUEUE_NAME=videoagent:default

# Health check (using pgrep without -x to handle truncated process names)
HEALTHCHECK --interval=30s --timeout=10s --start-period=60s --retries=3 \
    CMD pgrep videoagent > /dev/null || exit 1

# Expose metrics port (if implemented)
EXPOSE 9091

# Start worker
ENTRYPOINT ["/app/videoagent-worker"]

# VideoAgent Bridge Service Dockerfile
# Multi-stage build: TypeScript bridge + Go worker binary
#
# IMPORTANT: Build this from services/nexus-videoagent directory:
#   docker build -f bridge/Dockerfile -t nexus-videoagent-bridge .

# Build arguments for traceability
ARG BUILD_ID
ARG BUILD_TIMESTAMP
ARG GIT_COMMIT
ARG GIT_BRANCH
ARG VERSION=1.0.0

# ============================================
# Stage 1: Build TypeScript Bridge
# ============================================
FROM node:20-alpine AS bridge-builder

WORKDIR /build

# Copy package files
COPY bridge/package.json bridge/tsconfig.json ./

# Install dependencies
RUN npm install --production=false

# Copy source code
COPY bridge/src ./src

# Build TypeScript to JavaScript
RUN npm run build

# ============================================
# Stage 2: Build Go Worker
# ============================================
FROM golang:1.22-alpine AS worker-builder

WORKDIR /build

# Install build dependencies
RUN apk add --no-cache git make

# Copy Go module files
COPY worker/go.mod worker/go.sum ./

# Download dependencies
RUN go mod download

# Copy Go source code
COPY worker ./

# Build Go worker binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o /build/videoagent-worker \
    ./cmd/worker

# ============================================
# Stage 3: Runtime Image
# ============================================
FROM node:20-alpine

# Install runtime dependencies
RUN apk add --no-cache \
    ffmpeg \
    python3 \
    py3-pip \
    && pip3 install --no-cache-dir --break-system-packages yt-dlp

WORKDIR /app

# Copy Node.js dependencies from bridge builder
COPY --from=bridge-builder /build/node_modules ./node_modules
COPY --from=bridge-builder /build/package.json ./package.json

# Copy compiled TypeScript (bridge service)
COPY --from=bridge-builder /build/dist ./dist

# Copy Go worker binary
COPY --from=worker-builder /build/videoagent-worker /app/videoagent-worker

# Ensure Go binary is executable
RUN chmod +x /app/videoagent-worker

# Build metadata labels
ARG BUILD_ID
ARG BUILD_TIMESTAMP
ARG GIT_COMMIT
ARG GIT_BRANCH
ARG VERSION

LABEL org.opencontainers.image.title="Nexus VideoAgent Bridge" \
      org.opencontainers.image.description="BullMQ bridge to Go VideoAgent worker" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}" \
      com.adverant.build.id="${BUILD_ID}" \
      com.adverant.build.timestamp="${BUILD_TIMESTAMP}" \
      com.adverant.build.branch="${GIT_BRANCH}"

# Expose build metadata as environment variables
ENV NEXUS_BUILD_ID="${BUILD_ID}" \
    NEXUS_BUILD_TIMESTAMP="${BUILD_TIMESTAMP}" \
    NEXUS_GIT_COMMIT="${GIT_COMMIT}" \
    NEXUS_VERSION="${VERSION}"

# Environment variables with defaults
ENV REDIS_URL=redis://nexus-redis:6379 \
    QUEUE_NAME=videoagent \
    WORKER_BINARY=/app/videoagent-worker \
    BRIDGE_CONCURRENCY=3 \
    JOB_TIMEOUT=3600000 \
    TEMP_DIR=/tmp/videoagent \
    MAGEAGENT_URL=http://nexus-mageagent:8081 \
    GRAPHRAG_URL=http://nexus-graphrag:8090 \
    NEXUS_AUTH_URL=http://nexus-auth:8080 \
    WORKER_CONCURRENCY=3

# Create temp directory
RUN mkdir -p /tmp/videoagent && chmod 777 /tmp/videoagent

# Run bridge service
CMD ["node", "dist/index.js"]

# VideoAgent Worker Dockerfile
# Multi-stage build: Node.js worker + Go binary
# IMPORTANT: Build with --platform linux/amd64 for compatibility

# Stage 1: Build Go binary
FROM --platform=linux/amd64 golang:1.22-alpine AS go-builder

WORKDIR /build

# Copy Go worker source (context is parent videoagent directory)
COPY worker/go.mod worker/go.sum ./
COPY worker/cmd ./cmd
COPY worker/internal ./internal

# Build Go binary
RUN go mod download && \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /build/worker-binary ./cmd/worker/main.go

# Stage 2: Build Node.js worker
FROM --platform=linux/amd64 node:18-alpine AS node-builder

WORKDIR /app

# Copy package files (context is parent videoagent directory)
COPY worker-node/package*.json ./
COPY worker-node/tsconfig.json ./

# Install dependencies
RUN npm ci

# Copy source
COPY worker-node/src ./src

# Build TypeScript
RUN npm run build

# Stage 3: Production image
FROM --platform=linux/amd64 node:18-alpine

# Install FFmpeg for video processing and yt-dlp for YouTube downloads
# yt-dlp requires Python 3
RUN apk add --no-cache ffmpeg yt-dlp python3

WORKDIR /app

# Copy Node.js dependencies
COPY --from=node-builder /app/node_modules ./node_modules
COPY --from=node-builder /app/package*.json ./

# Copy compiled Node.js worker
COPY --from=node-builder /app/dist ./dist

# Copy Go binary
COPY --from=go-builder /build/worker-binary ./worker

# Make Go binary executable
RUN chmod +x ./worker

# Create working directory
RUN mkdir -p /tmp/videoagent && chmod 777 /tmp/videoagent

# Set environment variables
ENV NODE_ENV=production
ENV GO_BINARY_PATH=/app/worker
ENV WORKING_DIR=/tmp/videoagent

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
  CMD node -e "require('net').connect(6379, process.env.REDIS_HOST || 'localhost', () => process.exit(0)).on('error', () => process.exit(1))"

# Start worker
CMD ["node", "dist/worker.js"]

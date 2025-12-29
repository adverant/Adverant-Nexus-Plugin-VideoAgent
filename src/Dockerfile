# ============================================================================
# VideoAgent TypeScript API Gateway - Multi-stage Dockerfile
# ============================================================================

# ============================================================================
# Stage 1: Build TypeScript
# ============================================================================
FROM --platform=linux/amd64 node:20-alpine AS builder

WORKDIR /build

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci --only=production && \
    npm ci --only=development

# Copy source code
COPY . .

# Build TypeScript
RUN npm run build

# ============================================================================
# Stage 2: Runtime image
# ============================================================================
FROM --platform=linux/amd64 node:20-alpine

# Install runtime dependencies
RUN apk add --no-cache \
    ca-certificates \
    tzdata \
    tini

# Create non-root user with high-numbered UID/GID to avoid conflicts
# Using 10001 which is in the safe range for non-system users
# UID/GID can be overridden via build args if needed
ARG USER_UID=10001
ARG USER_GID=10001

RUN addgroup -g ${USER_GID} videoagent && \
    adduser -D -u ${USER_UID} -G videoagent videoagent

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install production dependencies only
RUN npm ci --only=production && \
    npm cache clean --force

# Copy built application from builder
COPY --from=builder /build/dist ./dist

# Create directories
RUN mkdir -p /app/temp /app/logs && \
    chown -R videoagent:videoagent /app

# Switch to non-root user
USER videoagent

# Environment variables (override in docker-compose)
ENV NODE_ENV=production \
    PORT=3000 \
    REDIS_URL=redis://nexus-redis:6379 \
    POSTGRES_URL=postgresql://unified_brain:graphrag123@nexus-postgres:5432/nexus_graphrag \
    MAGEAGENT_URL=http://nexus-mageagent:8080 \
    ENABLE_GOOGLE_DRIVE=true \
    GOOGLE_CLIENT_ID= \
    GOOGLE_CLIENT_SECRET= \
    GOOGLE_REDIRECT_URI=http://localhost:3000/api/gdrive/auth/callback \
    MAX_FILE_SIZE=2147483648 \
    LOG_LEVEL=info

# Health check
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 \
    CMD node -e "require('http').get('http://localhost:3000/health', (r) => process.exit(r.statusCode === 200 ? 0 : 1))"

# Expose API port
EXPOSE 3000

# Use tini for proper signal handling
ENTRYPOINT ["/sbin/tini", "--"]

# Start API server
CMD ["node", "dist/server.js"]

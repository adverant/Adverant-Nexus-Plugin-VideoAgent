#!/bin/bash

# ============================================================================
# Redis Job Injector for VideoAgent Worker
# ============================================================================
# Injects video processing jobs directly into Redis queue for testing
# without requiring the VideoAgent API to be running

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
REDIS_CONTAINER="nexus-redis"
QUEUE_NAME="videoagent:default"
DEFAULT_USER_ID="test-user-$(date +%s)"

# ============================================================================
# Usage Function
# ============================================================================
usage() {
    cat << EOF
Usage: $0 [OPTIONS]

Inject video processing jobs into Redis queue for VideoAgent worker testing.

OPTIONS:
    -u, --url VIDEO_URL          Video URL to process (required)
    -i, --user-id USER_ID        User ID for the job (default: test-user-TIMESTAMP)
    -q, --queue QUEUE_NAME       Queue name (default: videoagent:default)
    -p, --priority PRIORITY      Job priority 1-10 (default: 5)
    -h, --help                   Show this help message

EXAMPLES:
    # Process a YouTube video
    $0 --url "https://www.youtube.com/watch?v=dQw4w9WgXcQ"

    # Process with custom user ID
    $0 --url "https://example.com/video.mp4" --user-id "user123"

    # High priority job
    $0 --url "https://example.com/video.mp4" --priority 9

NOTES:
    - Requires nexus-redis container to be running
    - Worker will pick up jobs automatically from the queue
    - Job ID will be auto-generated (UUID format)

EOF
    exit 1
}

# ============================================================================
# Log Functions
# ============================================================================
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_debug() {
    echo -e "${BLUE}[DEBUG]${NC} $1"
}

# ============================================================================
# Parse Arguments
# ============================================================================
VIDEO_URL=""
USER_ID="$DEFAULT_USER_ID"
PRIORITY=5

while [[ $# -gt 0 ]]; do
    case $1 in
        -u|--url)
            VIDEO_URL="$2"
            shift 2
            ;;
        -i|--user-id)
            USER_ID="$2"
            shift 2
            ;;
        -q|--queue)
            QUEUE_NAME="$2"
            shift 2
            ;;
        -p|--priority)
            PRIORITY="$2"
            shift 2
            ;;
        -h|--help)
            usage
            ;;
        *)
            log_error "Unknown option: $1"
            usage
            ;;
    esac
done

# Validate required parameters
if [ -z "$VIDEO_URL" ]; then
    log_error "Video URL is required"
    usage
fi

# Validate priority
if ! [[ "$PRIORITY" =~ ^[1-9]|10$ ]]; then
    log_error "Priority must be between 1 and 10"
    exit 1
fi

# ============================================================================
# Check Redis Container
# ============================================================================
log_info "Checking Redis container..."
if ! docker ps --filter "name=${REDIS_CONTAINER}" --format "{{.Names}}" | grep -q "^${REDIS_CONTAINER}$"; then
    log_error "Redis container '${REDIS_CONTAINER}' is not running"
    exit 1
fi
log_info "✓ Redis container is running"

# ============================================================================
# Generate Job ID
# ============================================================================
# Generate a UUID-like job ID (simple version)
JOB_ID="videoagent_$(date +%s)_$(openssl rand -hex 6)"
log_info "Generated Job ID: ${JOB_ID}"

# ============================================================================
# Create Job Payload
# ============================================================================
log_info "Creating job payload..."

# Get current timestamp in RFC3339 format
TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

# Create JSON payload for Asynq job
# Asynq job format: {"type": "task-type", "payload": {...}, "opts": {...}}
JOB_PAYLOAD=$(cat <<EOF
{
  "type": "video:process",
  "payload": {
    "jobId": "${JOB_ID}",
    "userId": "${USER_ID}",
    "videoUrl": "${VIDEO_URL}",
    "options": {
      "extractFrames": true,
      "extractAudio": true,
      "extractScenes": true,
      "generateEmbeddings": true,
      "maxFrames": 16,
      "frameInterval": 5
    },
    "metadata": {
      "source": "redis-job-injector",
      "createdAt": "${TIMESTAMP}"
    }
  },
  "opts": {
    "queue": "${QUEUE_NAME}",
    "max_retry": 3,
    "timeout": 600
  }
}
EOF
)

log_debug "Job payload:"
echo "$JOB_PAYLOAD" | python3 -m json.tool

# ============================================================================
# Inject Job into Redis
# ============================================================================
log_info "Injecting job into Redis queue: ${QUEUE_NAME}..."

# Asynq stores jobs in Redis using specific keys
# Format: asynq:{queue}:pending
REDIS_KEY="asynq:{${QUEUE_NAME}}:pending"

# Inject the job using Redis LPUSH (left push to list)
docker exec -i ${REDIS_CONTAINER} redis-cli LPUSH "${REDIS_KEY}" "${JOB_PAYLOAD}" > /dev/null

if [ $? -eq 0 ]; then
    log_info "✓ Job successfully injected into queue"
else
    log_error "Failed to inject job into Redis"
    exit 1
fi

# ============================================================================
# Verify Job in Queue
# ============================================================================
log_info "Verifying job in queue..."

QUEUE_LENGTH=$(docker exec ${REDIS_CONTAINER} redis-cli LLEN "${REDIS_KEY}")
log_info "✓ Queue length: ${QUEUE_LENGTH} job(s)"

# ============================================================================
# Display Job Information
# ============================================================================
echo ""
echo "========================================"
echo "Job Injection Summary"
echo "========================================"
echo -e "${GREEN}Job ID:${NC}        ${JOB_ID}"
echo -e "${GREEN}User ID:${NC}       ${USER_ID}"
echo -e "${GREEN}Video URL:${NC}     ${VIDEO_URL}"
echo -e "${GREEN}Queue:${NC}         ${QUEUE_NAME}"
echo -e "${GREEN}Priority:${NC}      ${PRIORITY}"
echo -e "${GREEN}Queue Length:${NC}  ${QUEUE_LENGTH} job(s)"
echo ""
echo -e "${BLUE}[INFO]${NC} Worker will pick up the job automatically"
echo -e "${BLUE}[INFO]${NC} Monitor worker logs: docker logs -f videoagent-worker"
echo -e "${BLUE}[INFO]${NC} Check job status in PostgreSQL: nexus_videoagent database"
echo ""

# ============================================================================
# Optional: Monitor Worker Logs
# ============================================================================
read -p "Do you want to monitor worker logs now? (y/N): " -n 1 -r
echo
if [[ $REPLY =~ ^[Yy]$ ]]; then
    log_info "Monitoring worker logs (Ctrl+C to exit)..."
    docker logs -f videoagent-worker
fi

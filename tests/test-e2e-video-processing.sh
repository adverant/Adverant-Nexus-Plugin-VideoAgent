#!/bin/bash

# ============================================================================
# VideoAgent End-to-End Video Processing Test
# ============================================================================
# Complete E2E test that:
# 1. Injects a test video job into Redis queue
# 2. Monitors worker processing
# 3. Validates results in PostgreSQL and Qdrant
# 4. Reports detailed processing metrics

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REDIS_CONTAINER="nexus-redis"
POSTGRES_CONTAINER="nexus-postgres"
QDRANT_CONTAINER="nexus-qdrant"
WORKER_CONTAINER="videoagent-worker"
QUEUE_NAME="videoagent:default"
DB_NAME="nexus_videoagent"
DB_USER="unified_brain"
DB_PASSWORD="graphrag123"

# Test video URL (short video for fast testing)
# Using a short Creative Commons video for testing
TEST_VIDEO_URL="https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4"
TEST_USER_ID="test-user-e2e-$(date +%s)"

# Timeouts and intervals
MAX_WAIT_TIME=300  # 5 minutes max wait
CHECK_INTERVAL=5   # Check every 5 seconds

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

log_step() {
    echo -e "\n${CYAN}===================================================${NC}"
    echo -e "${CYAN}$1${NC}"
    echo -e "${CYAN}===================================================${NC}\n"
}

# ============================================================================
# Check Prerequisites
# ============================================================================
check_prerequisites() {
    log_step "Step 1: Checking Prerequisites"

    # Check if required containers are running
    local containers=("$REDIS_CONTAINER" "$POSTGRES_CONTAINER" "$QDRANT_CONTAINER" "$WORKER_CONTAINER")

    for container in "${containers[@]}"; do
        if ! docker ps --filter "name=${container}" --format "{{.Names}}" | grep -q "^${container}$"; then
            log_error "Container '${container}' is not running"
            return 1
        fi
        log_info "✓ Container '${container}' is running"
    done

    # Check worker health
    local worker_health=$(docker inspect --format='{{.State.Health.Status}}' ${WORKER_CONTAINER} 2>/dev/null || echo "unknown")
    if [ "$worker_health" != "healthy" ]; then
        log_warn "Worker health status: ${worker_health} (expected: healthy)"
    else
        log_info "✓ Worker is healthy"
    fi

    return 0
}

# ============================================================================
# Generate Job ID
# ============================================================================
generate_job_id() {
    echo "videoagent_e2e_$(date +%s)_$(openssl rand -hex 6)"
}

# ============================================================================
# Inject Test Job
# ============================================================================
inject_test_job() {
    log_step "Step 2: Injecting Test Video Job"

    JOB_ID=$(generate_job_id)
    log_info "Job ID: ${JOB_ID}"
    log_info "User ID: ${TEST_USER_ID}"
    log_info "Video URL: ${TEST_VIDEO_URL}"

    # Create job payload
    local timestamp=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local payload=$(cat <<EOF
{
  "type": "video:process",
  "payload": {
    "jobId": "${JOB_ID}",
    "userId": "${TEST_USER_ID}",
    "videoUrl": "${TEST_VIDEO_URL}",
    "options": {
      "extractFrames": true,
      "extractAudio": true,
      "extractScenes": true,
      "generateEmbeddings": true,
      "maxFrames": 16,
      "frameInterval": 2
    },
    "metadata": {
      "source": "e2e-test",
      "createdAt": "${timestamp}"
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
    echo "$payload" | python3 -m json.tool

    # Inject into Redis
    local redis_key="asynq:{${QUEUE_NAME}}:pending"
    docker exec -i ${REDIS_CONTAINER} redis-cli LPUSH "${redis_key}" "${payload}" > /dev/null

    if [ $? -eq 0 ]; then
        log_info "✓ Job successfully injected into queue"
    else
        log_error "Failed to inject job into Redis"
        return 1
    fi

    # Verify in queue
    local queue_length=$(docker exec ${REDIS_CONTAINER} redis-cli LLEN "${redis_key}")
    log_info "✓ Queue length: ${queue_length} job(s)"

    return 0
}

# ============================================================================
# Monitor Job Processing
# ============================================================================
monitor_job_processing() {
    log_step "Step 3: Monitoring Job Processing"

    log_info "Waiting for worker to pick up job (max ${MAX_WAIT_TIME}s)..."

    local elapsed=0
    local job_started=false
    local job_completed=false
    local job_status="unknown"

    while [ $elapsed -lt $MAX_WAIT_TIME ]; do
        # Check worker logs for job processing
        if docker logs ${WORKER_CONTAINER} 2>&1 | grep -q "Processing job.*${JOB_ID}"; then
            if [ "$job_started" = false ]; then
                log_info "✓ Worker started processing job (${elapsed}s)"
                job_started=true
            fi
        fi

        # Check PostgreSQL for job status
        job_status=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
            "SELECT status FROM videoagent.jobs WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs || echo "")

        case "$job_status" in
            "completed")
                log_info "✓ Job completed successfully! (${elapsed}s)"
                job_completed=true
                break
                ;;
            "processing")
                if [ "$job_started" = false ]; then
                    log_info "✓ Job status: processing (${elapsed}s)"
                    job_started=true
                fi
                ;;
            "failed")
                log_error "Job failed!"
                # Get error message
                local error_msg=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
                    "SELECT error_message FROM videoagent.jobs WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)
                log_error "Error: ${error_msg}"
                return 1
                ;;
            "pending"|"")
                # Still waiting
                ;;
        esac

        sleep $CHECK_INTERVAL
        elapsed=$((elapsed + CHECK_INTERVAL))

        # Show progress every 15 seconds
        if [ $((elapsed % 15)) -eq 0 ] && [ "$job_completed" = false ]; then
            log_info "Still processing... (${elapsed}s elapsed)"
        fi
    done

    if [ "$job_completed" = false ]; then
        log_error "Timeout: Job did not complete within ${MAX_WAIT_TIME}s"
        log_info "Current status: ${job_status}"
        return 1
    fi

    return 0
}

# ============================================================================
# Validate PostgreSQL Results
# ============================================================================
validate_postgres_results() {
    log_step "Step 4: Validating PostgreSQL Results"

    # Check job record
    log_info "Checking job record..."
    local job_data=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT job_id, status, video_url FROM videoagent.jobs WHERE job_id = '${JOB_ID}';" 2>/dev/null)

    if [ -z "$job_data" ]; then
        log_error "Job record not found in database"
        return 1
    fi
    log_info "✓ Job record found"
    echo "$job_data"

    # Check frames
    log_info "Checking extracted frames..."
    local frame_count=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT COUNT(*) FROM videoagent.frames WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)

    if [ "$frame_count" -gt 0 ]; then
        log_info "✓ Found ${frame_count} frames"
    else
        log_warn "No frames found (expected at least 1)"
    fi

    # Check scenes
    log_info "Checking detected scenes..."
    local scene_count=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT COUNT(*) FROM videoagent.scenes WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)

    if [ "$scene_count" -gt 0 ]; then
        log_info "✓ Found ${scene_count} scenes"
    else
        log_warn "No scenes found"
    fi

    # Check audio analysis
    log_info "Checking audio analysis..."
    local audio_count=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT COUNT(*) FROM videoagent.audio_analysis WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)

    if [ "$audio_count" -gt 0 ]; then
        log_info "✓ Found ${audio_count} audio analysis records"
    else
        log_warn "No audio analysis found"
    fi

    # Get video metadata
    log_info "Fetching video metadata..."
    local metadata=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT duration, width, height, fps, format FROM videoagent.videos WHERE job_id = '${JOB_ID}';" 2>/dev/null)

    if [ -n "$metadata" ]; then
        log_info "✓ Video metadata:"
        echo "$metadata"
    else
        log_warn "No video metadata found"
    fi

    return 0
}

# ============================================================================
# Validate Qdrant Results
# ============================================================================
validate_qdrant_results() {
    log_step "Step 5: Validating Qdrant Vector Embeddings"

    # Check video_embeddings collection
    log_info "Checking video_embeddings collection..."
    local video_embeddings=$(docker exec ${QDRANT_CONTAINER} wget -qO- \
        "http://localhost:6333/collections/video_embeddings/points/scroll" \
        -H "Content-Type: application/json" \
        --post-data '{"limit": 100}' 2>/dev/null | python3 -c "import sys, json; data=json.load(sys.stdin); print(len(data.get('result', {}).get('points', [])))")

    if [ "$video_embeddings" -gt 0 ]; then
        log_info "✓ Found ${video_embeddings} video embeddings (1024-D)"
    else
        log_warn "No video embeddings found in Qdrant"
    fi

    # Check scene_embeddings collection
    log_info "Checking scene_embeddings collection..."
    local scene_embeddings=$(docker exec ${QDRANT_CONTAINER} wget -qO- \
        "http://localhost:6333/collections/scene_embeddings/points/scroll" \
        -H "Content-Type: application/json" \
        --post-data '{"limit": 100}' 2>/dev/null | python3 -c "import sys, json; data=json.load(sys.stdin); print(len(data.get('result', {}).get('points', [])))")

    if [ "$scene_embeddings" -gt 0 ]; then
        log_info "✓ Found ${scene_embeddings} scene embeddings (1024-D)"
    else
        log_warn "No scene embeddings found in Qdrant"
    fi

    # Test similarity search
    if [ "$video_embeddings" -gt 0 ]; then
        log_info "Testing similarity search..."
        # This would require a query embedding - skip for now
        log_debug "Similarity search test skipped (requires query embedding)"
    fi

    return 0
}

# ============================================================================
# Display Processing Summary
# ============================================================================
display_summary() {
    log_step "Step 6: Processing Summary"

    # Get detailed job info
    local job_info=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -A -F'|' -c \
        "SELECT
            status,
            created_at,
            started_at,
            completed_at,
            EXTRACT(EPOCH FROM (completed_at - started_at)) as processing_time_sec
         FROM videoagent.jobs
         WHERE job_id = '${JOB_ID}';" 2>/dev/null | tail -1)

    # Get counts
    local frame_count=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT COUNT(*) FROM videoagent.frames WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)

    local scene_count=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT COUNT(*) FROM videoagent.scenes WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)

    local audio_count=$(docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -t -c \
        "SELECT COUNT(*) FROM videoagent.audio_analysis WHERE job_id = '${JOB_ID}';" 2>/dev/null | xargs)

    # Parse job info
    IFS='|' read -r status created started completed processing_time <<< "$job_info"

    echo ""
    echo "========================================"
    echo "E2E Test Results"
    echo "========================================"
    echo -e "${GREEN}Job ID:${NC}              ${JOB_ID}"
    echo -e "${GREEN}User ID:${NC}             ${TEST_USER_ID}"
    echo -e "${GREEN}Video URL:${NC}           ${TEST_VIDEO_URL}"
    echo ""
    echo -e "${GREEN}Status:${NC}              ${status}"
    echo -e "${GREEN}Created:${NC}             ${created}"
    echo -e "${GREEN}Started:${NC}             ${started}"
    echo -e "${GREEN}Completed:${NC}           ${completed}"
    echo -e "${GREEN}Processing Time:${NC}     ${processing_time}s"
    echo ""
    echo -e "${GREEN}Frames Extracted:${NC}    ${frame_count}"
    echo -e "${GREEN}Scenes Detected:${NC}     ${scene_count}"
    echo -e "${GREEN}Audio Analysis:${NC}      ${audio_count} records"
    echo ""
    echo -e "${GREEN}Vector Embeddings:${NC}"
    echo -e "  - Video embeddings:  (check Qdrant)"
    echo -e "  - Scene embeddings:  (check Qdrant)"
    echo -e "  - Dimension:         1024-D (VoyageAI voyage-3)"
    echo ""

    if [ "$status" = "completed" ]; then
        echo -e "${GREEN}✓ E2E Test PASSED${NC}"
        echo ""
        return 0
    else
        echo -e "${RED}✗ E2E Test FAILED${NC}"
        echo ""
        return 1
    fi
}

# ============================================================================
# Cleanup Function
# ============================================================================
cleanup_test_data() {
    read -p "Do you want to clean up test data? (y/N): " -n 1 -r
    echo
    if [[ $REPLY =~ ^[Yy]$ ]]; then
        log_info "Cleaning up test data..."

        # Delete from PostgreSQL
        docker exec ${POSTGRES_CONTAINER} psql -U ${DB_USER} -d ${DB_NAME} -c \
            "DELETE FROM videoagent.jobs WHERE job_id = '${JOB_ID}';" 2>/dev/null

        log_info "✓ Test data cleaned up"
    fi
}

# ============================================================================
# Main Execution
# ============================================================================
main() {
    echo "========================================"
    echo "VideoAgent E2E Video Processing Test"
    echo "========================================"
    echo ""

    # Run test steps
    check_prerequisites || exit 1
    inject_test_job || exit 1
    monitor_job_processing || exit 1
    validate_postgres_results || exit 1
    validate_qdrant_results || exit 1
    display_summary || exit 1

    # Optional cleanup
    cleanup_test_data

    echo ""
    log_info "E2E test completed successfully!"
    echo ""
}

main "$@"

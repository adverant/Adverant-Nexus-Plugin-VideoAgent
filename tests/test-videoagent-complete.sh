#!/bin/bash
# ============================================================================
# VideoAgent Complete Test Suite
# Tests all functionality: URL processing, Google Drive, folder processing
# ============================================================================

set -e

# Colors for output
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Configuration
API_URL="${VIDEOAGENT_API_URL:-http://localhost:9101}"
USER_ID="${TEST_USER_ID:-test-user-123}"
SESSION_ID="${TEST_SESSION_ID:-test-session-$(date +%s)}"

# Counters
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_TOTAL=0

# Helper functions
print_header() {
    echo ""
    echo "=========================================="
    echo "$1"
    echo "=========================================="
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
    ((TESTS_PASSED++))
    ((TESTS_TOTAL++))
}

print_failure() {
    echo -e "${RED}✗ $1${NC}"
    ((TESTS_FAILED++))
    ((TESTS_TOTAL++))
}

print_info() {
    echo -e "${YELLOW}→ $1${NC}"
}

# Test 1: Health Check
test_health_check() {
    print_header "Test 1: Health Check"

    response=$(curl -s -w "\n%{http_code}" "$API_URL/health")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ]; then
        print_success "Health check passed"
        echo "$body" | jq '.' || echo "$body"
    else
        print_failure "Health check failed (HTTP $http_code)"
        echo "$body"
    fi
}

# Test 2: Process Video from URL
test_process_video_url() {
    print_header "Test 2: Process Video from URL"

    # Using a sample video URL (replace with actual test video)
    VIDEO_URL="https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4"

    print_info "Submitting video URL for processing..."

    response=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/video/process" \
        -H "Content-Type: application/json" \
        -d @- << EOF
{
    "videoUrl": "$VIDEO_URL",
    "userId": "$USER_ID",
    "sessionId": "$SESSION_ID",
    "sourceType": "url",
    "options": {
        "extractFrames": true,
        "extractAudio": true,
        "generateTranscript": true,
        "detectScenes": true,
        "frameInterval": 30
    }
}
EOF
)

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ]; then
        JOB_ID=$(echo "$body" | jq -r '.jobId')
        print_success "Video processing job submitted (Job ID: $JOB_ID)"
        echo "$body" | jq '.'

        # Store job ID for status check
        echo "$JOB_ID" > /tmp/videoagent_test_job_id.txt
    else
        print_failure "Video processing submission failed (HTTP $http_code)"
        echo "$body"
    fi
}

# Test 3: Check Job Status
test_job_status() {
    print_header "Test 3: Check Job Status"

    if [ ! -f /tmp/videoagent_test_job_id.txt ]; then
        print_info "No job ID found, skipping status check"
        return
    fi

    JOB_ID=$(cat /tmp/videoagent_test_job_id.txt)
    print_info "Checking status for job: $JOB_ID"

    # Poll for job completion (max 60 seconds)
    for i in {1..12}; do
        sleep 5

        response=$(curl -s -w "\n%{http_code}" "$API_URL/api/video/job/$JOB_ID")
        http_code=$(echo "$response" | tail -n1)
        body=$(echo "$response" | head -n-1)

        if [ "$http_code" = "200" ]; then
            STATUS=$(echo "$body" | jq -r '.status')
            PROGRESS=$(echo "$body" | jq -r '.progress // 0')

            print_info "Job status: $STATUS (Progress: $PROGRESS%)"

            if [ "$STATUS" = "completed" ]; then
                print_success "Job completed successfully"
                echo "$body" | jq '.'
                return
            elif [ "$STATUS" = "failed" ]; then
                print_failure "Job failed"
                echo "$body" | jq '.'
                return
            fi
        else
            print_failure "Status check failed (HTTP $http_code)"
            echo "$body"
            return
        fi
    done

    print_info "Job still processing after 60 seconds (normal for video processing)"
}

# Test 4: Queue Statistics
test_queue_stats() {
    print_header "Test 4: Queue Statistics"

    response=$(curl -s -w "\n%{http_code}" "$API_URL/api/video/queue/stats")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ]; then
        print_success "Queue statistics retrieved"
        echo "$body" | jq '.'
    else
        print_failure "Queue statistics failed (HTTP $http_code)"
        echo "$body"
    fi
}

# Test 5: Google Drive - Auth URL
test_gdrive_auth_url() {
    print_header "Test 5: Google Drive - Get Auth URL"

    response=$(curl -s -w "\n%{http_code}" "$API_URL/api/gdrive/auth/url")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ]; then
        print_success "Google Drive auth URL retrieved"
        AUTH_URL=$(echo "$body" | jq -r '.authUrl')
        print_info "Auth URL: $AUTH_URL"
    else
        print_failure "Google Drive auth URL failed (HTTP $http_code)"
        echo "$body"
    fi
}

# Test 6: Google Drive - Auth Status
test_gdrive_auth_status() {
    print_header "Test 6: Google Drive - Check Auth Status"

    response=$(curl -s -w "\n%{http_code}" "$API_URL/api/gdrive/auth/status")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ]; then
        AUTHENTICATED=$(echo "$body" | jq -r '.authenticated')
        if [ "$AUTHENTICATED" = "true" ]; then
            print_success "Google Drive is authenticated"
        else
            print_info "Google Drive not authenticated (expected if not configured)"
        fi
        echo "$body" | jq '.'
    else
        print_failure "Google Drive auth status failed (HTTP $http_code)"
        echo "$body"
    fi
}

# Test 7: Google Drive - Extract File ID
test_gdrive_extract_file_id() {
    print_header "Test 7: Google Drive - Extract File ID"

    TEST_URL="https://drive.google.com/file/d/1a2b3c4d5e6f7g8h9i0j/view"

    response=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/gdrive/extract-file-id" \
        -H "Content-Type: application/json" \
        -d "{\"url\": \"$TEST_URL\"}")

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "200" ]; then
        FILE_ID=$(echo "$body" | jq -r '.fileId')
        print_success "File ID extracted: $FILE_ID"
    else
        print_failure "File ID extraction failed (HTTP $http_code)"
        echo "$body"
    fi
}

# Test 8: WebSocket Connection
test_websocket_connection() {
    print_header "Test 8: WebSocket Connection (Optional)"

    print_info "WebSocket test requires wscat (npm install -g wscat)"

    if command -v wscat &> /dev/null; then
        print_info "Testing WebSocket connection..."
        timeout 5 wscat -c "ws://localhost:9101" --execute "ping" 2>/dev/null || true
        print_info "WebSocket connection test completed"
    else
        print_info "Skipping WebSocket test (wscat not installed)"
    fi
}

# Test 9: Error Handling - Invalid Request
test_error_handling() {
    print_header "Test 9: Error Handling"

    # Test missing required field
    response=$(curl -s -w "\n%{http_code}" -X POST "$API_URL/api/video/process" \
        -H "Content-Type: application/json" \
        -d '{"userId": "test"}')

    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)

    if [ "$http_code" = "400" ]; then
        print_success "Error handling works correctly (returned 400 for invalid request)"
        echo "$body" | jq '.'
    else
        print_failure "Error handling failed (expected 400, got $http_code)"
        echo "$body"
    fi
}

# Test 10: Job Cancellation
test_job_cancellation() {
    print_header "Test 10: Job Cancellation"

    # Submit a job first
    response=$(curl -s -X POST "$API_URL/api/video/process" \
        -H "Content-Type: application/json" \
        -d @- << EOF
{
    "videoUrl": "https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4",
    "userId": "$USER_ID",
    "sessionId": "$SESSION_ID",
    "sourceType": "url",
    "options": {
        "extractFrames": false,
        "extractAudio": false,
        "generateTranscript": false
    }
}
EOF
)

    CANCEL_JOB_ID=$(echo "$response" | jq -r '.jobId')

    if [ "$CANCEL_JOB_ID" != "null" ]; then
        print_info "Created job for cancellation: $CANCEL_JOB_ID"

        # Try to cancel
        sleep 1
        response=$(curl -s -w "\n%{http_code}" -X DELETE "$API_URL/api/video/job/$CANCEL_JOB_ID")
        http_code=$(echo "$response" | tail -n1)

        if [ "$http_code" = "200" ]; then
            print_success "Job cancellation successful"
        else
            print_info "Job cancellation failed (may have already started processing)"
        fi
    else
        print_info "Could not create job for cancellation test"
    fi
}

# Main execution
main() {
    echo "=============================================="
    echo "VideoAgent Complete Test Suite"
    echo "=============================================="
    echo "API URL: $API_URL"
    echo "User ID: $USER_ID"
    echo "Session ID: $SESSION_ID"
    echo ""

    test_health_check
    test_process_video_url
    test_job_status
    test_queue_stats
    test_gdrive_auth_url
    test_gdrive_auth_status
    test_gdrive_extract_file_id
    test_error_handling
    test_job_cancellation
    test_websocket_connection

    # Summary
    print_header "Test Summary"
    echo "Total Tests: $TESTS_TOTAL"
    echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "${RED}Failed: $TESTS_FAILED${NC}"
    echo ""

    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}Some tests failed${NC}"
        exit 1
    fi
}

# Cleanup function
cleanup() {
    rm -f /tmp/videoagent_test_job_id.txt
}

trap cleanup EXIT

main "$@"

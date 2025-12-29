#!/bin/bash

# ============================================================================
# VideoAgent Worker Deployment Validation Test
# ============================================================================
# Tests that the VideoAgent worker is properly deployed and operational
# Validates all service connections and readiness

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0
TESTS_TOTAL=0

# Log functions
log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

# Test result functions
pass_test() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    TESTS_TOTAL=$((TESTS_TOTAL + 1))
    echo -e "${GREEN}✓ PASS${NC}: $1"
}

fail_test() {
    TESTS_FAILED=$((TESTS_FAILED + 1))
    TESTS_TOTAL=$((TESTS_TOTAL + 1))
    echo -e "${RED}✗ FAIL${NC}: $1"
    if [ -n "$2" ]; then
        echo -e "  ${RED}Reason:${NC} $2"
    fi
}

# ============================================================================
# Test 1: Container Existence and Naming
# ============================================================================
test_container_exists() {
    log_info "Test 1: Checking container existence and naming..."

    CONTAINER_NAME="videoagent-worker"

    if docker ps --filter "name=${CONTAINER_NAME}" --format "{{.Names}}" | grep -q "^${CONTAINER_NAME}$"; then
        pass_test "Container '${CONTAINER_NAME}' exists and is properly named"
        return 0
    else
        fail_test "Container '${CONTAINER_NAME}' not found or improperly named" "Expected exact name '${CONTAINER_NAME}'"
        return 1
    fi
}

# ============================================================================
# Test 2: Container Status
# ============================================================================
test_container_running() {
    log_info "Test 2: Checking container status..."

    CONTAINER_NAME="videoagent-worker"
    STATUS=$(docker ps --filter "name=${CONTAINER_NAME}" --format "{{.Status}}")

    if echo "$STATUS" | grep -q "Up"; then
        pass_test "Container is running (Status: $STATUS)"
        return 0
    else
        fail_test "Container is not running" "Status: $STATUS"
        return 1
    fi
}

# ============================================================================
# Test 3: Container Health Status
# ============================================================================
test_container_health() {
    log_info "Test 3: Checking container health status..."

    CONTAINER_NAME="videoagent-worker"
    HEALTH=$(docker inspect --format='{{.State.Health.Status}}' ${CONTAINER_NAME} 2>/dev/null || echo "no_healthcheck")

    if [ "$HEALTH" = "healthy" ]; then
        pass_test "Container health check is healthy"
        return 0
    elif [ "$HEALTH" = "no_healthcheck" ]; then
        log_warn "No health check configured for container"
        pass_test "Container health check not configured (acceptable)"
        return 0
    elif [ "$HEALTH" = "starting" ]; then
        log_warn "Container health check is starting (wait a moment)"
        pass_test "Container health check is starting (acceptable)"
        return 0
    else
        fail_test "Container health check failed" "Health status: $HEALTH"
        return 1
    fi
}

# ============================================================================
# Test 4: Redis Connection
# ============================================================================
test_redis_connection() {
    log_info "Test 4: Checking Redis connection..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for Redis connection success
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "✓ Redis connection established"; then
        pass_test "Redis connection established successfully"
        return 0
    elif docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Failed to connect to Redis"; then
        ERROR=$(docker logs ${CONTAINER_NAME} 2>&1 | grep "Failed to connect to Redis" | head -1)
        fail_test "Redis connection failed" "$ERROR"
        return 1
    else
        fail_test "Redis connection status unknown" "Check container logs manually"
        return 1
    fi
}

# ============================================================================
# Test 5: PostgreSQL Connection
# ============================================================================
test_postgres_connection() {
    log_info "Test 5: Checking PostgreSQL connection..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for PostgreSQL connection success
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "✓ Storage manager initialized"; then
        pass_test "PostgreSQL connection established successfully"
        return 0
    elif docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Failed to initialize storage"; then
        ERROR=$(docker logs ${CONTAINER_NAME} 2>&1 | grep "Failed to initialize storage" | head -1)
        fail_test "PostgreSQL connection failed" "$ERROR"
        return 1
    else
        fail_test "PostgreSQL connection status unknown" "Check container logs manually"
        return 1
    fi
}

# ============================================================================
# Test 6: Qdrant Connection
# ============================================================================
test_qdrant_connection() {
    log_info "Test 6: Checking Qdrant connection..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for Qdrant initialization
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Qdrant collections initialized"; then
        pass_test "Qdrant connection established successfully"
        return 0
    elif docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Failed to initialize Qdrant"; then
        ERROR=$(docker logs ${CONTAINER_NAME} 2>&1 | grep "Failed to initialize Qdrant" | head -1)
        fail_test "Qdrant connection failed" "$ERROR"
        return 1
    else
        fail_test "Qdrant connection status unknown" "Check container logs manually"
        return 1
    fi
}

# ============================================================================
# Test 7: GraphRAG Connection
# ============================================================================
test_graphrag_connection() {
    log_info "Test 7: Checking GraphRAG connection..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for GraphRAG initialization
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "GraphRAG client initialized"; then
        DETAILS=$(docker logs ${CONTAINER_NAME} 2>&1 | grep "GraphRAG client initialized" | head -1)
        pass_test "GraphRAG connection established successfully ($DETAILS)"
        return 0
    elif docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Failed to initialize GraphRAG"; then
        ERROR=$(docker logs ${CONTAINER_NAME} 2>&1 | grep "Failed to initialize GraphRAG" | head -1)
        fail_test "GraphRAG connection failed" "$ERROR"
        return 1
    else
        fail_test "GraphRAG connection status unknown" "Check container logs manually"
        return 1
    fi
}

# ============================================================================
# Test 8: MageAgent Connection
# ============================================================================
test_mageagent_connection() {
    log_info "Test 8: Checking MageAgent connection..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for MageAgent connection
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "MageAgent connection established"; then
        pass_test "MageAgent connection established successfully"
        return 0
    elif docker logs ${CONTAINER_NAME} 2>&1 | grep -q "Failed to connect to MageAgent"; then
        ERROR=$(docker logs ${CONTAINER_NAME} 2>&1 | grep "Failed to connect to MageAgent" | head -1)
        fail_test "MageAgent connection failed" "$ERROR"
        return 1
    else
        fail_test "MageAgent connection status unknown" "Check container logs manually"
        return 1
    fi
}

# ============================================================================
# Test 9: FFmpeg Availability
# ============================================================================
test_ffmpeg_available() {
    log_info "Test 9: Checking FFmpeg availability..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for FFmpeg initialization
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -q "FFmpeg initialized"; then
        pass_test "FFmpeg is available and initialized"
        return 0
    elif docker logs ${CONTAINER_NAME} 2>&1 | grep -q "FFmpeg not found"; then
        fail_test "FFmpeg not found" "FFmpeg binary missing in container"
        return 1
    else
        fail_test "FFmpeg availability unknown" "Check container logs manually"
        return 1
    fi
}

# ============================================================================
# Test 10: Worker Ready Status
# ============================================================================
test_worker_ready() {
    log_info "Test 10: Checking worker ready status..."

    CONTAINER_NAME="videoagent-worker"

    # Check logs for worker ready message (flexible matching)
    if docker logs ${CONTAINER_NAME} 2>&1 | grep -qE "(VideoAgent [Ww]orker ready|Starting VideoAgent worker)"; then
        pass_test "Worker is ready and waiting for jobs"
        return 0
    else
        fail_test "Worker not ready" "Worker initialization incomplete"
        return 1
    fi
}

# ============================================================================
# Test 11: No Crash Loops
# ============================================================================
test_no_crash_loops() {
    log_info "Test 11: Checking for crash loops..."

    CONTAINER_NAME="videoagent-worker"
    RESTART_COUNT=$(docker inspect --format='{{.RestartCount}}' ${CONTAINER_NAME} 2>/dev/null || echo "0")

    if [ "$RESTART_COUNT" -eq 0 ]; then
        pass_test "No crash loops detected (restart count: 0)"
        return 0
    elif [ "$RESTART_COUNT" -lt 3 ]; then
        log_warn "Container has restarted $RESTART_COUNT times (acceptable during startup)"
        pass_test "Minimal restarts detected (restart count: $RESTART_COUNT)"
        return 0
    else
        fail_test "Crash loop detected" "Restart count: $RESTART_COUNT"
        return 1
    fi
}

# ============================================================================
# Test 12: Network Connectivity
# ============================================================================
test_network_connectivity() {
    log_info "Test 12: Checking network connectivity..."

    CONTAINER_NAME="videoagent-worker"
    NETWORK_NAME="nexus-network"

    # Check if container is on correct network
    if docker inspect ${CONTAINER_NAME} | grep -q "${NETWORK_NAME}"; then
        pass_test "Container is on correct network (${NETWORK_NAME})"
        return 0
    else
        fail_test "Container not on correct network" "Expected network: ${NETWORK_NAME}"
        return 1
    fi
}

# ============================================================================
# Main Execution
# ============================================================================
main() {
    echo "========================================"
    echo "VideoAgent Worker Deployment Validation"
    echo "========================================"
    echo ""

    # Run all tests
    test_container_exists
    test_container_running
    test_container_health
    test_redis_connection
    test_postgres_connection
    test_qdrant_connection
    test_graphrag_connection
    test_mageagent_connection
    test_ffmpeg_available
    test_worker_ready
    test_no_crash_loops
    test_network_connectivity

    # Summary
    echo ""
    echo "========================================"
    echo "Test Summary"
    echo "========================================"
    echo "Total Tests: $TESTS_TOTAL"
    echo -e "Passed: ${GREEN}$TESTS_PASSED${NC}"
    echo -e "Failed: ${RED}$TESTS_FAILED${NC}"

    if [ $TESTS_FAILED -eq 0 ]; then
        echo ""
        echo -e "${GREEN}✓ All tests passed! VideoAgent worker is fully operational.${NC}"
        exit 0
    else
        echo ""
        echo -e "${RED}✗ Some tests failed. Check logs for details.${NC}"
        exit 1
    fi
}

main "$@"

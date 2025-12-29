#!/bin/bash
# ============================================================================
# VideoAgent Quick Test - Fast health check and basic functionality
# ============================================================================

set -e

API_URL="${VIDEOAGENT_API_URL:-http://localhost:9101}"

echo "VideoAgent Quick Test"
echo "====================="
echo ""

# Test 1: Health Check
echo "1. Health Check..."
if curl -sf "$API_URL/health" > /dev/null; then
    echo "   ✓ API is healthy"
else
    echo "   ✗ API is not responding"
    exit 1
fi

# Test 2: Queue Stats
echo ""
echo "2. Queue Statistics..."
STATS=$(curl -sf "$API_URL/api/video/queue/stats")
if [ $? -eq 0 ]; then
    echo "   ✓ Queue is accessible"
    echo "$STATS" | jq '.'
else
    echo "   ✗ Queue stats failed"
    exit 1
fi

# Test 3: Google Drive Auth Status
echo ""
echo "3. Google Drive Status..."
AUTH_STATUS=$(curl -sf "$API_URL/api/gdrive/auth/status")
if [ $? -eq 0 ]; then
    echo "   ✓ Google Drive endpoint is accessible"
    AUTHENTICATED=$(echo "$AUTH_STATUS" | jq -r '.authenticated')
    echo "   Authenticated: $AUTHENTICATED"
else
    echo "   ✗ Google Drive check failed"
fi

echo ""
echo "✓ All quick tests passed!"
echo ""
echo "Run ./test-videoagent-complete.sh for comprehensive testing"

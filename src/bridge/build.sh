#!/bin/bash
# VideoAgent Bridge Build Script
#
# This script builds the Docker image for the VideoAgent bridge service.
# It must be run from services/nexus-videoagent directory to include both
# the bridge/ and worker/ subdirectories in the build context.

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}VideoAgent Bridge Build Script${NC}"
echo ""

# Check we're in the correct directory
if [ ! -d "bridge" ] || [ ! -d "worker" ]; then
    echo -e "${RED}ERROR: Must run from services/nexus-videoagent directory${NC}"
    echo "Current directory: $(pwd)"
    exit 1
fi

# Generate build metadata
SERVICE="nexus-videoagent-bridge"
BUILD_ID="${SERVICE}-$(date +%Y%m%d)-$(openssl rand -hex 4)"
BUILD_TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
GIT_COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
GIT_BRANCH=$(git branch --show-current 2>/dev/null || echo "unknown")
VERSION="1.0.0"

echo -e "${YELLOW}Build Metadata:${NC}"
echo "  Build ID: ${BUILD_ID}"
echo "  Timestamp: ${BUILD_TIMESTAMP}"
echo "  Git Commit: ${GIT_COMMIT}"
echo "  Git Branch: ${GIT_BRANCH}"
echo "  Version: ${VERSION}"
echo ""

# Build Docker image
echo -e "${GREEN}Building Docker image...${NC}"
docker build \
  -f bridge/Dockerfile \
  --build-arg BUILD_ID="${BUILD_ID}" \
  --build-arg BUILD_TIMESTAMP="${BUILD_TIMESTAMP}" \
  --build-arg GIT_COMMIT="${GIT_COMMIT}" \
  --build-arg GIT_BRANCH="${GIT_BRANCH}" \
  --build-arg VERSION="${VERSION}" \
  -t ${SERVICE}:${BUILD_ID} \
  -t ${SERVICE}:latest \
  .

echo ""
echo -e "${GREEN}✓ Build complete!${NC}"
echo ""
echo "Images created:"
echo "  - ${SERVICE}:${BUILD_ID}"
echo "  - ${SERVICE}:latest"
echo ""
echo "To push to registry:"
echo "  docker tag ${SERVICE}:latest localhost:5000/${SERVICE}:latest"
echo "  docker push localhost:5000/${SERVICE}:latest"

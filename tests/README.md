# VideoAgent Testing Suite

Comprehensive testing suite for the VideoAgent microservice, including deployment validation, integration tests, and E2E processing tests.

## 📋 **Test Overview**

### ✅ **Available Tests**

| Test Script | Purpose | Status | Runtime |
|-------------|---------|--------|---------|
| `test-worker-deployment.sh` | Validates worker deployment and all service connections | ✅ **READY** | ~10s |
| `test-e2e-video-processing.sh` | End-to-end video processing validation framework | ⚠️ **Requires API** | ~5-10min |
| `redis-job-injector.sh` | Helper for injecting jobs into Redis queue | ⚠️ **Requires API** | ~1s |
| **`../testing/integration-test.sh`** | **Phase 5: 14 comprehensive integration tests** | ✅ **READY** | **~2-3min** |
| **`../testing/load-test.sh`** | **Phase 5: 7 load test scenarios** | ✅ **READY** | **~5-10min** |

---

## 🚀 **Quick Start**

### **1. Deployment Validation Test (Fully Functional)**

Tests that the VideoAgent worker is properly deployed and all services are connected.

```bash
# Run deployment validation
./test-worker-deployment.sh
```

**Tests Performed** (12 total):
- ✅ Container existence and naming
- ✅ Container running status
- ✅ Container health status
- ✅ Redis connection
- ✅ PostgreSQL connection
- ✅ Qdrant connection (1024-D embeddings)
- ✅ GraphRAG connection (VoyageAI voyage-3)
- ✅ MageAgent connection
- ✅ FFmpeg availability
- ✅ Worker ready status
- ✅ No crash loops
- ✅ Network connectivity

**Expected Output:**
```
========================================
VideoAgent Worker Deployment Validation
========================================

✓ PASS: Container 'videoagent-worker' exists and is properly named
✓ PASS: Container is running (Status: Up About a minute (healthy))
✓ PASS: Container health check is healthy
... (12 tests total)

========================================
Test Summary
========================================
Total Tests: 12
Passed: 12
Failed: 0

✓ All tests passed! VideoAgent worker is fully operational.
```

---

## ⚠️ **Current Limitations**

### **E2E Testing Requires VideoAgent API**

The `test-e2e-video-processing.sh` and `redis-job-injector.sh` scripts **cannot function without the VideoAgent API** because:

1. **Asynq Queue Format**: The Go worker uses Asynq library which stores jobs in Redis using MessagePack binary format
2. **Cannot Inject Plain JSON**: Direct Redis `LPUSH` with JSON payloads is not compatible with Asynq
3. **Requires Asynq Client**: Proper job injection requires using the Asynq Go client library (which the API uses)

### **Solution: Deploy VideoAgent API First**

To run E2E tests:
```bash
# 1. Deploy VideoAgent API
docker-compose -p nexus -f docker/docker-compose.nexus.yml up -d nexus-videoagent-api

# 2. Wait for API to be healthy
docker ps | grep videoagent-api

# 3. Use API to submit jobs
curl -X POST http://localhost:9101/api/videos/process \
  -H "Content-Type: application/json" \
  -d '{
    "videoUrl": "https://test-videos.co.uk/vids/bigbuckbunny/mp4/h264/360/Big_Buck_Bunny_360_10s_1MB.mp4",
    "userId": "test-user",
    "options": {
      "extractFrames": true,
      "extractAudio": true,
      "extractScenes": true,
      "generateEmbeddings": true
    }
  }'

# 4. Monitor processing
docker logs -f videoagent-worker
```

---

## 📊 **Test Results**

### **Deployment Validation (Current Status)**

Last run: 2025-10-29
```
Status: ✅ ALL TESTS PASSING (12/12)

Container: videoagent-worker
Architecture: amd64/x86_64
Health: healthy
Services Connected:
  ✓ Redis
  ✓ PostgreSQL (nexus_videoagent database)
  ✓ Qdrant (video_embeddings, scene_embeddings @ 1024-D)
  ✓ GraphRAG (VoyageAI voyage-3, 1024-D)
  ✓ MageAgent
  ✓ FFmpeg (CPU-only)

Worker Status:
  ✓ Ready and waiting for jobs
  ✓ Concurrency: 3 workers
  ✓ Zero crash loops
```

---

## 🧪 **Detailed Test Descriptions**

### **test-worker-deployment.sh**

**Purpose**: Comprehensive deployment validation

**Test Cases**:

1. **Container Existence** - Verifies container `videoagent-worker` exists with correct name
2. **Container Status** - Confirms container is running (not stopped/restarting)
3. **Health Check** - Validates Docker health check passes
4. **Redis Connection** - Checks `✓ Redis connection established` in logs
5. **PostgreSQL Connection** - Verifies `✓ Storage manager initialized` in logs
6. **Qdrant Connection** - Confirms `✓ Qdrant collections initialized` in logs
7. **GraphRAG Connection** - Validates GraphRAG client initialized with VoyageAI voyage-3
8. **MageAgent Connection** - Checks `✓ MageAgent connection established` in logs
9. **FFmpeg Availability** - Verifies `✓ FFmpeg initialized` in logs
10. **Worker Ready** - Confirms worker is ready and waiting for jobs
11. **No Crash Loops** - Ensures restart count is 0 (no repeated crashes)
12. **Network Connectivity** - Validates container is on `nexus-network`

**Exit Codes**:
- `0` - All tests passed
- `1` - One or more tests failed

**Usage**:
```bash
# Run all tests
./test-worker-deployment.sh

# Run and save output
./test-worker-deployment.sh > deployment-validation-$(date +%Y%m%d).log 2>&1
```

---

### **test-e2e-video-processing.sh** ⚠️

**Purpose**: End-to-end video processing validation

**Test Flow**:
1. Check prerequisites (all containers running)
2. Inject test video job into Redis queue
3. Monitor job processing (5-minute timeout)
4. Validate PostgreSQL results (frames, scenes, audio)
5. Validate Qdrant embeddings (1024-D vectors)
6. Display processing summary

**Current Status**: ⚠️ **BLOCKED** - Requires VideoAgent API for proper job injection

**Workaround**: Use VideoAgent API directly once deployed

---

### **redis-job-injector.sh** ⚠️

**Purpose**: Helper script to inject video processing jobs directly into Redis

**Current Status**: ⚠️ **BLOCKED** - Asynq requires MessagePack format

**Usage** (when API is available):
```bash
# Process a video
./redis-job-injector.sh --url "https://example.com/video.mp4"

# Custom user ID
./redis-job-injector.sh --url "https://example.com/video.mp4" --user-id "user123"

# High priority
./redis-job-injector.sh --url "https://example.com/video.mp4" --priority 9
```

**Parameters**:
- `-u, --url VIDEO_URL` - Video URL to process (required)
- `-i, --user-id USER_ID` - User ID for the job (default: test-user-TIMESTAMP)
- `-q, --queue QUEUE_NAME` - Queue name (default: videoagent:default)
- `-p, --priority PRIORITY` - Job priority 1-10 (default: 5)
- `-h, --help` - Show help message

---

## 🔧 **Troubleshooting**

### **Test Failures**

#### **Container Not Running**
```bash
# Check container status
docker ps -a | grep videoagent

# Restart container
docker-compose -p nexus -f docker/docker-compose.nexus.yml up -d nexus-videoagent-worker

# Check logs
docker logs videoagent-worker
```

#### **Health Check Failing**
```bash
# Check health status
docker inspect videoagent-worker --format='{{.State.Health.Status}}'

# View health check logs
docker inspect videoagent-worker --format='{{json .State.Health}}' | python3 -m json.tool

# Common causes:
# - Process not running (check: docker exec videoagent-worker ps aux)
# - Health check command incorrect (fixed in latest version)
```

#### **Service Connection Failures**
```bash
# Check Redis
docker exec nexus-redis redis-cli PING

# Check PostgreSQL
docker exec nexus-postgres psql -U unified_nexus -d nexus_videoagent -c "SELECT 1;"

# Check Qdrant
curl http://localhost:6333/collections

# Check GraphRAG
curl http://localhost:9090/health

# Check network
docker network inspect nexus-network
```

---

## 📚 **Architecture**

### **VideoAgent Worker Components**

```
videoagent-worker (Go)
├── FFmpeg Integration
│   └── Video extraction, frame capture
├── Storage Layer
│   ├── PostgreSQL (jobs, frames, scenes, audio)
│   └── Qdrant (vector embeddings @ 1024-D)
├── AI Services
│   ├── MageAgent (vision analysis)
│   └── GraphRAG (VoyageAI voyage-3 embeddings)
├── Queue System
│   └── Asynq (Redis-backed job queue)
└── Similarity Module
    ├── Video Embedder (1024-D)
    ├── Scene Embedder (1024-D)
    └── Search API
```

### **Data Flow**

```
1. Job Injection (via API)
   ↓
2. Redis Queue (Asynq MessagePack format)
   ↓
3. Worker Processing
   ├→ Download video
   ├→ Extract metadata (FFmpeg)
   ├→ Extract frames (FFmpeg)
   ├→ Analyze frames (MageAgent vision)
   ├→ Generate embeddings (GraphRAG VoyageAI)
   ├→ Detect scenes
   ├→ Extract audio
   └→ Audio analysis
   ↓
4. Storage
   ├→ PostgreSQL (structured data)
   └→ Qdrant (vector embeddings)
   ↓
5. Job Complete
```

---

## 🎯 **Next Steps**

### **Short Term**
1. ✅ **DONE**: Deployment validation test suite (12 tests)
2. ✅ **DONE**: Architecture fix (ARM64 → amd64)
3. ✅ **DONE**: Health check fix
4. ⏳ **PENDING**: Deploy VideoAgent API
5. ⏳ **PENDING**: Run E2E tests with real video

### **Future Enhancements**
- [ ] Unit tests for Go components
- [ ] Integration tests for each service
- [ ] Performance benchmarks
- [ ] Load testing (concurrent video processing)
- [ ] CI/CD integration (GitHub Actions)
- [ ] Automated nightly test runs
- [ ] Test result dashboards

---

## 📝 **Test Logs**

Test results are logged to stdout. To save logs:

```bash
# Deployment validation
./test-worker-deployment.sh > logs/deployment-$(date +%Y%m%d-%H%M%S).log 2>&1

# E2E test (when API available)
./test-e2e-video-processing.sh > logs/e2e-$(date +%Y%m%d-%H%M%S).log 2>&1
```

---

## 🤝 **Contributing**

To add new tests:

1. Create test script in `services/videoagent/tests/`
2. Make executable: `chmod +x test-script.sh`
3. Follow naming convention: `test-<purpose>.sh`
4. Use color-coded output (GREEN/RED/YELLOW)
5. Return exit code 0 for success, 1 for failure
6. Update this README with test description

---

## 📄 **License**

Same as parent project.

---

## 🙋 **Support**

- Issues: GitHub Issues
- Logs: `docker logs videoagent-worker`
- Health: `docker ps | grep videoagent`
- Architecture: This README, Section "Architecture"

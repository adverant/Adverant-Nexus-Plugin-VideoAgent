# VideoAgent Technical Specification

Complete technical reference for integrating the VideoAgent plugin into your applications.

---

## API Reference

### Base URL

```
https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video
```

All endpoints require authentication via Bearer token in the Authorization header.

---

### Endpoints

#### Create Project

```http
POST /projects
```

Creates a new video project for processing.

**Request Body:**
```json
{
  "name": "string",
  "description": "string (optional)",
  "sourceVideos": [
    {
      "url": "string",
      "title": "string (optional)"
    }
  ],
  "webhookUrl": "string (optional)",
  "settings": {
    "defaultLanguage": "en",
    "enableTranscription": true,
    "enableSceneDetection": true
  }
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "projectId": "proj_abc123",
    "name": "string",
    "status": "created",
    "sourceVideos": [
      {
        "id": "vid_xyz789",
        "url": "string",
        "status": "pending",
        "metadata": null
      }
    ],
    "createdAt": "2024-01-15T10:30:00Z"
  }
}
```

---

#### Generate Video Content

```http
POST /projects/:id/generate
```

Starts video generation based on project settings and source videos.

**Request Body:**
```json
{
  "outputType": "edited | highlights | clips | full",
  "settings": {
    "targetDuration": 60,
    "aspectRatio": "16:9 | 9:16 | 1:1",
    "resolution": "1080p | 4k",
    "style": "professional | casual | cinematic",
    "includeCaptions": true,
    "captionStyle": "bottom | dynamic | burned"
  },
  "highlightConfig": {
    "count": 5,
    "minDuration": 10,
    "maxDuration": 60,
    "criteria": ["engagement", "sentiment", "action"]
  }
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "generationId": "gen_def456",
    "projectId": "proj_abc123",
    "status": "queued",
    "estimatedDuration": 300,
    "websocketUrl": "wss://api.adverant.ai/proxy/nexus-videoagent/ws/generation/gen_def456"
  }
}
```

---

#### Transcribe Video

```http
POST /transcribe
```

Transcribes audio from video with speaker diarization.

**Request Body:**
```json
{
  "videoUrl": "string",
  "language": "en | auto",
  "options": {
    "speakerDiarization": true,
    "maxSpeakers": 10,
    "wordTimestamps": true,
    "customVocabulary": ["string"],
    "punctuate": true
  }
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "jobId": "trans_ghi789",
    "status": "processing",
    "estimatedTime": 120
  }
}
```

#### Get Transcription Results

```http
GET /transcribe/:jobId
```

**Response:**
```json
{
  "success": true,
  "data": {
    "jobId": "trans_ghi789",
    "status": "completed",
    "duration": 3600,
    "language": "en",
    "transcript": {
      "text": "Full transcript text...",
      "segments": [
        {
          "start": 0.0,
          "end": 5.2,
          "text": "Hello everyone.",
          "speaker": "Speaker 1",
          "confidence": 0.95,
          "words": [
            { "word": "Hello", "start": 0.0, "end": 0.5, "confidence": 0.98 },
            { "word": "everyone", "start": 0.6, "end": 1.2, "confidence": 0.94 }
          ]
        }
      ],
      "speakers": [
        {
          "id": "Speaker 1",
          "segments": [0, 2, 4, 6],
          "totalDuration": 1800
        }
      ]
    }
  }
}
```

---

#### Distribute to Platforms

```http
POST /distribute
```

Distributes video content to connected platforms.

**Request Body:**
```json
{
  "projectId": "proj_abc123",
  "outputId": "out_xyz789",
  "platforms": [
    {
      "platform": "youtube | linkedin | vimeo | tiktok | wistia",
      "settings": {
        "title": "string",
        "description": "string",
        "tags": ["string"],
        "visibility": "public | unlisted | private",
        "scheduledAt": "2024-01-20T10:00:00Z"
      }
    }
  ]
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "distributionId": "dist_jkl012",
    "status": "processing",
    "platforms": [
      {
        "platform": "youtube",
        "status": "uploading",
        "progress": 0
      }
    ]
  }
}
```

---

#### Get Project Details

```http
GET /projects/:id
```

**Response:**
```json
{
  "success": true,
  "data": {
    "projectId": "proj_abc123",
    "name": "string",
    "status": "completed",
    "sourceVideos": [
      {
        "id": "vid_xyz789",
        "url": "string",
        "status": "ready",
        "metadata": {
          "duration": 3600,
          "resolution": "1920x1080",
          "codec": "h264",
          "fileSize": 524288000
        }
      }
    ],
    "outputs": [
      {
        "id": "out_xyz789",
        "type": "final-video",
        "url": "https://cdn.adverant.ai/...",
        "duration": 87,
        "resolution": "1920x1080",
        "format": "mp4",
        "fileSize": 52428800,
        "createdAt": "2024-01-15T14:30:00Z"
      }
    ],
    "transcription": {
      "status": "completed",
      "jobId": "trans_ghi789"
    },
    "minutesUsed": 8.5,
    "createdAt": "2024-01-15T10:30:00Z",
    "updatedAt": "2024-01-15T14:30:00Z"
  }
}
```

---

#### Search Video Content

```http
POST /search
```

Semantic search across transcribed video content.

**Request Body:**
```json
{
  "query": "product announcement",
  "projectIds": ["proj_abc123"],
  "filters": {
    "speaker": "Speaker 1",
    "dateRange": {
      "start": "2024-01-01",
      "end": "2024-01-31"
    }
  },
  "limit": 20,
  "offset": 0
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "results": [
      {
        "projectId": "proj_abc123",
        "videoId": "vid_xyz789",
        "timestamp": 125.5,
        "duration": 30,
        "text": "We're excited to announce our new product...",
        "speaker": "Speaker 1",
        "relevanceScore": 0.95,
        "thumbnailUrl": "https://..."
      }
    ],
    "pagination": {
      "total": 45,
      "limit": 20,
      "offset": 0
    }
  }
}
```

---

#### Generate Highlights

```http
POST /highlights
```

Automatically generates highlight clips from video.

**Request Body:**
```json
{
  "videoUrl": "string",
  "count": 5,
  "criteria": ["engagement", "sentiment", "action", "topic_change"],
  "minDuration": 10,
  "maxDuration": 60,
  "format": "mp4 | webm",
  "resolution": "1080p | 720p | 480p"
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "jobId": "high_mno345",
    "status": "processing",
    "estimatedTime": 180
  }
}
```

---

#### Live Stream Analysis

```http
POST /streams
```

Starts real-time analysis of a live stream.

**Request Body:**
```json
{
  "streamUrl": "rtmp://... | https://...m3u8",
  "streamType": "rtmp | hls | rtsp | webrtc",
  "features": ["transcription", "scene_detection", "object_detection"],
  "webhookUrl": "string",
  "duration": 3600
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "streamId": "stream_pqr678",
    "status": "connecting",
    "websocketUrl": "wss://api.adverant.ai/proxy/nexus-videoagent/ws/stream/stream_pqr678"
  }
}
```

---

## Authentication

### Bearer Token

All API requests require authentication via the Nexus API Gateway.

```bash
curl -X GET "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/projects" \
  -H "Authorization: Bearer YOUR_NEXUS_API_TOKEN"
```

### Token Scopes

| Scope | Description |
|-------|-------------|
| `video:read` | Read project details, list videos |
| `video:write` | Create projects, upload videos |
| `video:generate` | Start video generation |
| `video:transcribe` | Transcribe video content |
| `video:distribute` | Distribute to external platforms |
| `video:stream` | Live stream analysis |

---

## Rate Limits

Rate limits vary by pricing tier:

| Tier | Requests/Minute | Concurrent Jobs | Minutes/Month |
|------|-----------------|-----------------|---------------|
| Starter | 30 | 1 | 60 |
| Creator | 60 | 3 | 300 |
| Studio | 120 | 10 | Unlimited |

### Rate Limit Headers

```http
X-RateLimit-Limit: 60
X-RateLimit-Remaining: 45
X-RateLimit-Reset: 1705312200
```

---

## Data Models

### Project

```typescript
interface VideoProject {
  projectId: string;
  name: string;
  description?: string;
  status: ProjectStatus;
  sourceVideos: SourceVideo[];
  outputs: VideoOutput[];
  transcription?: TranscriptionJob;
  settings: ProjectSettings;
  webhookUrl?: string;
  minutesUsed: number;
  createdAt: string;
  updatedAt: string;
}

type ProjectStatus = 'created' | 'ingesting' | 'ready' |
                     'processing' | 'completed' | 'failed';
```

### Source Video

```typescript
interface SourceVideo {
  id: string;
  url: string;
  title?: string;
  status: VideoStatus;
  metadata?: VideoMetadata;
}

interface VideoMetadata {
  duration: number;           // seconds
  resolution: string;         // "1920x1080"
  codec: string;              // "h264"
  frameRate: number;          // 30
  bitrate: number;            // bps
  fileSize: number;           // bytes
  audioChannels: number;
  audioCodec: string;
}

type VideoStatus = 'pending' | 'uploading' | 'processing' | 'ready' | 'failed';
```

### Video Output

```typescript
interface VideoOutput {
  id: string;
  type: OutputType;
  url: string;
  duration: number;
  resolution: string;
  format: string;
  fileSize: number;
  createdAt: string;
  expiresAt?: string;
}

type OutputType = 'final-video' | 'highlight' | 'clip' |
                  'thumbnail' | 'transcript';
```

### Transcription

```typescript
interface Transcription {
  text: string;
  segments: TranscriptSegment[];
  speakers: Speaker[];
  language: string;
  duration: number;
}

interface TranscriptSegment {
  start: number;
  end: number;
  text: string;
  speaker?: string;
  confidence: number;
  words?: Word[];
}

interface Word {
  word: string;
  start: number;
  end: number;
  confidence: number;
}

interface Speaker {
  id: string;
  segments: number[];
  totalDuration: number;
}
```

### Highlight

```typescript
interface Highlight {
  id: string;
  start: number;
  end: number;
  duration: number;
  score: number;
  reason: string;
  thumbnailUrl: string;
  clipUrl?: string;
}
```

---

## SDK Integration

### JavaScript/TypeScript SDK

```typescript
import { NexusClient } from '@nexus/sdk';

const nexus = new NexusClient({
  apiKey: process.env.NEXUS_API_KEY,
});

// Create a video project
const project = await nexus.video.createProject({
  name: 'Q4 Earnings Call',
  sourceVideos: [
    { url: 'https://storage.example.com/earnings.mp4' }
  ],
  settings: {
    enableTranscription: true,
    enableSceneDetection: true,
  },
});

// Wait for ingestion
await nexus.video.waitForReady(project.projectId);

// Get transcription
const transcript = await nexus.video.getTranscription(project.projectId);
console.log(`Speakers: ${transcript.speakers.length}`);

// Generate highlights
const highlights = await nexus.video.generateHighlights(project.projectId, {
  count: 5,
  criteria: ['engagement', 'sentiment'],
});

// Monitor progress via WebSocket
nexus.video.onProgress(project.projectId, (event) => {
  console.log(`${event.type}: ${event.progress}%`);
});

// Distribute to YouTube
const distribution = await nexus.video.distribute(project.projectId, {
  platforms: [
    {
      platform: 'youtube',
      settings: {
        title: 'Q4 2024 Earnings Call',
        visibility: 'unlisted',
      },
    },
  ],
});
```

### Python SDK

```python
from nexus import NexusClient

client = NexusClient(api_key=os.environ["NEXUS_API_KEY"])

# Create project
project = client.video.create_project(
    name="Q4 Earnings Call",
    source_videos=[
        {"url": "https://storage.example.com/earnings.mp4"}
    ],
    settings={
        "enable_transcription": True,
        "enable_scene_detection": True
    }
)

# Wait for processing
client.video.wait_for_ready(project.project_id)

# Get transcription
transcript = client.video.get_transcription(project.project_id)
print(f"Duration: {transcript.duration}s, Speakers: {len(transcript.speakers)}")

# Generate highlights
def on_progress(event):
    print(f"{event['type']}: {event['progress']}%")

highlights = client.video.generate_highlights(
    project.project_id,
    count=5,
    criteria=["engagement", "sentiment"],
    on_progress=on_progress
)

# Distribute
distribution = client.video.distribute(
    project.project_id,
    platforms=[{
        "platform": "youtube",
        "settings": {"title": "Q4 2024 Earnings Call", "visibility": "unlisted"}
    }]
)
```

---

## WebSocket API

### Connection

```javascript
const ws = new WebSocket('wss://api.adverant.ai/proxy/nexus-videoagent/ws');

ws.onopen = () => {
  // Authenticate
  ws.send(JSON.stringify({
    type: 'auth',
    token: 'YOUR_API_TOKEN'
  }));
};
```

### Subscribe to Project Updates

```javascript
// Subscribe to project events
ws.send(JSON.stringify({
  type: 'subscribe',
  channel: 'project',
  projectId: 'proj_abc123'
}));

// Handle messages
ws.onmessage = (event) => {
  const message = JSON.parse(event.data);

  switch (message.type) {
    case 'ingestion:progress':
      console.log(`Ingesting: ${message.progress}%`);
      break;
    case 'transcription:segment':
      console.log(`${message.speaker}: ${message.text}`);
      break;
    case 'generation:progress':
      console.log(`Generating: ${message.progress}%`);
      break;
    case 'generation:complete':
      console.log(`Output ready: ${message.outputUrl}`);
      break;
    case 'distribution:complete':
      console.log(`Published to ${message.platform}: ${message.url}`);
      break;
  }
};
```

### Message Types

| Type | Direction | Description |
|------|-----------|-------------|
| `auth` | Client → Server | Authenticate connection |
| `subscribe` | Client → Server | Subscribe to project |
| `unsubscribe` | Client → Server | Unsubscribe |
| `ingestion:progress` | Server → Client | Video upload progress |
| `transcription:progress` | Server → Client | Transcription progress |
| `transcription:segment` | Server → Client | Real-time transcript |
| `generation:progress` | Server → Client | Generation progress |
| `generation:complete` | Server → Client | Output ready |
| `highlight:found` | Server → Client | Highlight detected |
| `distribution:progress` | Server → Client | Upload progress |
| `distribution:complete` | Server → Client | Platform upload done |
| `error` | Server → Client | Error notification |

---

## Error Handling

### Error Response Format

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable error description",
    "details": {},
    "requestId": "req_abc123"
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|------|-------------|-------------|
| `INVALID_REQUEST` | 400 | Malformed request body |
| `INVALID_VIDEO_URL` | 400 | Video URL unreachable or invalid |
| `UNSUPPORTED_FORMAT` | 400 | Video format not supported |
| `AUTHENTICATION_REQUIRED` | 401 | Missing or invalid token |
| `INSUFFICIENT_PERMISSIONS` | 403 | Token lacks required scope |
| `PROJECT_NOT_FOUND` | 404 | Project does not exist |
| `RATE_LIMIT_EXCEEDED` | 429 | Too many requests |
| `QUOTA_EXCEEDED` | 402 | Monthly minutes exceeded |
| `PROCESSING_FAILED` | 500 | Video processing error |
| `TRANSCRIPTION_FAILED` | 500 | Speech recognition error |
| `DISTRIBUTION_FAILED` | 500 | Platform upload error |
| `PLATFORM_UNAVAILABLE` | 503 | External platform down |

---

## Deployment Requirements

### Container Specifications

| Resource | Minimum | Recommended |
|----------|---------|-------------|
| CPU | 2000m | 4000m |
| Memory | 4Gi | 8Gi |
| Storage | 50Gi | 100Gi |
| GPU | Optional | 1x NVIDIA A100 |
| Timeout | 1 hour | 4 hours |

### Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `NEXUS_API_KEY` | Yes | Nexus platform API key |
| `DATABASE_URL` | Yes | PostgreSQL connection string |
| `REDIS_URL` | Yes | Redis for job queue |
| `STORAGE_PATH` | Yes | Path for video storage |
| `CDN_BUCKET` | Yes | CDN storage bucket |
| `YOUTUBE_CLIENT_ID` | No | YouTube OAuth client |
| `YOUTUBE_CLIENT_SECRET` | No | YouTube OAuth secret |
| `LINKEDIN_CLIENT_ID` | No | LinkedIn OAuth client |
| `LINKEDIN_CLIENT_SECRET` | No | LinkedIn OAuth secret |

### Health Checks

```yaml
livenessProbe:
  httpGet:
    path: /live
    port: 8080
  initialDelaySeconds: 30
  periodSeconds: 10

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 5
```

---

## Supported Formats

### Video Input Formats

| Format | Container | Codecs | Max Resolution |
|--------|-----------|--------|----------------|
| MP4 | MPEG-4 | H.264, H.265 | 8K |
| MOV | QuickTime | H.264, ProRes | 8K |
| AVI | AVI | Various | 4K |
| MKV | Matroska | H.264, H.265, VP9 | 8K |
| WebM | WebM | VP8, VP9 | 4K |
| FLV | Flash | H.264 | 1080p |
| WMV | ASF | WMV | 1080p |
| M4V | MPEG-4 | H.264 | 4K |

### Audio Formats

| Format | Description |
|--------|-------------|
| MP3 | MPEG Audio Layer 3 |
| WAV | Waveform Audio |
| AAC | Advanced Audio Coding |
| FLAC | Free Lossless Audio |
| OGG | Ogg Vorbis |
| M4A | MPEG-4 Audio |

### Live Stream Protocols

| Protocol | Description |
|----------|-------------|
| RTMP | Real-Time Messaging Protocol |
| HLS | HTTP Live Streaming |
| RTSP | Real-Time Streaming Protocol |
| WebRTC | Web Real-Time Communication |

---

## Quotas and Limits

### Per-Tier Limits

| Limit | Starter | Creator | Studio |
|-------|---------|---------|--------|
| Minutes/Month | 60 | 300 | Unlimited |
| Max Video Duration | 30 min | 2 hours | 8 hours |
| Max File Size | 2 GB | 10 GB | 50 GB |
| Max Resolution | 1080p | 4K | 8K |
| Concurrent Projects | 2 | 5 | 20 |
| Concurrent Jobs | 1 | 3 | 10 |
| Highlight Count | 3/video | 10/video | Unlimited |
| Platform Connections | 1 | 3 | Unlimited |

### Checking Quota

```http
GET /account/quota
```

**Response:**
```json
{
  "success": true,
  "data": {
    "tier": "creator",
    "periodStart": "2024-01-01T00:00:00Z",
    "periodEnd": "2024-02-01T00:00:00Z",
    "minutes": {
      "used": 145.5,
      "limit": 300,
      "remaining": 154.5
    },
    "storage": {
      "usedGB": 45.2,
      "limitGB": 100
    },
    "concurrentJobs": {
      "active": 2,
      "limit": 3
    }
  }
}
```

---

## Processing Specifications

### Transcription Accuracy

| Model | Languages | Word Error Rate | Latency |
|-------|-----------|-----------------|---------|
| Standard | 50+ | 5% WER | 0.3x realtime |
| Premium | 100+ | 2% WER | 0.5x realtime |

### Speaker Diarization

- **Accuracy**: 92% DER (Diarization Error Rate)
- **Max Speakers**: 10 per video
- **Language Agnostic**: Works with any language

### Video Processing

| Operation | Speed | GPU Required |
|-----------|-------|--------------|
| Ingestion | 2x realtime | No |
| Transcription | 0.3x realtime | Optional |
| Scene Detection | 10x realtime | Yes |
| Highlight Generation | 1x realtime | Yes |
| Encoding (H.264) | 2x realtime | Optional |
| Encoding (H.265) | 1x realtime | Optional |

---

## Platform Integration

### Supported Platforms

| Platform | Features | OAuth Required |
|----------|----------|----------------|
| YouTube | Upload, scheduling, captions | Yes |
| LinkedIn | Upload, articles | Yes |
| Vimeo | Upload, privacy settings | Yes |
| TikTok | Upload, scheduled posting | Yes |
| Wistia | Upload, analytics | API Key |

### Connecting Platforms

```http
POST /platforms/connect
```

```json
{
  "platform": "youtube",
  "returnUrl": "https://your-app.com/oauth/callback"
}
```

Returns OAuth authorization URL for user to authenticate.

---

## Support

- **Documentation**: [docs.adverant.ai/plugins/videoagent](https://docs.adverant.ai/plugins/videoagent)
- **API Status**: [status.adverant.ai](https://status.adverant.ai)
- **Support Email**: support@adverant.ai
- **Discord**: [discord.gg/adverant](https://discord.gg/adverant)

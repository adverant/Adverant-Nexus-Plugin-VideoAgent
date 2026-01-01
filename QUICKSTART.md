# VideoAgent Quick Start Guide

Get up and running with VideoAgent - AI Video Production and Analysis in under 15 minutes. This guide walks you through installation, configuration, and creating your first AI-powered video project.

## Prerequisites

Before you begin, ensure you have:

- An active Nexus platform account with API access
- A valid API key from the [Nexus Dashboard](https://dashboard.adverant.ai/settings/api-keys)
- An active subscription tier (Starter, Creator, or Studio)
- Video files accessible via URL or ready for upload

## Installation

### Via Nexus Marketplace (Recommended)

The simplest way to install VideoAgent is through the Nexus CLI:

```bash
nexus plugin install nexus-videoagent
```

### Via API

Alternatively, install programmatically:

```bash
curl -X POST "https://api.adverant.ai/v1/plugins/install" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c" \
  -H "Content-Type: application/json" \
  -d '{
    "pluginId": "nexus-videoagent",
    "tier": "creator"
  }'
```

### Verify Installation

Confirm VideoAgent is installed and ready:

```bash
nexus plugin status nexus-videoagent
```

Expected output:
```
Plugin: nexus-videoagent
Status: active
Version: 1.0.0
Tier: creator
Minutes remaining: 300/300
```

## Configuration

### Environment Setup

Configure your API credentials. Create or update your `.env` file:

```bash
NEXUS_API_KEY=nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c
NEXUS_WORKSPACE_ID=ws_abc123def456
VIDEOAGENT_DEFAULT_LANGUAGE=en
VIDEOAGENT_OUTPUT_QUALITY=high
VIDEOAGENT_WEBHOOK_SECRET=whsec_your_webhook_secret
```

### SDK Initialization

Initialize the VideoAgent client in your application:

```typescript
import { NexusClient } from '@adverant/nexus-sdk';

const nexus = new NexusClient({
  apiKey: process.env.NEXUS_API_KEY,
  workspaceId: process.env.NEXUS_WORKSPACE_ID
});

const videoAgent = nexus.plugin('nexus-videoagent');
```

## Create Your First Video Project

Let us create a video project and generate AI-enhanced content to verify everything is working correctly.

### Step 1: Create a Project

```bash
curl -X POST "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/projects" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Q1 Product Launch Video",
    "description": "Marketing video for our new SaaS platform launch",
    "settings": {
      "defaultLanguage": "en",
      "outputResolution": "1080p",
      "frameRate": 30
    },
    "sourceVideos": [
      {
        "url": "https://storage.example.com/raw-footage/interview-ceo.mp4",
        "type": "interview",
        "label": "CEO Interview"
      },
      {
        "url": "https://storage.example.com/raw-footage/product-demo.mp4",
        "type": "demonstration",
        "label": "Product Demo"
      }
    ]
  }'
```

**Response:**
```json
{
  "projectId": "proj_9a8b7c6d5e4f3a2b",
  "name": "Q1 Product Launch Video",
  "status": "created",
  "createdAt": "2025-01-15T10:30:00Z",
  "sourceVideos": [
    {
      "id": "src_1a2b3c4d",
      "label": "CEO Interview",
      "status": "pending_upload",
      "duration": null
    },
    {
      "id": "src_5e6f7a8b",
      "label": "Product Demo",
      "status": "pending_upload",
      "duration": null
    }
  ],
  "estimatedProcessingMinutes": 12
}
```

### Step 2: Generate Video Content

Once source videos are uploaded, trigger AI-powered video generation:

```bash
curl -X POST "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/projects/proj_9a8b7c6d5e4f3a2b/generate" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c" \
  -H "Content-Type: application/json" \
  -d '{
    "outputType": "marketing-video",
    "duration": {
      "target": 90,
      "flexibility": "allow-shorter"
    },
    "style": {
      "pacing": "dynamic",
      "transitions": "modern",
      "colorGrading": "vibrant-corporate"
    },
    "audio": {
      "backgroundMusic": "upbeat-tech",
      "voiceoverEnhancement": true,
      "normalizeAudio": true
    },
    "branding": {
      "introLogo": "https://cdn.example.com/logo-animation.mp4",
      "outroSlate": "https://cdn.example.com/outro-template.mp4",
      "lowerThirds": true
    }
  }'
```

**Response:**
```json
{
  "generationId": "gen_7f6e5d4c3b2a1098",
  "projectId": "proj_9a8b7c6d5e4f3a2b",
  "status": "processing",
  "estimatedTimeSeconds": 480,
  "stages": [
    {"name": "analysis", "status": "in_progress"},
    {"name": "editing", "status": "pending"},
    {"name": "rendering", "status": "pending"},
    {"name": "encoding", "status": "pending"}
  ]
}
```

### Step 3: Check Generation Status

Poll for completion or use webhooks:

```bash
curl "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/projects/proj_9a8b7c6d5e4f3a2b" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c"
```

**Completed Response:**
```json
{
  "projectId": "proj_9a8b7c6d5e4f3a2b",
  "status": "completed",
  "outputs": [
    {
      "id": "out_abc123def",
      "type": "final-video",
      "url": "https://cdn.adverant.ai/videoagent/proj_9a8b7c6d5e4f3a2b/final_1080p.mp4",
      "format": "mp4",
      "resolution": "1920x1080",
      "duration": 87,
      "sizeBytes": 156847293
    }
  ],
  "minutesUsed": 8.5,
  "processingTimeSeconds": 423
}
```

## Transcribe a Video

VideoAgent provides industry-leading transcription with speaker identification:

```bash
curl -X POST "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/transcribe" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c" \
  -H "Content-Type: application/json" \
  -d '{
    "videoUrl": "https://storage.example.com/meeting-recording.mp4",
    "language": "en",
    "options": {
      "speakerDiarization": true,
      "maxSpeakers": 4,
      "punctuation": true,
      "wordTimestamps": true,
      "formatOutput": "srt"
    }
  }'
```

**Response:**
```json
{
  "transcriptionId": "trx_4d5e6f7a8b9c0d1e",
  "status": "processing",
  "estimatedTimeSeconds": 120,
  "videoInfo": {
    "duration": 1847,
    "detectedLanguage": "en-US"
  }
}
```

**Completed Transcription:**
```json
{
  "transcriptionId": "trx_4d5e6f7a8b9c0d1e",
  "status": "completed",
  "transcript": {
    "fullText": "Welcome everyone to our quarterly planning session...",
    "segments": [
      {
        "speaker": "Speaker 1",
        "start": 0.5,
        "end": 4.2,
        "text": "Welcome everyone to our quarterly planning session.",
        "confidence": 0.97
      },
      {
        "speaker": "Speaker 2",
        "start": 4.8,
        "end": 8.1,
        "text": "Thanks for having us. Excited to dive into the roadmap.",
        "confidence": 0.95
      }
    ],
    "speakers": [
      {"id": "Speaker 1", "speakingTime": 847.3},
      {"id": "Speaker 2", "speakingTime": 612.8},
      {"id": "Speaker 3", "speakingTime": 386.9}
    ]
  },
  "exports": {
    "srt": "https://cdn.adverant.ai/videoagent/trx_4d5e6f7a8b9c0d1e/transcript.srt",
    "vtt": "https://cdn.adverant.ai/videoagent/trx_4d5e6f7a8b9c0d1e/transcript.vtt",
    "json": "https://cdn.adverant.ai/videoagent/trx_4d5e6f7a8b9c0d1e/transcript.json"
  }
}
```

## Distribute to Platforms

Automatically distribute your video to multiple platforms:

```bash
curl -X POST "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/distribute" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c" \
  -H "Content-Type: application/json" \
  -d '{
    "videoId": "out_abc123def",
    "platforms": [
      {
        "platform": "youtube",
        "connectionId": "conn_yt_123",
        "metadata": {
          "title": "Introducing Our Revolutionary SaaS Platform",
          "description": "Discover how our new platform transforms business operations...",
          "tags": ["saas", "productivity", "enterprise", "ai"],
          "category": "Science & Technology",
          "visibility": "public",
          "scheduledPublish": "2025-01-20T14:00:00Z"
        }
      },
      {
        "platform": "linkedin",
        "connectionId": "conn_li_456",
        "metadata": {
          "title": "Big News: Product Launch Announcement",
          "description": "Were excited to share our latest innovation...",
          "visibility": "public"
        }
      },
      {
        "platform": "vimeo",
        "connectionId": "conn_vim_789",
        "metadata": {
          "title": "Q1 Product Launch - Full Presentation",
          "privacy": "password",
          "password": "launch2025"
        }
      }
    ]
  }'
```

**Response:**
```json
{
  "distributionId": "dist_8f7e6d5c4b3a2190",
  "status": "in_progress",
  "platforms": [
    {"platform": "youtube", "status": "uploading", "progress": 0},
    {"platform": "linkedin", "status": "queued"},
    {"platform": "vimeo", "status": "queued"}
  ]
}
```

## Using Webhooks

For production applications, configure webhooks to receive real-time notifications:

```bash
curl -X POST "https://api.adverant.ai/proxy/nexus-videoagent/api/v1/video/projects" \
  -H "Authorization: Bearer nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Automated Content Pipeline",
    "webhook": {
      "url": "https://your-app.com/webhooks/videoagent",
      "secret": "whsec_your_webhook_secret",
      "events": [
        "project.created",
        "generation.started",
        "generation.completed",
        "generation.failed",
        "transcription.completed",
        "distribution.completed"
      ]
    }
  }'
```

## SDK Usage Examples

### TypeScript/JavaScript

```typescript
import { NexusClient } from '@adverant/nexus-sdk';

const nexus = new NexusClient({
  apiKey: 'nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c'
});

const videoAgent = nexus.plugin('nexus-videoagent');

// Create a project and generate video
const project = await videoAgent.createProject({
  name: 'Weekly Content',
  sourceVideos: [
    { url: 'https://storage.example.com/raw.mp4', type: 'raw-footage' }
  ]
});

const generation = await project.generate({
  outputType: 'social-clip',
  duration: { target: 60 },
  style: { pacing: 'fast' }
});

// Wait for completion
const result = await generation.waitForCompletion();
console.log('Video ready:', result.outputs[0].url);
```

### Python

```python
from nexus_sdk import NexusClient

nexus = NexusClient(api_key="nxs_live_7f8a9b2c3d4e5f6a7b8c9d0e1f2a3b4c")
video_agent = nexus.plugin("nexus-videoagent")

# Transcribe a video
transcription = video_agent.transcribe(
    video_url="https://storage.example.com/recording.mp4",
    language="en",
    speaker_diarization=True
)

# Wait for completion
result = transcription.wait_for_completion()
print(f"Transcription complete: {len(result.segments)} segments")
print(f"Speakers identified: {len(result.speakers)}")
```

## Pricing Tiers Reference

| Tier | Monthly Price | Minutes/Month | Features |
|------|---------------|---------------|----------|
| **Starter** | $49 | 60 | Basic editing, transcription |
| **Creator** | $149 | 300 | AI editing, translation, multi-format |
| **Studio** | $499 | Unlimited | Priority processing, API access, white-label |

## Next Steps

Now that you have VideoAgent running, explore these resources:

- **[Use Cases Guide](USE-CASES.md)** - Detailed workflows for content creation, corporate video, and media production
- **[Architecture Overview](ARCHITECTURE.md)** - Understanding the technical architecture
- **[API Reference](docs/api-reference/endpoints.md)** - Complete endpoint documentation
- **[Platform Integrations](docs/guides/platform-setup.md)** - Connect YouTube, LinkedIn, Vimeo, and more

## Troubleshooting

### Common Issues

**Video processing taking too long?**
- Check video resolution and duration
- Creator and Studio tiers have priority processing
- Consider splitting very long videos into segments

**Transcription accuracy issues?**
- Specify the correct language code
- Ensure clear audio quality in source
- Use custom vocabulary for domain-specific terms

**Distribution failures?**
- Verify platform connection is still authorized
- Check platform-specific content policies
- Ensure video meets platform requirements (duration, format)

**Rate limited?**
- Creator and Studio tiers have higher rate limits
- Implement exponential backoff in your application
- Use webhooks instead of polling for status

### Support Channels

- **Documentation**: [docs.adverant.ai/plugins/videoagent](https://docs.adverant.ai/plugins/videoagent)
- **Discord Community**: [discord.gg/adverant](https://discord.gg/adverant)
- **Email Support**: support@adverant.ai
- **GitHub Issues**: [Report bugs](https://github.com/adverant/Adverant-Nexus-Plugin-VideoAgent/issues)

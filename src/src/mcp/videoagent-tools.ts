/**
 * VideoAgent MCP Tools Definition
 *
 * Defines 7 VideoAgent-specific MCP tools following the Nexus Gateway pattern:
 * 1. videoagent_process_video - Submit video processing job
 * 2. videoagent_get_job_status - Get job status and progress
 * 3. videoagent_cancel_job - Cancel running job
 * 4. videoagent_search_similar_frames - Find similar frames by embedding
 * 5. videoagent_search_scenes - Search scenes by description
 * 6. videoagent_get_queue_stats - Get job queue statistics
 * 7. videoagent_websocket_stats - Get WebSocket connection statistics
 *
 * Pattern: services/nexus-api-gateway/src/tools/nexus-tools.ts
 */

import { Tool } from '@modelcontextprotocol/sdk/types.js';

export const VIDEOAGENT_TOOLS: Tool[] = [
  // ========================================================================
  // Tool 1: Process Video
  // ========================================================================
  {
    name: 'videoagent_process_video',
    description: `Submit a video processing job to VideoAgent.

Features:
- Frame extraction at configurable FPS
- Scene detection with boundary analysis
- Audio extraction with transcript generation
- MageAgent vision analysis for each frame
- GraphRAG embedding generation (VoyageAI voyage-3, 1024-D)
- Qdrant vector storage for similarity search
- Real-time progress updates via WebSocket

Returns job ID for tracking progress.

Examples:
- Process video from URL: {"videoUrl": "https://example.com/video.mp4", "userId": "user_123"}
- Extract frames only: {"videoUrl": "...", "options": {"extractFrames": true, "extractAudio": false}}
- Custom FPS: {"videoUrl": "...", "options": {"fps": 2}}`,
    inputSchema: {
      type: 'object',
      properties: {
        videoUrl: {
          type: 'string',
          description: 'URL of the video to process (HTTP/HTTPS or Google Drive share link)',
        },
        userId: {
          type: 'string',
          description: 'User ID for tracking and ownership',
        },
        options: {
          type: 'object',
          description: 'Processing options',
          properties: {
            extractFrames: {
              type: 'boolean',
              description: 'Extract and analyze frames (default: true)',
              default: true,
            },
            extractAudio: {
              type: 'boolean',
              description: 'Extract audio and generate transcript (default: true)',
              default: true,
            },
            extractScenes: {
              type: 'boolean',
              description: 'Detect scene boundaries (default: true)',
              default: true,
            },
            generateEmbeddings: {
              type: 'boolean',
              description: 'Generate embeddings for similarity search (default: true)',
              default: true,
            },
            fps: {
              type: 'number',
              description: 'Frames per second to extract (default: 1)',
              default: 1,
            },
            maxFrames: {
              type: 'number',
              description: 'Maximum frames to process (default: unlimited)',
            },
          },
        },
      },
      required: ['videoUrl', 'userId'],
    },
  },

  // ========================================================================
  // Tool 2: Get Job Status
  // ========================================================================
  {
    name: 'videoagent_get_job_status',
    description: `Get detailed status and progress of a video processing job.

Returns:
- Job status (pending, processing, completed, failed)
- Progress percentage (0-100)
- Current processing step
- Frames processed / total frames
- Scenes detected
- Error information (if failed)
- Processing timestamps

Examples:
- Get status: {"jobId": "job_abc123"}
- Check completion: Returns status "completed" when done`,
    inputSchema: {
      type: 'object',
      properties: {
        jobId: {
          type: 'string',
          description: 'Job ID returned from videoagent_process_video',
        },
      },
      required: ['jobId'],
    },
  },

  // ========================================================================
  // Tool 3: Cancel Job
  // ========================================================================
  {
    name: 'videoagent_cancel_job',
    description: `Cancel a running video processing job.

Stops processing and releases resources. Cannot be undone.

Returns:
- Success confirmation
- Job final status

Examples:
- Cancel job: {"jobId": "job_abc123"}`,
    inputSchema: {
      type: 'object',
      properties: {
        jobId: {
          type: 'string',
          description: 'Job ID to cancel',
        },
      },
      required: ['jobId'],
    },
  },

  // ========================================================================
  // Tool 4: Search Similar Frames
  // ========================================================================
  {
    name: 'videoagent_search_similar_frames',
    description: `Search for similar video frames using vector similarity.

Search modes:
1. By embedding - Provide 1024-D embedding vector
2. By text query - Natural language description (generates embedding via GraphRAG)
3. By frame ID - Find frames similar to a specific frame

Uses Qdrant vector database with 1024-D VoyageAI voyage-3 embeddings.

Returns:
- Matching frames with similarity scores
- Frame metadata (timestamp, features, scene)
- Thumbnail URLs

Examples:
- Text search: {"query": "person waving at camera", "limit": 10}
- Similar to frame: {"frameId": "frame_xyz", "limit": 5}
- With filters: {"query": "sunset", "filters": {"sceneId": "scene_abc"}}`,
    inputSchema: {
      type: 'object',
      properties: {
        query: {
          type: 'string',
          description: 'Natural language search query (generates embedding)',
        },
        embedding: {
          type: 'array',
          description: '1024-D embedding vector for direct similarity search',
          items: {
            type: 'number',
          },
        },
        frameId: {
          type: 'string',
          description: 'Frame ID to find similar frames to',
        },
        limit: {
          type: 'number',
          description: 'Maximum results to return (default: 10)',
          default: 10,
        },
        threshold: {
          type: 'number',
          description: 'Minimum similarity score (0-1, default: 0.7)',
          default: 0.7,
        },
        filters: {
          type: 'object',
          description: 'Additional filters (jobId, sceneId, userId)',
          properties: {
            jobId: {
              type: 'string',
            },
            sceneId: {
              type: 'string',
            },
            userId: {
              type: 'string',
            },
          },
        },
      },
      oneOf: [
        { required: ['query'] },
        { required: ['embedding'] },
        { required: ['frameId'] },
      ],
    },
  },

  // ========================================================================
  // Tool 5: Search Scenes
  // ========================================================================
  {
    name: 'videoagent_search_scenes',
    description: `Search for video scenes by natural language description.

Scene detection uses:
- Visual change detection
- Audio segmentation
- Semantic boundary detection

Returns:
- Matching scenes with metadata
- Scene duration and frame count
- Key frames
- Description/summary

Examples:
- Search scenes: {"query": "outdoor conversation", "limit": 5}
- Filter by job: {"query": "action sequence", "jobId": "job_abc123"}
- Duration filter: {"query": "short clips", "minDuration": 0, "maxDuration": 10}`,
    inputSchema: {
      type: 'object',
      properties: {
        query: {
          type: 'string',
          description: 'Natural language scene description',
        },
        jobId: {
          type: 'string',
          description: 'Filter by specific job ID',
        },
        limit: {
          type: 'number',
          description: 'Maximum results to return (default: 10)',
          default: 10,
        },
        minDuration: {
          type: 'number',
          description: 'Minimum scene duration in seconds',
        },
        maxDuration: {
          type: 'number',
          description: 'Maximum scene duration in seconds',
        },
      },
      required: ['query'],
    },
  },

  // ========================================================================
  // Tool 6: Get Queue Statistics
  // ========================================================================
  {
    name: 'videoagent_get_queue_stats',
    description: `Get job queue statistics and system performance metrics.

Returns:
- Pending jobs count
- Active/processing jobs
- Completed jobs count
- Failed jobs count
- Average processing time
- Queue health status

Useful for:
- Monitoring system load
- Estimating wait times
- Capacity planning

Examples:
- Get stats: {}
- No parameters required`,
    inputSchema: {
      type: 'object',
      properties: {},
    },
  },

  // ========================================================================
  // Tool 7: WebSocket Statistics
  // ========================================================================
  {
    name: 'videoagent_websocket_stats',
    description: `Get WebSocket connection statistics and real-time monitoring info.

Returns:
- Total connections
- Active connections per namespace
- Event counts by type
- Server uptime
- Room subscriptions

Namespaces:
- /videoagent - General events
- /videoagent/jobs - Job lifecycle
- /videoagent/progress - Progress updates
- /videoagent/frames - Frame events
- /videoagent/scenes - Scene events

Examples:
- Get stats: {}
- No parameters required`,
    inputSchema: {
      type: 'object',
      properties: {},
    },
  },
];

/**
 * Tool name to description mapping for quick lookup
 */
export const TOOL_DESCRIPTIONS: Record<string, string> = {
  videoagent_process_video: 'Submit video processing job with frame/scene/audio extraction',
  videoagent_get_job_status: 'Get job status, progress, and processing details',
  videoagent_cancel_job: 'Cancel running video processing job',
  videoagent_search_similar_frames: 'Search frames by embedding/text similarity (1024-D)',
  videoagent_search_scenes: 'Search scenes by natural language description',
  videoagent_get_queue_stats: 'Get job queue statistics and system metrics',
  videoagent_websocket_stats: 'Get WebSocket connection and event statistics',
};

/**
 * Validate tool arguments against schema
 */
export function validateToolArguments(toolName: string, args: any): { valid: boolean; error?: string } {
  const tool = VIDEOAGENT_TOOLS.find(t => t.name === toolName);

  if (!tool) {
    return {
      valid: false,
      error: `Unknown tool: ${toolName}`,
    };
  }

  const schema = tool.inputSchema;
  const required = (schema as any).required || [];

  // Check required fields
  for (const field of required) {
    if (!(field in args)) {
      return {
        valid: false,
        error: `Missing required field: ${field}`,
      };
    }
  }

  // Check oneOf constraints for search_similar_frames
  if (toolName === 'videoagent_search_similar_frames') {
    const hasQuery = 'query' in args;
    const hasEmbedding = 'embedding' in args;
    const hasFrameId = 'frameId' in args;

    if (!hasQuery && !hasEmbedding && !hasFrameId) {
      return {
        valid: false,
        error: 'Must provide one of: query, embedding, or frameId',
      };
    }
  }

  return { valid: true };
}

/**
 * Get tool by name
 */
export function getTool(toolName: string): Tool | undefined {
  return VIDEOAGENT_TOOLS.find(t => t.name === toolName);
}

/**
 * Get all tool names
 */
export function getToolNames(): string[] {
  return VIDEOAGENT_TOOLS.map(t => t.name);
}

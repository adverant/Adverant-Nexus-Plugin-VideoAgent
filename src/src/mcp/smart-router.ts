/**
 * VideoAgent Smart Router
 *
 * Unified routing logic for all client types (MCP stdio, HTTP REST, WebSocket).
 * Ensures consistent behavior by using ONE implementation for tool execution.
 *
 * Pattern: services/nexus-api-gateway/src/routing/smart-router.ts
 *
 * Critical: Prevents version skew by centralizing ALL tool logic here.
 * MCP, HTTP, and WebSocket all route through this same code path.
 */

import { RedisProducer } from '../queue/RedisProducer';
import { MageAgentClient } from '../clients/MageAgentClient';
import { VideoAgentWebSocketServer } from '../websocket/VideoAgentWebSocketServer';
import { validateToolArguments } from './videoagent-tools';
import { createLogger, format, transports } from 'winston';
import axios from 'axios';

// ============================================================================
// Types
// ============================================================================

interface RouteDecision {
  service: string;
  endpoint: string;
  method: string;
  description: string;
  handler: (args: any) => Promise<any>;
}

interface SmartRouterConfig {
  redisProducer: RedisProducer;
  mageAgentClient: MageAgentClient;
  websocketServer: VideoAgentWebSocketServer | null; // Optional: null for MCP stdio mode
  postgresUrl: string;
  qdrantUrl: string;
  graphragUrl: string;
}

// ============================================================================
// Smart Router Class
// ============================================================================

export class VideoAgentSmartRouter {
  private logger: ReturnType<typeof createLogger>;
  private config: SmartRouterConfig;

  constructor(config: SmartRouterConfig) {
    this.config = config;

    // Initialize logger
    this.logger = createLogger({
      level: 'info',
      format: format.combine(
        format.timestamp(),
        format.errors({ stack: true }),
        format.json()
      ),
      transports: [
        new transports.Console({
          format: format.combine(
            format.colorize(),
            format.printf(({ timestamp, level, message, ...meta }) => {
              return `${timestamp} [${level}] [Smart Router]: ${message} ${
                Object.keys(meta).length ? JSON.stringify(meta) : ''
              }`;
            })
          ),
        }),
      ],
    });

    this.logger.info('VideoAgent Smart Router initialized');
  }

  // ==========================================================================
  // Main Routing Entry Point
  // ==========================================================================

  /**
   * Route a tool call to the appropriate handler
   * This is called by MCP adapter, HTTP routes, and WebSocket handlers
   */
  async route(toolName: string, args: any): Promise<RouteDecision> {
    this.logger.info(`Routing tool: ${toolName}`, {
      argsPreview: JSON.stringify(args).substring(0, 200),
    });

    // Validate arguments
    const validation = validateToolArguments(toolName, args);
    if (!validation.valid) {
      throw new Error(`Invalid arguments for ${toolName}: ${validation.error}`);
    }

    // Route to appropriate handler
    switch (toolName) {
      case 'videoagent_process_video':
        return this.routeProcessVideo(args);
      case 'videoagent_get_job_status':
        return this.routeGetJobStatus(args);
      case 'videoagent_cancel_job':
        return this.routeCancelJob(args);
      case 'videoagent_search_similar_frames':
        return this.routeSearchSimilarFrames(args);
      case 'videoagent_search_scenes':
        return this.routeSearchScenes(args);
      case 'videoagent_get_queue_stats':
        return this.routeGetQueueStats(args);
      case 'videoagent_websocket_stats':
        return this.routeWebSocketStats(args);
      default:
        throw new Error(`Unknown tool: ${toolName}`);
    }
  }

  // ==========================================================================
  // Tool Handlers
  // ==========================================================================

  private routeProcessVideo(_args: any): RouteDecision {
    return {
      service: 'VideoAgent',
      endpoint: '/videoagent/api/video/process',
      method: 'POST',
      description: 'Submit video processing job',
      handler: async (args) => {
        const { videoUrl, userId, options = {} } = args;

        // Generate job ID
        const jobId = `job_${Date.now()}_${Math.random().toString(36).substring(7)}`;

        // Default options matching ProcessingOptions interface
        const processingOptions = {
          extractFrames: options.extractFrames !== false,
          frameSamplingMode: options.frameSamplingMode || 'uniform' as const,
          frameSampleRate: options.frameSampleRate || options.fps || 1,
          maxFrames: options.maxFrames || 0,
          extractAudio: options.extractAudio !== false,
          transcribeAudio: options.transcribeAudio !== false,
          detectScenes: options.detectScenes !== false,
          detectObjects: options.detectObjects || false,
          extractText: options.extractText || false,
          classifyContent: options.classifyContent || false,
          generateSummary: options.generateSummary !== false,
          customAnalysis: options.customAnalysis,
          targetLanguages: options.targetLanguages || ['en'],
          qualityPreference: options.qualityPreference || 'balanced' as const,
          additionalMetadata: options.additionalMetadata,
        };

        // Enqueue job via Redis producer
        await this.config.redisProducer.enqueueJob({
          jobId,
          filename: `video_${jobId}.mp4`,
          userId,
          videoUrl,
          sourceType: 'url',
          options: processingOptions,
          enqueuedAt: new Date(),
        });

        this.logger.info('Video processing job enqueued', {
          jobId,
          userId,
          videoUrl: videoUrl.substring(0, 100),
        });

        // Broadcast job created event (if WebSocket server available)
        if (this.config.websocketServer) {
          this.config.websocketServer.broadcastJobEvent({
            jobId,
            userId,
            videoUrl,
            status: 'created',
            timestamp: new Date(),
          });
        }

        return {
          success: true,
          jobId,
          status: 'queued',
          message: 'Video processing job submitted successfully',
          options: processingOptions,
        };
      },
    };
  }

  private routeGetJobStatus(_args: any): RouteDecision {
    return {
      service: 'VideoAgent',
      endpoint: `/videoagent/api/video/job/${_args.jobId}`,
      method: 'GET',
      description: 'Get job status and progress',
      handler: async (args) => {
        const { jobId } = args;

        // Query job status from Redis or PostgreSQL
        const status = await this.getJobStatusFromStorage(jobId);

        if (!status) {
          throw new Error(`Job not found: ${jobId}`);
        }

        this.logger.debug('Job status retrieved', { jobId, status: status.status });

        return {
          success: true,
          job: status,
        };
      },
    };
  }

  private routeCancelJob(_args: any): RouteDecision {
    return {
      service: 'VideoAgent',
      endpoint: `/videoagent/api/video/job/${_args.jobId}`,
      method: 'DELETE',
      description: 'Cancel video processing job',
      handler: async (args) => {
        const { jobId } = args;

        // Remove job from queue
        const removed = await this.config.redisProducer.removeJob(jobId);

        if (!removed) {
          throw new Error(`Failed to cancel job: ${jobId} not found or already completed`);
        }

        this.logger.info('Job cancelled', { jobId });

        // Broadcast job cancelled event (if WebSocket server available)
        if (this.config.websocketServer) {
          this.config.websocketServer.broadcastJobEvent({
            jobId,
            userId: 'system',
            videoUrl: '',
            status: 'failed',
            error: 'Job cancelled by user',
            timestamp: new Date(),
          });
        }

        return {
          success: true,
          jobId,
          status: 'cancelled',
          message: 'Job cancelled successfully',
        };
      },
    };
  }

  private routeSearchSimilarFrames(_args: any): RouteDecision {
    return {
      service: 'Qdrant',
      endpoint: `${this.config.qdrantUrl}/collections/videoagent_frames/points/search`,
      method: 'POST',
      description: 'Search similar frames by embedding/text',
      handler: async (args) => {
        const { query, embedding, frameId, limit = 10, threshold = 0.7, filters } = args;

        let searchEmbedding: number[];

        if (embedding) {
          // Direct embedding search
          searchEmbedding = embedding;
        } else if (query) {
          // Generate embedding from text query via GraphRAG
          const embeddingResponse = await axios.post(
            `${this.config.graphragUrl}/api/embeddings/generate`,
            { content: query },
            { timeout: 10000 }
          );

          if (!embeddingResponse.data.success) {
            throw new Error('Failed to generate embedding for query');
          }

          searchEmbedding = embeddingResponse.data.embedding;
        } else if (frameId) {
          // Get embedding for specified frame
          const frameEmbedding = await this.getFrameEmbedding(frameId);
          if (!frameEmbedding) {
            throw new Error(`Frame not found: ${frameId}`);
          }
          searchEmbedding = frameEmbedding;
        } else {
          throw new Error('Must provide query, embedding, or frameId');
        }

        // Build Qdrant filter
        const qdrantFilter: any = {};
        if (filters) {
          if (filters.jobId) {
            qdrantFilter.must = qdrantFilter.must || [];
            qdrantFilter.must.push({
              key: 'jobId',
              match: { value: filters.jobId },
            });
          }
          if (filters.sceneId) {
            qdrantFilter.must = qdrantFilter.must || [];
            qdrantFilter.must.push({
              key: 'sceneId',
              match: { value: filters.sceneId },
            });
          }
          if (filters.userId) {
            qdrantFilter.must = qdrantFilter.must || [];
            qdrantFilter.must.push({
              key: 'userId',
              match: { value: filters.userId },
            });
          }
        }

        // Search Qdrant
        const searchResponse = await axios.post(
          `${this.config.qdrantUrl}/collections/videoagent_frames/points/search`,
          {
            vector: searchEmbedding,
            limit,
            score_threshold: threshold,
            with_payload: true,
            filter: Object.keys(qdrantFilter).length > 0 ? qdrantFilter : undefined,
          },
          { timeout: 10000 }
        );

        const results = searchResponse.data.result || [];

        this.logger.info('Frame similarity search completed', {
          resultsCount: results.length,
          query: query?.substring(0, 50),
        });

        return {
          success: true,
          results: results.map((r: any) => ({
            frameId: r.id,
            score: r.score,
            metadata: r.payload,
          })),
          count: results.length,
        };
      },
    };
  }

  private routeSearchScenes(_args: any): RouteDecision {
    return {
      service: 'PostgreSQL',
      endpoint: 'SELECT * FROM scenes WHERE description ILIKE $1',
      method: 'QUERY',
      description: 'Search scenes by description',
      handler: async (args) => {
        const { query, jobId, limit = 10, minDuration, maxDuration } = args;

        // Build SQL query
        let sql = 'SELECT * FROM scenes WHERE description ILIKE $1';
        const params: any[] = [`%${query}%`];
        let paramIndex = 2;

        if (jobId) {
          sql += ` AND job_id = $${paramIndex}`;
          params.push(jobId);
          paramIndex++;
        }

        if (minDuration !== undefined) {
          sql += ` AND (end_time - start_time) >= $${paramIndex}`;
          params.push(minDuration);
          paramIndex++;
        }

        if (maxDuration !== undefined) {
          sql += ` AND (end_time - start_time) <= $${paramIndex}`;
          params.push(maxDuration);
          paramIndex++;
        }

        sql += ` ORDER BY created_at DESC LIMIT $${paramIndex}`;
        params.push(limit);

        // Execute query (placeholder - would use actual PostgreSQL client)
        const results = await this.queryPostgreSQL(sql, params);

        this.logger.info('Scene search completed', {
          resultsCount: results.length,
          query: query.substring(0, 50),
        });

        return {
          success: true,
          scenes: results,
          count: results.length,
        };
      },
    };
  }

  private routeGetQueueStats(_args: any): RouteDecision {
    return {
      service: 'Redis',
      endpoint: '/videoagent/api/video/queue/stats',
      method: 'GET',
      description: 'Get job queue statistics',
      handler: async (_args) => {
        const stats = await this.config.redisProducer.getQueueStats();

        this.logger.debug('Queue stats retrieved', stats);

        return {
          success: true,
          stats,
        };
      },
    };
  }

  private routeWebSocketStats(_args: any): RouteDecision {
    return {
      service: 'WebSocket',
      endpoint: '/videoagent/api/websocket/stats',
      method: 'GET',
      description: 'Get WebSocket connection statistics',
      handler: async (_args) => {
        // WebSocket stats only available when WebSocket server is running
        if (!this.config.websocketServer) {
          return {
            success: false,
            error: 'WebSocket server not available (MCP stdio mode)',
            stats: {
              totalConnections: 0,
              activeConnections: 0,
              mode: 'stdio',
            },
          };
        }

        const stats = this.config.websocketServer.getStatistics();

        this.logger.debug('WebSocket stats retrieved', {
          totalConnections: stats.totalConnections,
          activeConnections: stats.activeConnections,
        });

        return {
          success: true,
          stats,
        };
      },
    };
  }

  // ==========================================================================
  // Helper Methods
  // ==========================================================================

  /**
   * Get job status from storage (Redis or PostgreSQL)
   */
  private async getJobStatusFromStorage(jobId: string): Promise<any | null> {
    // This is a placeholder - implement based on your storage solution
    // Could query Redis first, then PostgreSQL as fallback
    try {
      // Example: Query PostgreSQL
      const result = await this.queryPostgreSQL(
        'SELECT * FROM jobs WHERE job_id = $1',
        [jobId]
      );

      if (result.length > 0) {
        return result[0];
      }

      return null;
    } catch (error) {
      this.logger.error('Failed to get job status', {
        jobId,
        error: error instanceof Error ? error.message : 'Unknown error',
      });
      return null;
    }
  }

  /**
   * Get frame embedding from Qdrant
   */
  private async getFrameEmbedding(frameId: string): Promise<number[] | null> {
    try {
      const response = await axios.post(
        `${this.config.qdrantUrl}/collections/videoagent_frames/points/retrieve`,
        {
          ids: [frameId],
          with_vector: true,
        },
        { timeout: 5000 }
      );

      const points = response.data.result || [];
      if (points.length > 0 && points[0].vector) {
        return points[0].vector as number[];
      }

      return null;
    } catch (error) {
      this.logger.error('Failed to get frame embedding', {
        frameId,
        error: error instanceof Error ? error.message : 'Unknown error',
      });
      return null;
    }
  }

  /**
   * Query PostgreSQL database
   * Placeholder - implement with actual PostgreSQL client
   */
  private async queryPostgreSQL(sql: string, _params: any[]): Promise<any[]> {
    // This would use pg.Pool or similar
    // For now, return empty array as placeholder
    this.logger.warn('PostgreSQL query not implemented', { sql });
    return [];
  }
}

/**
 * Export singleton instance
 * Will be initialized by server.ts
 */
export let smartRouter: VideoAgentSmartRouter;

/**
 * Initialize smart router with configuration
 * Called once during server startup
 */
export function initializeSmartRouter(config: SmartRouterConfig): void {
  smartRouter = new VideoAgentSmartRouter(config);
}

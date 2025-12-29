/**
 * VideoAgent WebSocket Server - Socket.IO Implementation
 *
 * Enterprise-grade WebSocket server following GraphRAG pattern with:
 * - 5 specialized namespaces for different event types
 * - Room-based subscriptions for job-specific updates
 * - Redis pub/sub integration for worker communication
 * - Session management and statistics
 * - Automatic reconnection and fallback transports
 *
 * Namespaces:
 * - /videoagent: General video processing events
 * - /videoagent/jobs: Job lifecycle events (created, started, completed, failed)
 * - /videoagent/progress: Real-time progress updates (percentage, frames processed)
 * - /videoagent/frames: Frame extraction and analysis events
 * - /videoagent/scenes: Scene detection events
 */

import { Server as SocketIOServer, Socket, Namespace } from 'socket.io';
import { Server as HTTPServer } from 'http';
import Redis from 'ioredis';
import { createLogger, format, transports } from 'winston';

// ============================================================================
// Types and Interfaces
// ============================================================================

interface SessionMetadata {
  sessionId: string;
  userId?: string;
  connectedAt: Date;
  lastActivity: Date;
  subscribedJobs: Set<string>;
}

interface ProgressUpdate {
  jobId: string;
  status: 'pending' | 'processing' | 'completed' | 'failed';
  progress: number; // 0-100
  currentStep?: string;
  framesProcessed?: number;
  totalFrames?: number;
  scenesDetected?: number;
  message?: string;
  timestamp: Date;
}

interface FrameEvent {
  jobId: string;
  frameId: string;
  frameNumber: number;
  timestamp: number;
  features?: {
    objects: string[];
    scene: string;
    confidence: number;
  };
  embedding?: number[];
  thumbnailUrl?: string;
}

interface SceneEvent {
  jobId: string;
  sceneId: string;
  startTime: number;
  endTime: number;
  frameCount: number;
  description?: string;
  keyFrames?: string[];
}

interface JobEvent {
  jobId: string;
  userId: string;
  videoUrl: string;
  status: 'created' | 'queued' | 'processing' | 'completed' | 'failed';
  error?: string;
  metadata?: Record<string, any>;
  timestamp: Date;
}

interface WebSocketStatistics {
  totalConnections: number;
  activeConnections: number;
  namespaceStats: {
    [namespace: string]: {
      connections: number;
      rooms: number;
    };
  };
  eventCounts: {
    [eventType: string]: number;
  };
  uptime: number;
}

// ============================================================================
// VideoAgent WebSocket Server Class
// ============================================================================

export class VideoAgentWebSocketServer {
  private io: SocketIOServer;
  private redisClient: Redis;
  private redisSub: Redis;
  private logger: ReturnType<typeof createLogger>;

  // Namespaces
  private videoagentNamespace: Namespace;
  private jobsNamespace: Namespace;
  private progressNamespace: Namespace;
  private framesNamespace: Namespace;
  private scenesNamespace: Namespace;

  // Session tracking
  private sessions: Map<string, SessionMetadata> = new Map();
  private eventCounts: Map<string, number> = new Map();
  private startTime: Date;

  constructor(
    httpServer: HTTPServer,
    redisUrl: string
  ) {
    this.startTime = new Date();

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
              return `${timestamp} [${level}] [VideoAgent WS]: ${message} ${
                Object.keys(meta).length ? JSON.stringify(meta) : ''
              }`;
            })
          ),
        }),
      ],
    });

    // Initialize Socket.IO server with production-ready configuration
    this.io = new SocketIOServer(httpServer, {
      path: '/videoagent/socket.io',
      cors: {
        origin: '*', // Configure based on environment in production
        methods: ['GET', 'POST'],
        credentials: true,
      },
      transports: ['websocket', 'polling'],
      pingTimeout: 60000, // 60 seconds
      pingInterval: 25000, // 25 seconds
      maxHttpBufferSize: 1e6, // 1MB
      allowEIO3: true, // Socket.IO v3 compatibility
    });

    // Initialize Redis clients
    this.redisClient = new Redis(redisUrl);
    this.redisSub = new Redis(redisUrl); // Separate connection for pub/sub

    // Initialize namespaces
    this.videoagentNamespace = this.io.of('/videoagent');
    this.jobsNamespace = this.io.of('/videoagent/jobs');
    this.progressNamespace = this.io.of('/videoagent/progress');
    this.framesNamespace = this.io.of('/videoagent/frames');
    this.scenesNamespace = this.io.of('/videoagent/scenes');

    // Setup namespace handlers
    this.setupVideoAgentNamespace();
    this.setupJobsNamespace();
    this.setupProgressNamespace();
    this.setupFramesNamespace();
    this.setupScenesNamespace();

    // Setup Redis pub/sub subscriptions
    this.setupRedisSubscriptions();

    this.logger.info('VideoAgent WebSocket Server initialized', {
      path: '/videoagent/socket.io',
      namespaces: [
        '/videoagent',
        '/videoagent/jobs',
        '/videoagent/progress',
        '/videoagent/frames',
        '/videoagent/scenes',
      ],
    });
  }

  // ==========================================================================
  // Namespace Setup Methods
  // ==========================================================================

  private setupVideoAgentNamespace(): void {
    this.videoagentNamespace.on('connection', (socket: Socket) => {
      const sessionId = socket.id;
      const userId = socket.handshake.query.userId as string | undefined;

      this.logger.info('Client connected to /videoagent namespace', {
        sessionId,
        userId,
      });

      // Initialize session metadata
      this.sessions.set(sessionId, {
        sessionId,
        userId,
        connectedAt: new Date(),
        lastActivity: new Date(),
        subscribedJobs: new Set(),
      });

      // Send welcome message with server capabilities
      socket.emit('connected', {
        message: 'Connected to VideoAgent WebSocket Server',
        namespaces: [
          '/videoagent',
          '/videoagent/jobs',
          '/videoagent/progress',
          '/videoagent/frames',
          '/videoagent/scenes',
        ],
        features: [
          'Real-time job updates',
          'Frame processing events',
          'Scene detection events',
          'Progress tracking',
        ],
        timestamp: new Date(),
      });

      // Handle job subscription
      socket.on('subscribe:job', (jobId: string) => {
        this.handleJobSubscription(socket, sessionId, jobId);
      });

      // Handle job unsubscription
      socket.on('unsubscribe:job', (jobId: string) => {
        this.handleJobUnsubscription(socket, sessionId, jobId);
      });

      // Handle disconnection
      socket.on('disconnect', () => {
        this.handleDisconnection(sessionId);
      });

      // Update activity timestamp on any message
      socket.onAny(() => {
        const session = this.sessions.get(sessionId);
        if (session) {
          session.lastActivity = new Date();
        }
      });
    });
  }

  private setupJobsNamespace(): void {
    this.jobsNamespace.on('connection', (socket: Socket) => {
      this.logger.info('Client connected to /videoagent/jobs namespace', {
        sessionId: socket.id,
      });

      socket.emit('connected', {
        message: 'Connected to VideoAgent Jobs namespace',
        eventTypes: ['job:created', 'job:queued', 'job:started', 'job:completed', 'job:failed'],
      });

      // Allow clients to query job status
      socket.on('job:status', async (jobId: string, callback) => {
        try {
          const status = await this.getJobStatus(jobId);
          callback({ success: true, data: status });
        } catch (error) {
          callback({ success: false, error: (error as Error).message });
        }
      });
    });
  }

  private setupProgressNamespace(): void {
    this.progressNamespace.on('connection', (socket: Socket) => {
      this.logger.info('Client connected to /videoagent/progress namespace', {
        sessionId: socket.id,
      });

      socket.emit('connected', {
        message: 'Connected to VideoAgent Progress namespace',
        eventTypes: ['progress:update', 'progress:frame', 'progress:scene'],
      });
    });
  }

  private setupFramesNamespace(): void {
    this.framesNamespace.on('connection', (socket: Socket) => {
      this.logger.info('Client connected to /videoagent/frames namespace', {
        sessionId: socket.id,
      });

      socket.emit('connected', {
        message: 'Connected to VideoAgent Frames namespace',
        eventTypes: ['frame:extracted', 'frame:analyzed', 'frame:embedded'],
      });
    });
  }

  private setupScenesNamespace(): void {
    this.scenesNamespace.on('connection', (socket: Socket) => {
      this.logger.info('Client connected to /videoagent/scenes namespace', {
        sessionId: socket.id,
      });

      socket.emit('connected', {
        message: 'Connected to VideoAgent Scenes namespace',
        eventTypes: ['scene:detected', 'scene:analyzed'],
      });
    });
  }

  // ==========================================================================
  // Redis Pub/Sub Integration
  // ==========================================================================

  private setupRedisSubscriptions(): void {
    // Subscribe to all VideoAgent channel patterns (job-specific and global)
    // Using psubscribe to match patterns like "videoagent:progress:*"
    const patterns = [
      'videoagent:jobs',           // Global job events
      'videoagent:jobs:*',         // Job-specific events
      'videoagent:progress:*',     // Job-specific progress updates
      'videoagent:frames:*',       // Job-specific frame events
      'videoagent:scenes:*',       // Job-specific scene events
    ];

    patterns.forEach(pattern => {
      this.redisSub.psubscribe(pattern, (err, count) => {
        if (err) {
          this.logger.error(`Failed to subscribe to pattern ${pattern}`, { error: err.message });
        } else {
          this.logger.info(`Subscribed to Redis pattern: ${pattern}`, { patternCount: count });
        }
      });
    });

    // Handle incoming Redis messages from pattern subscriptions
    this.redisSub.on('pmessage', (pattern, channel, message) => {
      try {
        const data = JSON.parse(message);
        this.handleRedisMessage(channel, data);
      } catch (error) {
        this.logger.error('Failed to parse Redis message', {
          pattern,
          channel,
          error: (error as Error).message,
        });
      }
    });
  }

  private handleRedisMessage(channel: string, data: any): void {
    // Extract channel type from pattern (e.g., "videoagent:progress:job123" -> "progress")
    const parts = channel.split(':');
    const channelType = parts[1]; // "jobs", "progress", "frames", "scenes"

    switch (channelType) {
      case 'jobs':
        this.broadcastJobEvent(data as JobEvent);
        break;
      case 'progress':
        this.broadcastProgressUpdate(data as ProgressUpdate);
        break;
      case 'frames':
        this.broadcastFrameEvent(data as FrameEvent);
        break;
      case 'scenes':
        this.broadcastSceneEvent(data as SceneEvent);
        break;
      default:
        this.logger.warn('Unknown Redis channel type', { channel, channelType });
    }
  }

  // ==========================================================================
  // Event Broadcasting Methods
  // ==========================================================================

  public broadcastJobEvent(event: JobEvent): void {
    const { jobId, status } = event;

    // Broadcast to jobs namespace
    this.jobsNamespace.to(`job:${jobId}`).emit(`job:${status}`, event);

    // Also broadcast to main namespace for subscribers
    this.videoagentNamespace.to(`job:${jobId}`).emit('job:event', event);

    this.incrementEventCount(`job:${status}`);

    this.logger.debug('Broadcasted job event', { jobId, status });
  }

  public broadcastProgressUpdate(update: ProgressUpdate): void {
    const { jobId } = update;

    // Broadcast to progress namespace
    this.progressNamespace.to(`job:${jobId}`).emit('progress:update', update);

    // Also broadcast to main namespace
    this.videoagentNamespace.to(`job:${jobId}`).emit('progress', update);

    this.incrementEventCount('progress:update');
  }

  public broadcastFrameEvent(event: FrameEvent): void {
    const { jobId } = event;

    // Broadcast to frames namespace
    this.framesNamespace.to(`job:${jobId}`).emit('frame:extracted', event);

    // Also broadcast to main namespace
    this.videoagentNamespace.to(`job:${jobId}`).emit('frame', event);

    this.incrementEventCount('frame:extracted');
  }

  public broadcastSceneEvent(event: SceneEvent): void {
    const { jobId } = event;

    // Broadcast to scenes namespace
    this.scenesNamespace.to(`job:${jobId}`).emit('scene:detected', event);

    // Also broadcast to main namespace
    this.videoagentNamespace.to(`job:${jobId}`).emit('scene', event);

    this.incrementEventCount('scene:detected');
  }

  // ==========================================================================
  // Session Management
  // ==========================================================================

  private handleJobSubscription(socket: Socket, sessionId: string, jobId: string): void {
    const session = this.sessions.get(sessionId);
    if (!session) return;

    // Join room for this job across all namespaces
    const roomName = `job:${jobId}`;
    socket.join(roomName);

    // Track subscription
    session.subscribedJobs.add(jobId);

    this.logger.info('Client subscribed to job', { sessionId, jobId });

    socket.emit('subscribed', {
      jobId,
      message: `Subscribed to updates for job ${jobId}`,
    });
  }

  private handleJobUnsubscription(socket: Socket, sessionId: string, jobId: string): void {
    const session = this.sessions.get(sessionId);
    if (!session) return;

    // Leave room
    const roomName = `job:${jobId}`;
    socket.leave(roomName);

    // Remove from tracking
    session.subscribedJobs.delete(jobId);

    this.logger.info('Client unsubscribed from job', { sessionId, jobId });

    socket.emit('unsubscribed', {
      jobId,
      message: `Unsubscribed from updates for job ${jobId}`,
    });
  }

  private handleDisconnection(sessionId: string): void {
    const session = this.sessions.get(sessionId);
    if (session) {
      this.logger.info('Client disconnected', {
        sessionId,
        userId: session.userId,
        subscribedJobs: Array.from(session.subscribedJobs),
        duration: Date.now() - session.connectedAt.getTime(),
      });

      this.sessions.delete(sessionId);
    }
  }

  // ==========================================================================
  // Helper Methods
  // ==========================================================================

  private async getJobStatus(jobId: string): Promise<any> {
    // Query job status from Redis or database
    // This is a placeholder - implement based on your storage solution
    const status = await this.redisClient.get(`videoagent:job:${jobId}:status`);
    return status ? JSON.parse(status) : null;
  }

  private incrementEventCount(eventType: string): void {
    const current = this.eventCounts.get(eventType) || 0;
    this.eventCounts.set(eventType, current + 1);
  }

  // ==========================================================================
  // Statistics and Monitoring
  // ==========================================================================

  public getStatistics(): WebSocketStatistics {
    const namespaces = [
      this.videoagentNamespace,
      this.jobsNamespace,
      this.progressNamespace,
      this.framesNamespace,
      this.scenesNamespace,
    ];

    const namespaceStats: { [key: string]: { connections: number; rooms: number } } = {};
    let totalConnections = 0;

    namespaces.forEach(namespace => {
      const sockets = Array.from(namespace.sockets.values());
      const connections = sockets.length;
      totalConnections += connections;

      const rooms = namespace.adapter.rooms.size;

      namespaceStats[namespace.name] = {
        connections,
        rooms,
      };
    });

    return {
      totalConnections: this.sessions.size,
      activeConnections: totalConnections,
      namespaceStats,
      eventCounts: Object.fromEntries(this.eventCounts),
      uptime: Date.now() - this.startTime.getTime(),
    };
  }

  // ==========================================================================
  // Cleanup
  // ==========================================================================

  public async close(): Promise<void> {
    this.logger.info('Closing VideoAgent WebSocket Server');

    // Disconnect all clients
    this.io.disconnectSockets();

    // Close Socket.IO server
    this.io.close();

    // Close Redis connections
    await this.redisSub.quit();
    await this.redisClient.quit();

    this.logger.info('VideoAgent WebSocket Server closed');
  }
}

import express from 'express';
import cors from 'cors';
import helmet from 'helmet';
import { createServer } from 'http';
import dotenv from 'dotenv';

import { MageAgentClient } from './clients/MageAgentClient';
import { BullMQAdapter } from './queue/BullMQAdapter';
import { IQueueAdapter } from './queue/IQueueAdapter';
import { VideoAgentWebSocketServer } from './websocket/VideoAgentWebSocketServer';
import { HealthChecker } from './utils/health-checker';
import { GoogleOAuthManager } from './services/GoogleOAuthManager';
import { GoogleDriveProvider } from './services/GoogleDriveProvider';
import { setupVideoRoutes } from './routes/video.routes';
import { setupGoogleDriveRoutes } from './routes/gdrive.routes';
import { errorHandler } from './middleware/errorHandler';
import { usageTrackingMiddleware, flushPendingReports } from './middleware/usage-tracking';
import { VideoAgentConfig } from './types';
import Redis from 'ioredis';

// Load environment variables
dotenv.config();

/**
 * VideoAgent API Server
 * Zero-hardcoded-models architecture with hybrid Go/TypeScript processing
 *
 * REFACTORED: Now using BullMQ adapter pattern for queue management
 */
class VideoAgentServer {
  private app: express.Application;
  private server: ReturnType<typeof createServer>;
  private config: VideoAgentConfig;
  private mageAgent: MageAgentClient;
  private queueAdapter: IQueueAdapter;
  private websocketServer: VideoAgentWebSocketServer;
  private healthChecker: HealthChecker;
  private redisClient: Redis;
  private googleOAuthManager?: GoogleOAuthManager;
  private googleDriveProvider?: GoogleDriveProvider;

  constructor() {
    this.app = express();
    this.server = createServer(this.app);
    this.config = this.loadConfig();

    // Initialize clients
    this.mageAgent = new MageAgentClient(this.config.mageAgentUrl);

    // Initialize BullMQ adapter (replaces RedisProducer)
    this.queueAdapter = new BullMQAdapter({
      queueName: 'videoagent',
      redis: {
        host: new URL(this.config.redisUrl).hostname,
        port: parseInt(new URL(this.config.redisUrl).port || '6379'),
        password: new URL(this.config.redisUrl).password || undefined,
      },
      defaultJobOptions: {
        attempts: 3,
        backoff: {
          type: 'exponential',
          delay: 5000,
        },
        removeOnComplete: 100,
        removeOnFail: 500,
      },
    });

    this.websocketServer = new VideoAgentWebSocketServer(this.server, this.config.redisUrl);

    // Initialize Redis client for health checker
    this.redisClient = new Redis(this.config.redisUrl);

    // Initialize health checker with enhanced retry and caching
    this.healthChecker = new HealthChecker(
      this.config.mageAgentUrl,
      this.redisClient
    );

    // Initialize Google Drive if enabled
    if (this.config.enableGoogleDrive && this.config.googleClientId && this.config.googleClientSecret) {
      this.googleOAuthManager = new GoogleOAuthManager(
        this.config.googleClientId,
        this.config.googleClientSecret,
        this.config.googleRedirectUri || `http://localhost:${this.config.port}/videoagent/api/gdrive/auth/callback`,
        this.config.redisUrl,
        this.config.postgresUrl
      );
      this.googleDriveProvider = new GoogleDriveProvider(this.googleOAuthManager);
      console.log('✓ Google Drive integration enabled');
    }

    this.setupMiddleware();
    this.setupRoutes();
    this.setupErrorHandling();
  }

  /**
   * Load configuration from environment
   */
  private loadConfig(): VideoAgentConfig {
    return {
      port: parseInt(process.env.PORT || '3000'),
      redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
      postgresUrl: process.env.POSTGRES_URL || 'postgresql://unified_nexus:graphrag123@localhost:5432/nexus_graphrag',
      mageAgentUrl: process.env.MAGEAGENT_URL || 'http://localhost:3000',
      tempDir: process.env.TEMP_DIR || '/tmp/videoagent',
      maxVideoSize: parseInt(process.env.MAX_VIDEO_SIZE || '2147483648'), // 2GB
      enableGoogleDrive: process.env.ENABLE_GOOGLE_DRIVE === 'true',
      googleClientId: process.env.GOOGLE_CLIENT_ID,
      googleClientSecret: process.env.GOOGLE_CLIENT_SECRET,
      googleRedirectUri: process.env.GOOGLE_REDIRECT_URI,
    };
  }

  /**
   * Setup middleware
   */
  private setupMiddleware(): void {
    // Security
    this.app.use(helmet());
    this.app.use(cors({
      origin: process.env.CORS_ORIGIN || '*',
      credentials: true,
    }));

    // Body parsing
    this.app.use(express.json({ limit: '10mb' }));
    this.app.use(express.urlencoded({ extended: true, limit: '10mb' }));

    // Usage tracking middleware for billing and analytics
    this.app.use(usageTrackingMiddleware);

    // Request logging
    this.app.use((req, res, next) => {
      console.log(`${req.method} ${req.path}`);
      next();
    });
  }

  /**
   * Setup routes
   */
  private setupRoutes(): void {
    // Health check with enhanced caching and retry
    this.app.get('/health', async (req, res) => {
      try {
        const healthStatus = await this.healthChecker.getHealthStatus();

        const statusCode = healthStatus.status === 'healthy' ? 200 :
                          healthStatus.status === 'degraded' ? 200 : 503;

        res.status(statusCode).json(healthStatus);
      } catch (error) {
        res.status(503).json({
          status: 'unhealthy',
          error: error instanceof Error ? error.message : 'Unknown error',
          timestamp: new Date(),
        });
      }
    });

    // API routes - Enterprise Pattern: /videoagent/api/* for service-level namespace isolation
    this.app.use('/videoagent/api/video', setupVideoRoutes(this.queueAdapter, this.mageAgent));

    // Google Drive routes (if enabled)
    if (this.googleOAuthManager && this.googleDriveProvider) {
      this.app.use('/videoagent/api/gdrive', setupGoogleDriveRoutes(
        this.googleOAuthManager,
        this.googleDriveProvider,
        this.queueAdapter
      ));
    }

    // WebSocket info endpoint
    this.app.get('/videoagent/api/websocket/stats', (req, res) => {
      const stats = this.websocketServer.getStatistics();
      res.json({
        success: true,
        stats,
      });
    });

    // 404 handler
    this.app.use((req, res) => {
      res.status(404).json({
        success: false,
        error: 'Not found',
        path: req.path,
      });
    });
  }

  /**
   * Setup error handling
   */
  private setupErrorHandling(): void {
    this.app.use(errorHandler);
  }

  /**
   * Start server
   */
  async start(): Promise<void> {
    // Verify MageAgent connection
    const mageAgentHealthy = await this.mageAgent.healthCheck();
    if (!mageAgentHealthy) {
      console.warn('WARNING: MageAgent is not responding. Some features may not work.');
    } else {
      console.log('✓ MageAgent connection verified');
    }

    // Verify Redis connection via queue adapter
    try {
      await this.queueAdapter.getMetrics();
      console.log('✓ Redis/BullMQ connection verified');
    } catch (error) {
      throw new Error(`Redis/BullMQ connection failed: ${error instanceof Error ? error.message : 'Unknown error'}`);
    }

    // Start periodic health monitoring (60s intervals)
    this.healthChecker.startPeriodicMonitoring();
    console.log('✓ Periodic health monitoring started');

    // Start HTTP server
    await new Promise<void>((resolve) => {
      this.server.listen(this.config.port, () => {
        console.log('');
        console.log('='.repeat(60));
        console.log('VideoAgent API Server - READY (Enterprise Routing)');
        console.log('='.repeat(60));
        console.log(`Port: ${this.config.port}`);
        console.log(`MageAgent URL: ${this.config.mageAgentUrl}`);
        console.log(`Redis URL: ${this.config.redisUrl}`);
        console.log(`Google Drive: ${this.config.enableGoogleDrive ? 'Enabled' : 'Disabled'}`);
        console.log(`Routing: Enterprise pattern (/videoagent/api/*)`);
        console.log('');
        console.log('Endpoints:');
        console.log(`  POST   http://localhost:${this.config.port}/videoagent/api/video/process`);
        console.log(`  GET    http://localhost:${this.config.port}/videoagent/api/video/status/:jobId`);
        console.log(`  DELETE http://localhost:${this.config.port}/videoagent/api/video/cancel/:jobId`);
        console.log(`  GET    http://localhost:${this.config.port}/videoagent/api/video/metrics`);
        console.log(`  GET    http://localhost:${this.config.port}/health`);
        console.log('');
        console.log('WebSocket (Socket.IO):');
        console.log(`  Path: /videoagent/socket.io`);
        console.log(`  Namespaces:`);
        console.log(`    - /videoagent (general events)`);
        console.log(`    - /videoagent/jobs (job lifecycle)`);
        console.log(`    - /videoagent/progress (progress updates)`);
        console.log(`    - /videoagent/frames (frame extraction)`);
        console.log(`    - /videoagent/scenes (scene detection)`);
        console.log('='.repeat(60));
        console.log('');
        resolve();
      });
    });
  }

  /**
   * Graceful shutdown
   */
  async stop(): Promise<void> {
    console.log('Shutting down VideoAgent API server...');

    // Flush pending usage tracking reports
    try {
      await flushPendingReports();
      console.log('✓ Usage tracking reports flushed');
    } catch (error) {
      console.error('Failed to flush usage reports:', error);
    }

    // Stop periodic health monitoring
    this.healthChecker.cleanup();
    console.log('✓ Health monitoring stopped');

    // Close WebSocket server
    await this.websocketServer.close();
    console.log('✓ WebSocket server closed');

    // Close queue adapter
    await this.queueAdapter.shutdown(30000);
    console.log('✓ Queue adapter closed');

    // Close Redis client
    await this.redisClient.quit();
    console.log('✓ Redis client closed');

    // Close HTTP server
    await new Promise<void>((resolve, reject) => {
      this.server.close((err) => {
        if (err) reject(err);
        else resolve();
      });
    });
    console.log('✓ HTTP server closed');

    console.log('VideoAgent API server stopped');
  }
}

// Main execution
if (require.main === module) {
  const server = new VideoAgentServer();

  // Handle graceful shutdown
  process.on('SIGINT', async () => {
    await server.stop();
    process.exit(0);
  });

  process.on('SIGTERM', async () => {
    await server.stop();
    process.exit(0);
  });

  // Start server
  server.start().catch((error) => {
    console.error('Failed to start server:', error);
    process.exit(1);
  });
}

export { VideoAgentServer };

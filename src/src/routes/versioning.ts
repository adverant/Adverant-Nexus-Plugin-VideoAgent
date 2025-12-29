/**
 * API Versioning System
 *
 * Root Cause Fixed: No versioning strategy made it impossible to evolve
 * the API without breaking existing clients.
 *
 * Solution: URL-based versioning (/v1, /v2) with version detection,
 * deprecation warnings, and backward compatibility support.
 */

import { Express, Router, Request, Response, NextFunction } from 'express';
import { RedisProducer } from '../queue/RedisProducer';
import { MageAgentClient } from '../clients/MageAgentClient';

/**
 * API version configuration
 */
export interface VersionConfig {
  version: string;
  deprecated?: boolean;
  deprecationDate?: Date;
  sunsetDate?: Date;
  upgradeMessage?: string;
}

/**
 * Version metadata
 */
export interface VersionMetadata {
  current: string;
  supported: string[];
  deprecated: string[];
  sunset: string[];
}

/**
 * API Versioning Manager
 *
 * Manages multiple API versions with deprecation tracking and
 * automatic version detection.
 */
export class APIVersioning {
  private versions: Map<string, VersionConfig> = new Map();
  private defaultVersion: string = 'v2';

  constructor() {
    // Register API versions
    this.registerVersion({
      version: 'v1',
      deprecated: true,
      deprecationDate: new Date('2024-12-01'),
      sunsetDate: new Date('2025-06-01'),
      upgradeMessage: 'API v1 is deprecated. Please migrate to v2. See: /docs/migration-v1-to-v2',
    });

    this.registerVersion({
      version: 'v2',
      deprecated: false,
    });
  }

  /**
   * Register API version
   */
  registerVersion(config: VersionConfig): void {
    this.versions.set(config.version, config);
  }

  /**
   * Get version configuration
   */
  getVersion(version: string): VersionConfig | undefined {
    return this.versions.get(version);
  }

  /**
   * Get all versions
   */
  getAllVersions(): VersionMetadata {
    const supported: string[] = [];
    const deprecated: string[] = [];
    const sunset: string[] = [];
    const now = new Date();

    for (const [version, config] of this.versions) {
      if (config.sunsetDate && config.sunsetDate < now) {
        sunset.push(version);
      } else if (config.deprecated) {
        deprecated.push(version);
      } else {
        supported.push(version);
      }
    }

    return {
      current: this.defaultVersion,
      supported,
      deprecated,
      sunset,
    };
  }

  /**
   * Check if version is supported
   */
  isVersionSupported(version: string): boolean {
    const config = this.versions.get(version);
    if (!config) {
      return false;
    }

    // Check if sunset
    if (config.sunsetDate && config.sunsetDate < new Date()) {
      return false;
    }

    return true;
  }

  /**
   * Get default version
   */
  getDefaultVersion(): string {
    return this.defaultVersion;
  }

  /**
   * Set default version
   */
  setDefaultVersion(version: string): void {
    if (!this.versions.has(version)) {
      throw new Error(`Version ${version} not registered`);
    }
    this.defaultVersion = version;
  }
}

/**
 * Create version detection middleware
 *
 * Detects API version from URL, header, or query parameter.
 */
export function createVersionDetectionMiddleware(versioning: APIVersioning) {
  return (req: Request, res: Response, next: NextFunction): void => {
    // Extract version from URL (/v1/..., /v2/...)
    const urlMatch = req.path.match(/^\/v(\d+)\//);
    let version: string | undefined;

    if (urlMatch) {
      version = `v${urlMatch[1]}`;
    }

    // Fallback to header
    if (!version) {
      version = req.get('api-version') || req.get('x-api-version');
    }

    // Fallback to query parameter
    if (!version) {
      version = req.query.version as string;
    }

    // Fallback to default version
    if (!version) {
      version = versioning.getDefaultVersion();
    }

    // Store version in request
    (req as any).apiVersion = version;

    // Check if version is supported
    if (!versioning.isVersionSupported(version)) {
      res.status(400).json({
        success: false,
        error: `API version ${version} is not supported`,
        supportedVersions: versioning.getAllVersions().supported,
      });
      return;
    }

    // Add deprecation warning header
    const versionConfig = versioning.getVersion(version);
    if (versionConfig?.deprecated) {
      res.setHeader('X-API-Deprecation', 'true');
      res.setHeader('X-API-Deprecation-Message', versionConfig.upgradeMessage || 'This API version is deprecated');

      if (versionConfig.sunsetDate) {
        res.setHeader('X-API-Sunset', versionConfig.sunsetDate.toISOString());
      }
    }

    // Add version header to response
    res.setHeader('X-API-Version', version);

    next();
  };
}

/**
 * Setup versioned routes
 *
 * Configures all API versions with appropriate routes.
 */
export function setupVersionedRoutes(
  app: Express,
  redisProducer: RedisProducer,
  mageAgent: MageAgentClient,
  versioning: APIVersioning
): void {
  // Version detection middleware
  app.use('/videoagent/api', createVersionDetectionMiddleware(versioning));

  // API versions endpoint
  app.get('/videoagent/api/versions', (req, res) => {
    res.json({
      success: true,
      versions: versioning.getAllVersions(),
    });
  });

  // Setup V1 routes (legacy, deprecated)
  setupV1Routes(app, redisProducer, mageAgent);

  // Setup V2 routes (current)
  setupV2Routes(app, redisProducer, mageAgent);

  // Default to latest version (unversioned URLs)
  setupDefaultRoutes(app, redisProducer, mageAgent);
}

/**
 * Setup V1 routes (deprecated)
 */
function setupV1Routes(
  app: Express,
  redisProducer: RedisProducer,
  mageAgent: MageAgentClient
): void {
  const v1Router = Router();

  // V1 video processing endpoint (simplified, legacy format)
  v1Router.post('/video/process', async (req, res, next) => {
    try {
      // V1 format: simpler payload structure
      const { videoUrl, userId, options } = req.body;

      if (!videoUrl || !userId) {
        res.status(400).json({
          error: 'videoUrl and userId are required',
        });
        return;
      }

      // Enqueue job
      const jobId = require('uuid').v4();
      await redisProducer.enqueueJob({
        jobId,
        filename: `video_${jobId}.mp4`,
        videoUrl,
        userId,
        sourceType: 'url',
        options: {
          ...options,
          extractFrames: true,
          extractAudio: true,
          detectScenes: true,
          frameSamplingMode: 'keyframes',
          maxFrames: 50,
          targetLanguages: ['en'],
          qualityPreference: 'balanced',
        },
        enqueuedAt: new Date(),
      }, 'default');

      // V1 response format (no success field)
      res.json({
        jobId,
        status: 'queued',
      });
    } catch (error) {
      next(error);
    }
  });

  // V1 job status endpoint
  v1Router.get('/video/job/:jobId', async (req, res, next) => {
    try {
      const { jobId } = req.params;
      const status = await redisProducer.getJobStatus(jobId);

      if (!status) {
        res.status(404).json({
          error: 'Job not found',
        });
        return;
      }

      // V1 response format
      res.json({
        jobId,
        state: status.state,
        progress: status.progress || 0,
        failedReason: status.failedReason,
      });
    } catch (error) {
      next(error);
    }
  });

  app.use('/videoagent/api/v1', v1Router);
}

/**
 * Setup V2 routes (current)
 */
function setupV2Routes(
  app: Express,
  redisProducer: RedisProducer,
  mageAgent: MageAgentClient
): void {
  const v2Router = Router();

  // V2 video processing endpoint (enhanced with more options)
  v2Router.post('/video/process', async (req, res, next) => {
    try {
      const { videoUrl, userId, sessionId, options, sourceType } = req.body;

      if (!userId) {
        res.status(400).json({
          success: false,
          error: 'userId is required',
        });
        return;
      }

      if (!videoUrl && !req.file) {
        res.status(400).json({
          success: false,
          error: 'videoUrl or video file is required',
        });
        return;
      }

      // Enqueue job with full V2 options
      const jobId = require('uuid').v4();
      const enqueuedAt = new Date();
      await redisProducer.enqueueJob({
        jobId,
        filename: req.file?.originalname || `video_${jobId}.mp4`,
        videoUrl,
        videoBuffer: req.file?.buffer,
        userId,
        sessionId,
        sourceType: sourceType || 'url',
        options: {
          extractFrames: options?.extractFrames ?? true,
          frameSamplingMode: options?.frameSamplingMode || 'keyframes',
          frameSampleRate: options?.frameSampleRate || 1,
          maxFrames: options?.maxFrames || 50,
          extractAudio: options?.extractAudio ?? true,
          transcribeAudio: options?.transcribeAudio ?? false,
          detectScenes: options?.detectScenes ?? true,
          detectObjects: options?.detectObjects ?? false,
          extractText: options?.extractText ?? false,
          classifyContent: options?.classifyContent ?? false,
          generateSummary: options?.generateSummary ?? false,
          targetLanguages: options?.targetLanguages || ['en'],
          qualityPreference: options?.qualityPreference || 'balanced',
          customAnalysis: options?.customAnalysis,
          additionalMetadata: options?.additionalMetadata,
        },
        metadata: {},
        enqueuedAt,
      }, 'default');

      // V2 response format (with success field)
      res.json({
        success: true,
        jobId,
        status: 'enqueued',
        message: 'Video processing job enqueued successfully',
        enqueuedAt,
      });
    } catch (error) {
      next(error);
    }
  });

  // V2 job status endpoint (enhanced with more details)
  v2Router.get('/video/job/:jobId', async (req, res, next) => {
    try {
      const { jobId } = req.params;
      const status = await redisProducer.getJobStatus(jobId);

      if (!status) {
        res.status(404).json({
          success: false,
          error: 'Job not found',
        });
        return;
      }

      // V2 response format (comprehensive)
      res.json({
        success: true,
        jobId,
        status: {
          state: status.state,
          progress: status.progress || 0,
          currentStage: (status as any).currentStage,
          failedReason: status.failedReason,
          processedAt: (status as any).processedAt,
          finishedAt: (status as any).finishedAt,
        },
      });
    } catch (error) {
      next(error);
    }
  });

  // V2 job cancellation endpoint (new in V2)
  v2Router.delete('/video/job/:jobId', async (req, res, next) => {
    try {
      const { jobId } = req.params;
      const removed = await redisProducer.removeJob(jobId);

      if (!removed) {
        res.status(404).json({
          success: false,
          error: 'Job not found or already completed',
        });
        return;
      }

      res.json({
        success: true,
        message: 'Job cancelled successfully',
      });
    } catch (error) {
      next(error);
    }
  });

  // V2 queue stats endpoint (new in V2)
  v2Router.get('/video/queue/stats', async (req, res, next) => {
    try {
      const stats = await redisProducer.getQueueStats();

      res.json({
        success: true,
        stats,
      });
    } catch (error) {
      next(error);
    }
  });

  app.use('/videoagent/api/v2', v2Router);
}

/**
 * Setup default (unversioned) routes
 *
 * Points to latest stable version.
 */
function setupDefaultRoutes(
  app: Express,
  redisProducer: RedisProducer,
  mageAgent: MageAgentClient
): void {
  // Forward unversioned routes to V2 (latest)
  const defaultRouter = Router();

  defaultRouter.use((req, res, next) => {
    // Rewrite path to V2
    req.url = `/v2${req.url}`;
    next();
  });

  app.use('/videoagent/api/video', defaultRouter);
}

/**
 * Create version compatibility layer
 *
 * Handles request/response transformation between versions.
 */
export function createVersionCompatibilityMiddleware(versioning: APIVersioning) {
  return (req: Request, res: Response, next: NextFunction): void => {
    const version = (req as any).apiVersion;

    if (version === 'v1') {
      // V1 compatibility: transform V2 responses to V1 format
      const originalJson = res.json.bind(res);
      res.json = function (body: any): Response {
        // Transform V2 response to V1 format
        if (body.success !== undefined) {
          // Remove success field for V1
          const { success, ...v1Body } = body;
          return originalJson(v1Body);
        }
        return originalJson(body);
      };
    }

    next();
  };
}

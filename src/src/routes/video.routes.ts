import { Router } from 'express';
import multer from 'multer';
import { v4 as uuidv4 } from 'uuid';
import * as fs from 'fs';
import { IQueueAdapter, JobData } from '../queue/IQueueAdapter';
import { MageAgentClient } from '../clients/MageAgentClient';
import { asyncHandler, ApiError } from '../middleware/errorHandler';
import { ProcessVideoRequest } from '../types';

const router = Router();
const upload = multer({ storage: multer.memoryStorage(), limits: { fileSize: 2 * 1024 * 1024 * 1024 } }); // 2GB limit

/**
 * Setup video routes with queue adapter
 *
 * Refactored to use IQueueAdapter interface for queue system abstraction.
 * This allows swapping queue implementations (BullMQ, Asynq, SQS) without changing routes.
 *
 * @param queueAdapter - Queue adapter implementation
 * @param mageAgent - MageAgent client for vision analysis
 * @returns Express router
 */
export function setupVideoRoutes(queueAdapter: IQueueAdapter, mageAgent: MageAgentClient): Router {
  /**
   * POST /api/video/process
   * Process video from URL or upload
   *
   * Enhanced with:
   * - Comprehensive validation (required fields, URL format, filename safety)
   * - Priority support (1-10)
   * - Delay support (scheduled processing)
   * - Retry configuration
   */
  router.post('/process', upload.single('video'), asyncHandler(async (req, res) => {
    const body: ProcessVideoRequest = req.body;

    // Comprehensive validation
    if (!body.userId || typeof body.userId !== 'string') {
      throw new ApiError(400, 'userId is required and must be a string');
    }

    if (!body.filename || typeof body.filename !== 'string') {
      throw new ApiError(400, 'filename is required and must be a string');
    }

    // Path traversal prevention
    if (body.filename.includes('..') || body.filename.includes('/') || body.filename.includes('\\')) {
      throw new ApiError(400, 'Invalid filename: path traversal detected');
    }

    // Validate source
    if (!body.videoUrl && !req.file) {
      throw new ApiError(400, 'Either videoUrl or video file upload is required');
    }

    // URL format validation
    if (body.videoUrl) {
      // Allow file:// protocol for pre-downloaded files from FileProcess
      if (body.videoUrl.startsWith('file://')) {
        const filePath = body.videoUrl.substring(7); // Remove 'file://' prefix
        // Basic path traversal prevention
        if (filePath.includes('..')) {
          throw new ApiError(400, 'Invalid file path: path traversal detected');
        }
        // Validate path starts with allowed directories
        const allowedPrefixes = ['/tmp/', '/shared/', '/data/'];
        if (!allowedPrefixes.some(prefix => filePath.startsWith(prefix))) {
          throw new ApiError(400, 'Invalid file path: must be in allowed directory');
        }
      } else {
        try {
          const url = new URL(body.videoUrl);
          if (!['http:', 'https:'].includes(url.protocol)) {
            throw new ApiError(400, 'Invalid videoUrl: protocol must be http, https, or file');
          }
        } catch {
          throw new ApiError(400, 'Invalid videoUrl format');
        }
      }
    }

    // Validate options
    if (!body.options || typeof body.options !== 'object') {
      throw new ApiError(400, 'Processing options are required and must be an object');
    }

    // Create job data
    const jobId = uuidv4();
    const jobData: JobData = {
      jobId,
      userId: body.userId,
      videoUrl: body.videoUrl || '', // Will be set later for uploads
      filename: body.filename,
      options: {
        extractMetadata: body.options.extractMetadata ?? true,
        detectScenes: body.options.detectScenes ?? false,
        analyzeFrames: body.options.analyzeFrames ?? false,
        transcribeAudio: body.options.transcribeAudio ?? false,
        maxFrames: body.options.maxFrames,
        frameInterval: body.options.frameInterval,
      },
      enqueuedAt: new Date(),
    };

    // Handle file uploads
    if (req.file) {
      // Ensure upload directory exists
      const uploadDir = '/tmp/videoagent-uploads';
      await fs.promises.mkdir(uploadDir, { recursive: true });

      const tempPath = `${uploadDir}/${jobId}-${body.filename.replace(/[^a-zA-Z0-9._-]/g, '_')}`;
      await fs.promises.writeFile(tempPath, req.file.buffer);

      jobData.videoUrl = `file://${tempPath}`;
    }

    // Enqueue job with options
    const returnedJobId = await queueAdapter.enqueue(jobData, {
      priority: body.priority || 5, // 1 (highest) to 10 (lowest)
      delay: body.delay || 0,
      attempts: 3,
      backoff: {
        type: 'exponential',
        delay: 5000,
      },
      timeout: 300000, // 5 minutes
      removeOnComplete: 100,
      removeOnFail: 500,
    });

    res.json({
      success: true,
      jobId: returnedJobId,
      status: 'enqueued',
      message: 'Video processing job enqueued successfully',
      enqueuedAt: jobData.enqueuedAt,
    });
  }));

  /**
   * GET /api/video/status/:jobId
   * Get job status and results
   *
   * Enhanced with:
   * - Complete job metadata (timestamps, attempts, error details)
   * - Progress percentage (0-100)
   * - Result data when completed
   */
  router.get('/status/:jobId', asyncHandler(async (req, res) => {
    const { jobId } = req.params;

    if (!jobId || typeof jobId !== 'string') {
      throw new ApiError(400, 'Invalid jobId');
    }

    const status = await queueAdapter.getStatus(jobId);
    if (!status) {
      throw new ApiError(404, 'Job not found');
    }

    res.json({
      success: true,
      jobId: status.jobId,
      status: status.status,
      progress: status.progress,
      result: status.result,
      error: status.error,
      createdAt: status.createdAt,
      processedAt: status.processedAt,
      completedAt: status.completedAt,
      attemptsMade: status.attemptsMade,
    });
  }));

  /**
   * DELETE /api/video/cancel/:jobId
   * Cancel a pending or active job
   *
   * Note: Cannot cancel completed or failed jobs
   */
  router.delete('/cancel/:jobId', asyncHandler(async (req, res) => {
    const { jobId } = req.params;

    if (!jobId || typeof jobId !== 'string') {
      throw new ApiError(400, 'Invalid jobId');
    }

    const cancelled = await queueAdapter.cancel(jobId);
    if (!cancelled) {
      throw new ApiError(404, 'Job not found or already completed/failed');
    }

    res.json({
      success: true,
      message: 'Job cancelled successfully',
      jobId,
    });
  }));

  /**
   * GET /api/video/metrics
   * Get queue health metrics for observability
   *
   * Returns:
   * - waiting: Jobs waiting to be processed
   * - active: Jobs currently being processed
   * - completed: Successfully completed jobs
   * - failed: Failed jobs
   * - delayed: Jobs scheduled for future execution
   * - paused: Jobs in paused queue
   *
   * Prometheus-compatible format
   */
  router.get('/metrics', asyncHandler(async (req, res) => {
    const metrics = await queueAdapter.getMetrics();

    res.json({
      success: true,
      metrics: {
        waiting: metrics.waiting,
        active: metrics.active,
        completed: metrics.completed,
        failed: metrics.failed,
        delayed: metrics.delayed,
        paused: metrics.paused,
        total: metrics.waiting + metrics.active + metrics.completed + metrics.failed + metrics.delayed + metrics.paused,
      },
      timestamp: new Date().toISOString(),
    });
  }));

  return router;
}

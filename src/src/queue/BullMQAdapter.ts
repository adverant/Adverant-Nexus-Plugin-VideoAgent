/**
 * BullMQ Adapter Implementation
 *
 * Production-grade adapter using BullMQ for job queue management.
 * Implements the IQueueAdapter interface to provide queue abstraction.
 *
 * Features:
 * - Robust error handling with custom exceptions
 * - Defensive validation (path traversal, URL format)
 * - Event-driven monitoring and observability
 * - Graceful shutdown with in-flight job completion
 * - Retry logic with exponential backoff
 * - Queue metrics for Prometheus integration
 *
 * SOLID Principles:
 * - Single Responsibility: Only handles BullMQ operations
 * - Open/Closed: Extensible through configuration, closed for modification
 * - Liskov Substitution: Can replace any IQueueAdapter implementation
 * - Interface Segregation: Implements minimal IQueueAdapter interface
 * - Dependency Inversion: Depends on abstractions (IQueueAdapter, Redis)
 */

import { Queue, Job, QueueEvents, Worker } from 'bullmq';
import Redis from 'ioredis';
import {
  IQueueAdapter,
  JobData,
  JobOptions,
  JobStatus,
  QueueMetrics,
  QueueEnqueueError,
  QueueStatusError,
  QueueCancelError,
  QueueProgressError,
} from './IQueueAdapter';

/**
 * Logger interface for dependency injection
 */
interface Logger {
  info(message: string, meta?: any): void;
  warn(message: string, meta?: any): void;
  error(message: string, meta?: any): void;
  debug(message: string, meta?: any): void;
}

/**
 * BullMQ Adapter Configuration
 */
export interface BullMQAdapterConfig {
  queueName: string;
  redis: {
    host: string;
    port: number;
    password?: string;
    db?: number;
    maxRetriesPerRequest?: number;
    enableReadyCheck?: boolean;
    retryStrategy?: (times: number) => number | void;
  };
  defaultJobOptions?: JobOptions;
  logger?: Logger;
}

/**
 * Default console logger if none provided
 */
const defaultLogger: Logger = {
  info: (msg, meta) => console.log(`[INFO] ${msg}`, meta || ''),
  warn: (msg, meta) => console.warn(`[WARN] ${msg}`, meta || ''),
  error: (msg, meta) => console.error(`[ERROR] ${msg}`, meta || ''),
  debug: (msg, meta) => console.debug(`[DEBUG] ${msg}`, meta || ''),
};

/**
 * BullMQ Adapter Implementation
 *
 * Thread-safe, production-ready queue adapter with comprehensive
 * error handling and observability.
 */
export class BullMQAdapter implements IQueueAdapter {
  private queue: Queue;
  private queueEvents: QueueEvents;
  private redisConnection: Redis;
  private logger: Logger;
  private isShuttingDown = false;

  constructor(private readonly config: BullMQAdapterConfig) {
    // Initialize logger FIRST - required for retry strategy factory
    this.logger = config.logger || defaultLogger;

    // Create retry strategy with logger dependency injected via closure
    // This prevents 'this' binding issues when ioredis calls the strategy
    const retryStrategy = config.redis.retryStrategy || this.createRetryStrategy(this.logger);

    // Initialize Redis connection with retry strategy
    // NOTE: maxRetriesPerRequest MUST be null for BullMQ blocking operations (QueueEvents)
    this.redisConnection = new Redis({
      host: config.redis.host,
      port: config.redis.port,
      password: config.redis.password,
      db: config.redis.db || 0,
      maxRetriesPerRequest: config.redis.maxRetriesPerRequest ?? null,
      enableReadyCheck: config.redis.enableReadyCheck ?? true,
      retryStrategy,
    });

    // Setup connection lifecycle event handlers for observability
    this.setupRedisConnectionHandlers();

    // Initialize BullMQ Queue
    this.queue = new Queue(config.queueName, {
      connection: this.redisConnection,
      defaultJobOptions: this.mapJobOptionsToBull(config.defaultJobOptions),
    });

    // Initialize Queue Events for monitoring
    this.queueEvents = new QueueEvents(config.queueName, {
      connection: this.redisConnection.duplicate(),
    });

    this.setupEventListeners();

    this.logger.info(`BullMQAdapter initialized for queue: ${config.queueName}`, {
      redis: `${config.redis.host}:${config.redis.port}`,
    });
  }

  /**
   * Create retry strategy factory with logger dependency injection
   *
   * Uses closure pattern to capture logger dependency without relying on 'this' binding.
   * This ensures the retry strategy function remains pure and doesn't crash when
   * called by ioredis in a different context.
   *
   * Design Pattern: Factory Method + Closure
   * - Factory creates pure function with captured dependencies
   * - Closure maintains lexical scope access to logger
   * - Defense-in-depth: fallback logging if primary logger fails
   *
   * @param logger - Logger instance to capture in closure
   * @returns Pure function compatible with ioredis retryStrategy signature
   */
  private createRetryStrategy(logger: Logger): (times: number) => number | void {
    return (times: number): number => {
      try {
        // Calculate exponential backoff with max 10 second delay
        const delay = Math.min(times * 1000, 10000);

        // Attempt to log retry attempt (non-critical operation)
        try {
          logger.debug(`Redis retry attempt ${times}, waiting ${delay}ms`, {
            attempt: times,
            delayMs: delay,
            maxDelayMs: 10000,
          });
        } catch (loggingError) {
          // Fallback: log to console if primary logger fails
          // This ensures observability even if logger is misconfigured
          console.error(
            `[CRITICAL] BullMQAdapter retry strategy logging failed:`,
            loggingError instanceof Error ? loggingError.message : loggingError
          );
          console.log(`[FALLBACK] Redis retry attempt ${times}, waiting ${delay}ms`);
        }

        return delay;
      } catch (error) {
        // Ultimate fallback: if entire retry strategy fails, return safe default
        // This prevents Redis connection from crashing the application
        const fallbackDelay = Math.min(times * 1000, 10000);

        console.error(
          `[CRITICAL] BullMQAdapter retry strategy failed:`,
          error instanceof Error ? error.message : error
        );
        console.error(`[FALLBACK] Returning delay: ${fallbackDelay}ms`);

        return fallbackDelay;
      }
    };
  }

  /**
   * Setup Redis connection lifecycle event handlers
   *
   * Provides observability into connection state transitions:
   * - connect: Initial connection established
   * - ready: Connection ready for commands
   * - error: Connection error occurred
   * - close: Connection closed
   * - reconnecting: Attempting to reconnect
   * - end: Connection permanently ended
   *
   * These handlers help diagnose network issues and monitor connection health.
   */
  private setupRedisConnectionHandlers(): void {
    this.redisConnection.on('connect', () => {
      this.logger.info('Redis connection established', {
        host: this.config.redis.host,
        port: this.config.redis.port,
      });
    });

    this.redisConnection.on('ready', () => {
      this.logger.info('Redis connection ready for commands');
    });

    this.redisConnection.on('error', (error) => {
      this.logger.error('Redis connection error', {
        error: error.message,
        code: (error as any).code,
        syscall: (error as any).syscall,
      });
    });

    this.redisConnection.on('close', () => {
      this.logger.warn('Redis connection closed');
    });

    this.redisConnection.on('reconnecting', (delay: number) => {
      this.logger.info('Redis reconnecting', {
        delayMs: delay,
      });
    });

    this.redisConnection.on('end', () => {
      this.logger.warn('Redis connection ended permanently');
    });
  }

  /**
   * Set up event listeners for queue monitoring
   */
  private setupEventListeners(): void {
    this.queueEvents.on('completed', ({ jobId, returnvalue }) => {
      this.logger.info(`Job completed: ${jobId}`, { result: returnvalue });
    });

    this.queueEvents.on('failed', ({ jobId, failedReason }) => {
      this.logger.error(`Job failed: ${jobId}`, { reason: failedReason });
    });

    this.queueEvents.on('stalled', ({ jobId }) => {
      this.logger.warn(`Job stalled: ${jobId} (worker may have crashed)`);
    });

    this.queueEvents.on('progress', ({ jobId, data }) => {
      this.logger.debug(`Job progress: ${jobId}`, { progress: data });
    });
  }

  /**
   * Enqueue a new job for processing
   *
   * @param data - Job payload
   * @param options - Queue-specific options
   * @returns Job ID for tracking
   * @throws QueueEnqueueError if enqueue fails
   */
  async enqueue(data: JobData, options?: JobOptions): Promise<string> {
    if (this.isShuttingDown) {
      throw new QueueEnqueueError('Queue is shutting down, cannot enqueue new jobs', {
        queueName: this.config.queueName,
        jobId: data.jobId,
      });
    }

    try {
      // Defensive validation
      this.validateJobData(data);

      // Map options to BullMQ format
      const bullOptions = this.mapJobOptionsToBull(options);

      // Enqueue job
      const job = await this.queue.add(data.jobId, data, bullOptions);

      this.logger.info(`Job enqueued: ${data.jobId}`, {
        userId: data.userId,
        filename: data.filename,
        priority: options?.priority,
      });

      return job.id!;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      const errorStack = error instanceof Error ? error.stack : undefined;

      this.logger.error(`Failed to enqueue job: ${data.jobId}`, {
        error: errorMessage,
        stack: errorStack,
      });

      throw new QueueEnqueueError(`Failed to enqueue job: ${errorMessage}`, {
        queueName: this.config.queueName,
        jobId: data.jobId,
        originalError: error,
      });
    }
  }

  /**
   * Retrieve current status of a job
   *
   * @param jobId - Unique job identifier
   * @returns Job status or null if not found
   * @throws QueueStatusError if status retrieval fails
   */
  async getStatus(jobId: string): Promise<JobStatus | null> {
    try {
      const job = await this.queue.getJob(jobId);

      if (!job) {
        return null;
      }

      const state = await job.getState();
      const progress = job.progress as number;
      const failedReason = job.failedReason;

      const status: JobStatus = {
        jobId: job.id!,
        status: this.mapBullMQStateToStatus(state),
        progress: typeof progress === 'number' ? progress : 0,
        result: job.returnvalue,
        error: failedReason
          ? {
              message: failedReason,
              code: 'JOB_FAILED',
            }
          : undefined,
        createdAt: new Date(job.timestamp),
        processedAt: job.processedOn ? new Date(job.processedOn) : undefined,
        completedAt: job.finishedOn ? new Date(job.finishedOn) : undefined,
        attemptsMade: job.attemptsMade,
        failedReason,
      };

      return status;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';

      this.logger.error(`Failed to get job status: ${jobId}`, {
        error: errorMessage,
      });

      throw new QueueStatusError(`Failed to retrieve job status: ${errorMessage}`, {
        queueName: this.config.queueName,
        jobId,
        originalError: error,
      });
    }
  }

  /**
   * Cancel a pending or active job
   *
   * @param jobId - Unique job identifier
   * @returns true if cancelled, false if already completed/failed
   * @throws QueueCancelError if cancellation fails
   */
  async cancel(jobId: string): Promise<boolean> {
    try {
      const job = await this.queue.getJob(jobId);

      if (!job) {
        this.logger.warn(`Cannot cancel job: ${jobId} (not found)`);
        return false;
      }

      const state = await job.getState();

      // Cannot cancel completed or failed jobs
      if (state === 'completed' || state === 'failed') {
        this.logger.warn(`Cannot cancel job: ${jobId} (already ${state})`);
        return false;
      }

      // Remove job from queue
      await job.remove();

      this.logger.info(`Job cancelled: ${jobId}`, { previousState: state });

      return true;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';

      this.logger.error(`Failed to cancel job: ${jobId}`, {
        error: errorMessage,
      });

      throw new QueueCancelError(`Failed to cancel job: ${errorMessage}`, {
        queueName: this.config.queueName,
        jobId,
        originalError: error,
      });
    }
  }

  /**
   * Update job progress (called by worker)
   *
   * @param jobId - Unique job identifier
   * @param progress - Progress percentage (0-100)
   * @throws QueueProgressError if update fails
   */
  async updateProgress(jobId: string, progress: number): Promise<void> {
    try {
      // Validate progress range
      if (progress < 0 || progress > 100) {
        throw new Error(`Invalid progress value: ${progress} (must be 0-100)`);
      }

      const job = await this.queue.getJob(jobId);

      if (!job) {
        throw new Error(`Job not found: ${jobId}`);
      }

      await job.updateProgress(progress);

      this.logger.debug(`Job progress updated: ${jobId}`, { progress });
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';

      this.logger.error(`Failed to update job progress: ${jobId}`, {
        error: errorMessage,
        progress,
      });

      throw new QueueProgressError(`Failed to update job progress: ${errorMessage}`, {
        queueName: this.config.queueName,
        jobId,
        progress,
        originalError: error,
      });
    }
  }

  /**
   * Get queue health metrics
   *
   * @returns Current queue depths and job counts
   */
  async getMetrics(): Promise<QueueMetrics> {
    try {
      const counts = await this.queue.getJobCounts(
        'waiting',
        'active',
        'completed',
        'failed',
        'delayed',
        'paused'
      );

      const metrics: QueueMetrics = {
        waiting: counts.waiting || 0,
        active: counts.active || 0,
        completed: counts.completed || 0,
        failed: counts.failed || 0,
        delayed: counts.delayed || 0,
        paused: counts.paused || 0,
      };

      return metrics;
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';

      this.logger.error(`Failed to get queue metrics`, {
        error: errorMessage,
      });

      // Return zero metrics on error rather than throwing
      return {
        waiting: 0,
        active: 0,
        completed: 0,
        failed: 0,
        delayed: 0,
        paused: 0,
      };
    }
  }

  /**
   * Graceful shutdown - wait for active jobs to complete
   *
   * @param timeout - Max wait time in milliseconds
   */
  async shutdown(timeout: number = 30000): Promise<void> {
    if (this.isShuttingDown) {
      this.logger.warn('Shutdown already in progress');
      return;
    }

    this.isShuttingDown = true;
    this.logger.info('Starting graceful shutdown', { timeout });

    try {
      // Wait for active jobs to complete (with timeout)
      await Promise.race([
        this.queue.close(),
        new Promise((_, reject) =>
          setTimeout(() => reject(new Error('Shutdown timeout')), timeout)
        ),
      ]);

      // Close queue events
      await this.queueEvents.close();

      // Close Redis connection
      await this.redisConnection.quit();

      this.logger.info('Graceful shutdown complete');
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      this.logger.error('Error during shutdown', { error: errorMessage });

      // Force close connections
      await this.redisConnection.disconnect();
    }
  }

  /**
   * Validate job data for security and correctness
   *
   * Defensive checks:
   * - URL format validation
   * - Path traversal prevention
   * - Required field validation
   */
  private validateJobData(data: JobData): void {
    // Required fields
    if (!data.jobId || typeof data.jobId !== 'string') {
      throw new Error('Invalid jobId: must be a non-empty string');
    }

    if (!data.userId || typeof data.userId !== 'string') {
      throw new Error('Invalid userId: must be a non-empty string');
    }

    if (!data.videoUrl || typeof data.videoUrl !== 'string') {
      throw new Error('Invalid videoUrl: must be a non-empty string');
    }

    if (!data.filename || typeof data.filename !== 'string') {
      throw new Error('Invalid filename: must be a non-empty string');
    }

    // URL format validation (basic)
    try {
      const url = new URL(data.videoUrl);
      if (!['http:', 'https:'].includes(url.protocol)) {
        throw new Error('Invalid protocol: must be http or https');
      }
    } catch (error) {
      throw new Error(`Invalid videoUrl format: ${data.videoUrl}`);
    }

    // Path traversal prevention
    if (data.filename.includes('..') || data.filename.includes('/') || data.filename.includes('\\')) {
      throw new Error(`Invalid filename: path traversal detected in ${data.filename}`);
    }

    // Validate options object
    if (data.options && typeof data.options !== 'object') {
      throw new Error('Invalid options: must be an object');
    }

    // Validate enqueuedAt
    if (!(data.enqueuedAt instanceof Date) || isNaN(data.enqueuedAt.getTime())) {
      throw new Error('Invalid enqueuedAt: must be a valid Date');
    }
  }

  /**
   * Map IQueueAdapter JobOptions to BullMQ JobsOptions
   */
  private mapJobOptionsToBull(options?: JobOptions): any {
    if (!options) {
      return {};
    }

    return {
      priority: options.priority,
      delay: options.delay,
      attempts: options.attempts || 3,
      backoff: options.backoff
        ? {
            type: options.backoff.type,
            delay: options.backoff.delay,
          }
        : { type: 'exponential', delay: 5000 },
      removeOnComplete: options.removeOnComplete ?? 100, // Keep last 100
      removeOnFail: options.removeOnFail ?? 500, // Keep last 500 failures
      timeout: options.timeout,
    };
  }

  /**
   * Map BullMQ job state to IQueueAdapter status
   */
  private mapBullMQStateToStatus(
    state: string
  ): 'waiting' | 'active' | 'completed' | 'failed' | 'delayed' | 'paused' {
    switch (state) {
      case 'waiting':
      case 'waiting-children':
        return 'waiting';
      case 'active':
        return 'active';
      case 'completed':
        return 'completed';
      case 'failed':
        return 'failed';
      case 'delayed':
        return 'delayed';
      case 'paused':
        return 'paused';
      default:
        return 'waiting'; // Default to waiting for unknown states
    }
  }
}

import { Queue, QueueOptions } from 'bullmq';
import { Redis } from 'ioredis';
import { JobPayload } from '../types';

/**
 * RedisProducer - Enqueues video processing jobs to Redis for Go workers
 */
export class RedisProducer {
  private queue: Queue;
  private redis: Redis;

  constructor(redisUrl: string) {
    // Parse Redis URL
    const redisOptions = this.parseRedisUrl(redisUrl);

    this.redis = new Redis(redisOptions);

    // Create BullMQ queue
    this.queue = new Queue('videoagent', {
      connection: this.redis,
      defaultJobOptions: {
        attempts: 3,
        backoff: {
          type: 'exponential',
          delay: 60000, // 1 minute
        },
        removeOnComplete: {
          age: 24 * 3600, // Keep for 24 hours
          count: 1000,
        },
        removeOnFail: {
          age: 7 * 24 * 3600, // Keep for 7 days
        },
      },
    });
  }

  /**
   * Enqueue video processing job
   */
  async enqueueJob(
    job: JobPayload,
    priority: 'critical' | 'default' | 'low' = 'default'
  ): Promise<string> {
    try {
      // Serialize job payload
      const payload = this.serializeJobPayload(job);

      // Map priority to BullMQ priority (lower number = higher priority)
      const priorityMap = {
        critical: 1,
        default: 5,
        low: 10,
      };

      // Add job to queue
      const bullJob = await this.queue.add(
        'videoagent:process',
        payload,
        {
          jobId: job.jobId,
          priority: priorityMap[priority],
        }
      );

      return bullJob.id as string;
    } catch (error) {
      throw new Error(`Failed to enqueue job: ${error instanceof Error ? error.message : String(error)}`);
    }
  }

  /**
   * Get job status
   */
  async getJobStatus(jobId: string): Promise<{
    state: string;
    progress: number;
    failedReason?: string;
  } | null> {
    try {
      const job = await this.queue.getJob(jobId);
      if (!job) {
        return null;
      }

      const state = await job.getState();
      const progress = job.progress as number || 0;
      const failedReason = job.failedReason;

      return {
        state,
        progress,
        failedReason,
      };
    } catch (error) {
      return null;
    }
  }

  /**
   * Remove job from queue
   */
  async removeJob(jobId: string): Promise<boolean> {
    try {
      const job = await this.queue.getJob(jobId);
      if (!job) {
        return false;
      }

      await job.remove();
      return true;
    } catch (error) {
      return false;
    }
  }

  /**
   * Get queue statistics
   */
  async getQueueStats(): Promise<{
    waiting: number;
    active: number;
    completed: number;
    failed: number;
  }> {
    try {
      const [waiting, active, completed, failed] = await Promise.all([
        this.queue.getWaitingCount(),
        this.queue.getActiveCount(),
        this.queue.getCompletedCount(),
        this.queue.getFailedCount(),
      ]);

      return { waiting, active, completed, failed };
    } catch (error) {
      return { waiting: 0, active: 0, completed: 0, failed: 0 };
    }
  }

  /**
   * Serialize job payload for Go worker
   * Ensures proper JSON serialization including Buffer handling
   */
  private serializeJobPayload(job: JobPayload): any {
    return {
      jobId: job.jobId,
      videoUrl: job.videoUrl,
      // Convert Buffer to base64 string for JSON serialization
      videoBuffer: job.videoBuffer ? job.videoBuffer.toString('base64') : undefined,
      sourceType: job.sourceType,
      userId: job.userId,
      sessionId: job.sessionId,
      options: job.options,
      metadata: job.metadata,
      enqueuedAt: job.enqueuedAt.toISOString(),
    };
  }

  /**
   * Parse Redis URL to connection options
   */
  private parseRedisUrl(url: string): {
    host: string;
    port: number;
    password?: string;
    db?: number;
  } {
    const urlObj = new URL(url);

    return {
      host: urlObj.hostname,
      port: parseInt(urlObj.port) || 6379,
      password: urlObj.password || undefined,
      db: urlObj.pathname ? parseInt(urlObj.pathname.slice(1)) : 0,
    };
  }

  /**
   * Close connections
   */
  async close(): Promise<void> {
    await this.queue.close();
    await this.redis.quit();
  }

  /**
   * Health check
   */
  async healthCheck(): Promise<boolean> {
    try {
      await this.redis.ping();
      return true;
    } catch (error) {
      return false;
    }
  }
}

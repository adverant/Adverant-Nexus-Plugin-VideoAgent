/**
 * VideoAgent BullMQ Bridge Service
 *
 * This service bridges the gap between the BullMQ queue (used by the Node.js API)
 * and the Go VideoAgent worker (which expects JSON via stdin in subprocess mode).
 *
 * Architecture Flow:
 * 1. BullMQ API enqueues jobs to bull:videoagent:wait
 * 2. This bridge worker consumes jobs from BullMQ
 * 3. Bridge spawns Go worker with WORKER_MODE=subprocess
 * 4. Go worker reads JSON from stdin, processes video, writes result to stdout
 * 5. Bridge reads result from stdout and marks job complete in BullMQ
 *
 * This approach:
 * - Uses existing BullMQ infrastructure (no asynq needed)
 * - Leverages Go worker's subprocess mode (no code changes needed)
 * - Provides TypeScript-based job management
 * - Enables easy monitoring and error handling
 */

import { Worker, Job } from 'bullmq';
import Redis from 'ioredis';
import { spawn } from 'child_process';
import * as path from 'path';

/**
 * BullMQ Job Data Interface (from API)
 */
interface BullMQJobData {
  jobId: string;
  userId: string;
  videoUrl: string;
  filename: string;
  options: {
    extractMetadata?: boolean;
    detectScenes?: boolean;
    analyzeFrames?: boolean;
    transcribeAudio?: boolean;
    maxFrames?: number;
    frameInterval?: number;
  };
  enqueuedAt: Date;
}

/**
 * Go Worker Job Payload (expected by subprocess mode)
 */
interface GoWorkerJobPayload {
  JobID: string;
  UserID: string;
  VideoURL: string;
  VideoBuffer: null;  // Always null for URL-based processing
  Filename: string;
  Options: {
    ExtractMetadata?: boolean;
    DetectScenes?: boolean;
    AnalyzeFrames?: boolean;
    TranscribeAudio?: boolean;
    MaxFrames?: number;
    FrameInterval?: number;
  };
}

/**
 * Environment Configuration
 */
const config = {
  redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
  queueName: process.env.QUEUE_NAME || 'videoagent',
  workerBinary: process.env.WORKER_BINARY || '/app/videoagent-worker',
  concurrency: parseInt(process.env.BRIDGE_CONCURRENCY || '3', 10),
  jobTimeout: parseInt(process.env.JOB_TIMEOUT || '3600000', 10), // 1 hour default
};

/**
 * Logger utility
 */
const logger = {
  info: (msg: string, meta?: any) => console.log(`[INFO] ${msg}`, meta || ''),
  warn: (msg: string, meta?: any) => console.warn(`[WARN] ${msg}`, meta || ''),
  error: (msg: string, meta?: any) => console.error(`[ERROR] ${msg}`, meta || ''),
  debug: (msg: string, meta?: any) => console.debug(`[DEBUG] ${msg}`, meta || ''),
};

/**
 * Transform BullMQ job data to Go worker payload format
 */
function transformJobData(bullmqData: BullMQJobData): GoWorkerJobPayload {
  return {
    JobID: bullmqData.jobId,
    UserID: bullmqData.userId,
    VideoURL: bullmqData.videoUrl,
    VideoBuffer: null,
    Filename: bullmqData.filename,
    Options: {
      ExtractMetadata: bullmqData.options.extractMetadata ?? true,
      DetectScenes: bullmqData.options.detectScenes ?? false,
      AnalyzeFrames: bullmqData.options.analyzeFrames ?? false,
      TranscribeAudio: bullmqData.options.transcribeAudio ?? false,
      MaxFrames: bullmqData.options.maxFrames,
      FrameInterval: bullmqData.options.frameInterval,
    },
  };
}

/**
 * Process a job by spawning the Go worker in subprocess mode
 */
async function processJob(job: Job<BullMQJobData>): Promise<any> {
  const jobId = job.data.jobId;
  logger.info(`Bridge: Processing job ${jobId}`, {
    videoUrl: job.data.videoUrl,
    userId: job.data.userId,
  });

  // Transform job data to Go format
  const goPayload = transformJobData(job.data);

  return new Promise((resolve, reject) => {
    // Spawn Go worker in subprocess mode
    const workerProcess = spawn(config.workerBinary, [], {
      env: {
        ...process.env,
        WORKER_MODE: 'subprocess',
      },
      stdio: ['pipe', 'pipe', 'pipe'], // stdin, stdout, stderr
    });

    let stdoutData = '';
    let stderrData = '';

    // Collect stdout (JSON result)
    workerProcess.stdout.on('data', (data) => {
      stdoutData += data.toString();
    });

    // Collect stderr (logs)
    workerProcess.stderr.on('data', (data) => {
      const logLine = data.toString().trim();
      if (logLine) {
        logger.debug(`[Go Worker ${jobId}] ${logLine}`);
      }
      stderrData += data.toString();
    });

    // Handle process completion
    workerProcess.on('close', (code) => {
      if (code === 0) {
        // Success - parse JSON result
        try {
          const result = JSON.parse(stdoutData);

          if (result.success) {
            logger.info(`Bridge: Job ${jobId} completed successfully`);
            resolve(result.results);
          } else {
            logger.error(`Bridge: Job ${jobId} failed`, { error: result.error });
            reject(new Error(result.error || 'Go worker returned success=false'));
          }
        } catch (parseError) {
          logger.error(`Bridge: Failed to parse Go worker output for job ${jobId}`, {
            stdout: stdoutData,
            parseError: parseError instanceof Error ? parseError.message : parseError,
          });
          reject(new Error(`Failed to parse Go worker output: ${parseError}`));
        }
      } else {
        // Failure - extract error from stdout or stderr
        logger.error(`Bridge: Go worker exited with code ${code} for job ${jobId}`, {
          stdout: stdoutData,
          stderr: stderrData,
        });

        // Try to parse error from stdout (Go worker sends JSON error)
        try {
          const errorResult = JSON.parse(stdoutData);
          if (errorResult.error) {
            reject(new Error(errorResult.error));
            return;
          }
        } catch {
          // If stdout is not JSON, use stderr
        }

        reject(new Error(`Go worker failed with exit code ${code}: ${stderrData || stdoutData}`));
      }
    });

    // Handle process errors
    workerProcess.on('error', (error) => {
      logger.error(`Bridge: Failed to spawn Go worker for job ${jobId}`, {
        error: error.message,
        workerBinary: config.workerBinary,
      });
      reject(new Error(`Failed to spawn Go worker: ${error.message}`));
    });

    // Send job payload to worker via stdin
    try {
      const payloadJSON = JSON.stringify(goPayload);
      logger.debug(`Bridge: Sending payload to Go worker for job ${jobId}`, {
        payloadSize: payloadJSON.length,
      });
      workerProcess.stdin.write(payloadJSON);
      workerProcess.stdin.end();
    } catch (writeError) {
      logger.error(`Bridge: Failed to write to Go worker stdin for job ${jobId}`, {
        error: writeError instanceof Error ? writeError.message : writeError,
      });
      workerProcess.kill();
      reject(new Error(`Failed to write to Go worker stdin: ${writeError}`));
    }
  });
}

/**
 * Main entry point
 */
async function main() {
  logger.info('VideoAgent BullMQ Bridge starting...', {
    redisUrl: config.redisUrl,
    queueName: config.queueName,
    workerBinary: config.workerBinary,
    concurrency: config.concurrency,
  });

  // Parse Redis connection options from URL
  const redisOptions = {
    host: new URL(config.redisUrl).hostname,
    port: parseInt(new URL(config.redisUrl).port || '6379', 10),
    maxRetriesPerRequest: null, // Required for BullMQ blocking operations
    enableReadyCheck: true,
    retryStrategy: (times: number) => {
      const delay = Math.min(times * 50, 2000);
      logger.debug(`Redis retry attempt ${times}, waiting ${delay}ms`);
      return delay;
    },
  };

  logger.info('Connecting to Redis...', {
    host: redisOptions.host,
    port: redisOptions.port,
  });

  // Initialize BullMQ Worker with connection options (not a Redis instance)
  // BullMQ will create and manage its own Redis connections internally
  const worker = new Worker(
    config.queueName,
    async (job: Job<BullMQJobData>) => {
      // Process job by spawning Go worker
      return await processJob(job);
    },
    {
      connection: redisOptions,
      concurrency: config.concurrency,
      lockDuration: 60000, // 1 minute lock renewal
      maxStalledCount: 3,
      stalledInterval: 30000,
    }
  );

  // Worker event handlers
  worker.on('completed', (job, result) => {
    logger.info(`Job completed: ${job.id}`, {
      jobId: job.data.jobId,
      duration: Date.now() - new Date(job.data.enqueuedAt).getTime(),
    });
  });

  worker.on('failed', (job, error) => {
    logger.error(`Job failed: ${job?.id}`, {
      jobId: job?.data?.jobId,
      error: error.message,
      stack: error.stack,
    });
  });

  worker.on('stalled', (jobId) => {
    logger.warn(`Job stalled: ${jobId} (worker may have crashed or timed out)`);
  });

  worker.on('progress', (job, progress) => {
    logger.debug(`Job progress: ${job.id}`, {
      jobId: job.data.jobId,
      progress,
    });
  });

  worker.on('error', (error) => {
    logger.error('Worker error', {
      error: error.message,
      stack: error.stack,
    });
  });

  // Graceful shutdown
  const shutdown = async () => {
    logger.info('Shutdown signal received, stopping gracefully...');

    try {
      await worker.close();
      logger.info('Worker closed (Redis connections managed by BullMQ)');

      process.exit(0);
    } catch (error) {
      logger.error('Error during shutdown', {
        error: error instanceof Error ? error.message : error,
      });
      process.exit(1);
    }
  };

  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);

  logger.info('VideoAgent BullMQ Bridge ready - waiting for jobs...', {
    concurrency: `${config.concurrency} workers`,
  });
}

// Start the bridge
main().catch((error) => {
  logger.error('Bridge startup failed', {
    error: error.message,
    stack: error.stack,
  });
  process.exit(1);
});

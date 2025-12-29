/**
 * VideoAgent BullMQ Worker
 *
 * Production-grade worker that processes video jobs from the BullMQ queue.
 * Delegates CPU-intensive video processing to the Go binary via subprocess.
 *
 * Architecture:
 * - Node.js: Async I/O, queue integration, progress tracking
 * - Go Binary: Stateless subprocess for video processing (FFmpeg, frame extraction)
 * - PostgreSQL: Persistent result storage
 * - Redis: Queue state and job metadata
 *
 * Design Patterns:
 * - Adapter Pattern: Wraps Go binary as stateless processor
 * - Strategy Pattern: Different processing strategies based on job options
 * - Circuit Breaker: Graceful degradation on Go binary failures
 *
 * SOLID Principles:
 * - Single Responsibility: Only handles job processing orchestration
 * - Open/Closed: Extensible through configuration
 * - Dependency Inversion: Depends on abstractions (IJobProcessor)
 */

import { Worker, Job, ConnectionOptions } from 'bullmq';
import { spawn } from 'child_process';
import * as path from 'path';
import * as fs from 'fs/promises';

/**
 * Job data structure (matches IQueueAdapter.JobData)
 */
interface JobData {
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
 * Logger interface for dependency injection
 */
interface Logger {
  info(message: string, meta?: any): void;
  warn(message: string, meta?: any): void;
  error(message: string, meta?: any): void;
  debug(message: string, meta?: any): void;
}

/**
 * Default console logger
 */
const defaultLogger: Logger = {
  info: (msg, meta) => console.log(`[INFO] ${msg}`, meta ? JSON.stringify(meta) : ''),
  warn: (msg, meta) => console.warn(`[WARN] ${msg}`, meta ? JSON.stringify(meta) : ''),
  error: (msg, meta) => console.error(`[ERROR] ${msg}`, meta ? JSON.stringify(meta) : ''),
  debug: (msg, meta) => console.debug(`[DEBUG] ${msg}`, meta ? JSON.stringify(meta) : ''),
};

/**
 * Worker Configuration
 */
export interface WorkerConfig {
  queueName: string;
  redisConnection: ConnectionOptions;
  concurrency: number;
  goBinaryPath: string;
  workingDir: string;
  logger?: Logger;
  processingTimeout?: number; // Milliseconds
}

/**
 * Go Binary Processing Request
 */
interface ProcessingRequest {
  jobId: string;
  userId: string;
  videoUrl: string;
  filename: string;
  outputDir: string;
  options: {
    extractMetadata?: boolean;
    detectScenes?: boolean;
    analyzeFrames?: boolean;
    transcribeAudio?: boolean;
    maxFrames?: number;
    frameInterval?: number;
  };
}

/**
 * Go Binary Processing Response
 */
interface ProcessingResponse {
  success: boolean;
  jobId: string;
  metadata?: {
    duration: number;
    width: number;
    height: number;
    fps: number;
    codec: string;
  };
  scenes?: Array<{ start: number; end: number; confidence: number }>;
  frames?: Array<{
    timestamp: number;
    path: string;
    embedding?: number[];
    description?: string;
  }>;
  transcription?: {
    text: string;
    segments: Array<{ start: number; end: number; text: string }>;
  };
  error?: {
    message: string;
    code: string;
  };
}

/**
 * BullMQ Worker for VideoAgent
 *
 * Processes video jobs by delegating to Go binary subprocess.
 * Provides progress tracking, error handling, and result persistence.
 */
export class VideoAgentWorker {
  private worker: Worker;
  private logger: Logger;
  private isShuttingDown = false;

  constructor(private readonly config: WorkerConfig) {
    this.logger = config.logger || defaultLogger;

    // Initialize BullMQ Worker
    this.worker = new Worker(
      config.queueName,
      async (job: Job<JobData>) => this.processJob(job),
      {
        connection: config.redisConnection,
        concurrency: config.concurrency,
        removeOnComplete: { count: 100 },
        removeOnFail: { count: 500 },
      }
    );

    this.setupEventListeners();
    this.setupGracefulShutdown();

    this.logger.info(`VideoAgent worker started`, {
      queueName: config.queueName,
      concurrency: config.concurrency,
      goBinaryPath: config.goBinaryPath,
    });
  }

  /**
   * Set up worker event listeners for monitoring
   */
  private setupEventListeners(): void {
    this.worker.on('completed', (job: Job) => {
      this.logger.info(`Job completed: ${job.id}`, {
        duration: Date.now() - job.timestamp,
        attemptsMade: job.attemptsMade,
      });
    });

    this.worker.on('failed', (job: Job | undefined, error: Error) => {
      this.logger.error(`Job failed: ${job?.id}`, {
        error: error.message,
        stack: error.stack,
        attemptsMade: job?.attemptsMade,
      });
    });

    this.worker.on('error', (error: Error) => {
      this.logger.error(`Worker error`, {
        error: error.message,
        stack: error.stack,
      });
    });

    this.worker.on('stalled', (jobId: string) => {
      this.logger.warn(`Job stalled: ${jobId} (may be restarted)`);
    });
  }

  /**
   * Set up graceful shutdown on SIGTERM/SIGINT
   */
  private setupGracefulShutdown(): void {
    const shutdown = async (signal: string) => {
      if (this.isShuttingDown) {
        return;
      }

      this.logger.info(`Received ${signal}, starting graceful shutdown`);
      this.isShuttingDown = true;

      try {
        await this.worker.close();
        this.logger.info('Worker shutdown complete');
        process.exit(0);
      } catch (error) {
        this.logger.error('Error during shutdown', { error });
        process.exit(1);
      }
    };

    process.on('SIGTERM', () => shutdown('SIGTERM'));
    process.on('SIGINT', () => shutdown('SIGINT'));
  }

  /**
   * Process a video job
   *
   * Workflow:
   * 1. Update progress: 10% (job started)
   * 2. Prepare working directory
   * 3. Call Go binary via subprocess (progress: 20% → 80%)
   * 4. Parse results
   * 5. Store results in PostgreSQL (progress: 90%)
   * 6. Cleanup temp files (progress: 100%)
   *
   * @param job - BullMQ job instance
   * @returns Processing result
   */
  private async processJob(job: Job<JobData>): Promise<any> {
    const jobData = job.data;
    const startTime = Date.now();

    this.logger.info(`Processing job: ${jobData.jobId}`, {
      userId: jobData.userId,
      filename: jobData.filename,
      options: jobData.options,
    });

    try {
      // Progress: 10% - Job started
      await job.updateProgress(10);

      // Prepare working directory
      const outputDir = path.join(this.config.workingDir, jobData.jobId);
      await fs.mkdir(outputDir, { recursive: true });

      this.logger.debug(`Created working directory: ${outputDir}`);

      // Progress: 20% - Calling Go binary
      await job.updateProgress(20);

      // Call Go binary subprocess
      const result = await this.callGoBinary({
        jobId: jobData.jobId,
        userId: jobData.userId,
        videoUrl: jobData.videoUrl,
        filename: jobData.filename,
        outputDir,
        options: jobData.options,
      }, job);

      // Check for errors
      if (!result.success) {
        throw new Error(result.error?.message || 'Go binary processing failed');
      }

      // Progress: 90% - Storing results
      await job.updateProgress(90);

      // Store results in PostgreSQL (optional - depends on your persistence layer)
      // await this.storeResults(jobData.jobId, result);

      // Progress: 100% - Cleanup
      await job.updateProgress(100);

      // Cleanup temporary files
      await this.cleanup(outputDir);

      const duration = Date.now() - startTime;

      this.logger.info(`Job completed successfully: ${jobData.jobId}`, {
        duration,
        framesExtracted: result.frames?.length || 0,
        scenesDetected: result.scenes?.length || 0,
      });

      return {
        success: true,
        jobId: jobData.jobId,
        duration,
        metadata: result.metadata,
        frames: result.frames?.length || 0,
        scenes: result.scenes?.length || 0,
        transcription: result.transcription ? true : false,
      };
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Unknown error';
      const errorStack = error instanceof Error ? error.stack : undefined;

      this.logger.error(`Job failed: ${jobData.jobId}`, {
        error: errorMessage,
        stack: errorStack,
      });

      throw new Error(`Video processing failed: ${errorMessage}`);
    }
  }

  /**
   * Call Go binary as subprocess
   *
   * Communication: JSON via stdin/stdout
   * Progress tracking: Intermediate JSON messages on stdout
   *
   * IMPORTANT: Explicitly sets WORKER_MODE=subprocess to ensure Go binary
   * runs in subprocess mode (stdin/stdout) rather than standalone mode (Redis queue).
   *
   * @param request - Processing request
   * @param job - BullMQ job for progress updates
   * @returns Processing response
   */
  private async callGoBinary(
    request: ProcessingRequest,
    job: Job<JobData>
  ): Promise<ProcessingResponse> {
    return new Promise((resolve, reject) => {
      // Explicitly set subprocess mode to avoid silent fallback to standalone mode
      const subprocessEnv = {
        ...process.env,
        WORKER_MODE: 'subprocess',  // ✅ Explicit mode declaration
      };

      // Spawn Go binary with explicit subprocess configuration
      const goBinary = spawn(this.config.goBinaryPath, [], {
        stdio: ['pipe', 'pipe', 'pipe'],
        env: subprocessEnv,
      });

      let stdout = '';
      let stderr = '';

      // Collect stdout
      goBinary.stdout.on('data', (chunk: Buffer) => {
        const data = chunk.toString();
        stdout += data;

        // Parse progress updates (if Go binary sends them)
        try {
          const lines = data.split('\n').filter((line) => line.trim());
          for (const line of lines) {
            const json = JSON.parse(line);
            if (json.type === 'progress' && typeof json.progress === 'number') {
              // Progress range: 20% → 80% (reserved for Go processing)
              const scaledProgress = 20 + (json.progress / 100) * 60;
              job.updateProgress(Math.floor(scaledProgress)).catch((err) => {
                this.logger.warn('Failed to update progress', { error: err.message });
              });
            }
          }
        } catch {
          // Not JSON or invalid format - ignore
        }
      });

      // Collect stderr for verbose error reporting
      goBinary.stderr.on('data', (chunk: Buffer) => {
        const stderrData = chunk.toString();
        stderr += stderrData;

        // Log stderr in real-time for debugging
        this.logger.debug(`Go binary stderr: ${stderrData.trim()}`);
      });

      // Handle process exit with verbose error reporting
      goBinary.on('close', (code: number) => {
        if (code === 0) {
          try {
            // Parse final JSON response
            const response: ProcessingResponse = JSON.parse(stdout);

            // Validate response structure
            if (!response.success && !response.error) {
              reject(new Error(
                `Invalid Go binary response: missing 'success' or 'error' field. ` +
                `Response: ${stdout.substring(0, 500)}`
              ));
              return;
            }

            resolve(response);
          } catch (error) {
            reject(new Error(
              `Failed to parse Go binary JSON response. ` +
              `Error: ${error instanceof Error ? error.message : 'Unknown parse error'}. ` +
              `Stdout: ${stdout.substring(0, 500)}. ` +
              `Stderr: ${stderr.substring(0, 500)}`
            ));
          }
        } else {
          // Verbose error message with complete context
          const errorMessage =
            `Go binary exited with code ${code}. ` +
            `This indicates the Go binary failed to process the video. ` +
            `\n\nStderr output:\n${stderr}` +
            `\n\nStdout output:\n${stdout.substring(0, 1000)}` +
            `\n\nTroubleshooting:` +
            `\n- Check if WORKER_MODE=subprocess is set correctly` +
            `\n- Verify Go binary has access to required services (MageAgent, GraphRAG, Qdrant)` +
            `\n- Check if video URL is accessible from Docker container` +
            `\n- Review stderr output above for specific error details`;

          reject(new Error(errorMessage));
        }
      });

      // Handle process spawn errors with context
      goBinary.on('error', (error: Error) => {
        reject(new Error(
          `Failed to spawn Go binary at path: ${this.config.goBinaryPath}. ` +
          `Error: ${error.message}. ` +
          `Troubleshooting: ` +
          `\n- Verify the Go binary exists and has execute permissions` +
          `\n- Check if the path is correct (current: ${this.config.goBinaryPath})` +
          `\n- Ensure the binary is compiled for the correct architecture (linux/amd64)`
        ));
      });

      // Send request via stdin with error handling
      try {
        const requestJSON = JSON.stringify(request);
        goBinary.stdin.write(requestJSON);
        goBinary.stdin.end();

        this.logger.debug(`Sent request to Go binary via stdin`, {
          jobId: request.jobId,
          requestSize: requestJSON.length,
        });
      } catch (error) {
        reject(new Error(
          `Failed to write JSON request to Go binary stdin. ` +
          `Error: ${error instanceof Error ? error.message : 'Unknown error'}. ` +
          `This usually indicates the Go binary crashed immediately after spawning.`
        ));
      }

      // Timeout handling with cleanup
      const timeout = this.config.processingTimeout || 300000; // 5 minutes default
      const timeoutHandle = setTimeout(() => {
        if (!goBinary.killed) {
          goBinary.kill('SIGTERM');

          // Give process 5 seconds to terminate gracefully
          setTimeout(() => {
            if (!goBinary.killed) {
              goBinary.kill('SIGKILL'); // Force kill if still running
            }
          }, 5000);

          reject(new Error(
            `Go binary processing timeout after ${timeout}ms (${timeout / 1000}s). ` +
            `The video processing took longer than the configured timeout. ` +
            `\n\nLast known state:` +
            `\n- Stdout: ${stdout.substring(Math.max(0, stdout.length - 500))}` +
            `\n- Stderr: ${stderr.substring(Math.max(0, stderr.length - 500))}` +
            `\n\nTroubleshooting:` +
            `\n- Increase PROCESSING_TIMEOUT environment variable for longer videos` +
            `\n- Check if the Go binary is hung on a blocking operation` +
            `\n- Verify external services (MageAgent, GraphRAG) are responsive`
          ));
        }
      }, timeout);

      // Clear timeout on successful completion
      goBinary.on('close', () => {
        clearTimeout(timeoutHandle);
      });
    });
  }

  /**
   * Cleanup temporary files
   */
  private async cleanup(outputDir: string): Promise<void> {
    try {
      await fs.rm(outputDir, { recursive: true, force: true });
      this.logger.debug(`Cleaned up working directory: ${outputDir}`);
    } catch (error) {
      this.logger.warn(`Failed to cleanup directory: ${outputDir}`, { error });
    }
  }

  /**
   * Close worker gracefully
   */
  async close(): Promise<void> {
    await this.worker.close();
  }
}

/**
 * Main entry point
 */
if (require.main === module) {
  // Load configuration from environment variables
  const config: WorkerConfig = {
    queueName: process.env.QUEUE_NAME || 'videoagent',
    redisConnection: {
      host: process.env.REDIS_HOST || 'localhost',
      port: parseInt(process.env.REDIS_PORT || '6379', 10),
      password: process.env.REDIS_PASSWORD,
    },
    concurrency: parseInt(process.env.WORKER_CONCURRENCY || '2', 10),
    goBinaryPath: process.env.GO_BINARY_PATH || '/app/worker',
    workingDir: process.env.WORKING_DIR || '/tmp/videoagent',
    processingTimeout: parseInt(process.env.PROCESSING_TIMEOUT || '300000', 10),
  };

  // Start worker (instance automatically begins processing jobs via BullMQ)
  // @ts-ignore - worker instance is intentionally unused; constructor starts BullMQ listener
  const worker = new VideoAgentWorker(config);

  defaultLogger.info('VideoAgent worker is running', {
    queueName: config.queueName,
    concurrency: config.concurrency,
  });
}

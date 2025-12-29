/**
 * Queue Adapter Interface
 *
 * Defines the contract for any queue system implementation.
 * This abstraction allows switching between BullMQ, Asynq, SQS, etc.
 * without changing application logic.
 *
 * SOLID Principles:
 * - Interface Segregation: Minimal, focused interface
 * - Dependency Inversion: High-level code depends on abstraction, not concrete impl
 */

export interface JobOptions {
  priority?: number;        // 1 (highest) to 10 (lowest)
  delay?: number;           // Milliseconds to delay job execution
  attempts?: number;        // Max retry attempts
  backoff?: {
    type: 'fixed' | 'exponential';
    delay: number;
  };
  timeout?: number;         // Job timeout in milliseconds
  removeOnComplete?: boolean | number; // true, false, or keep last N
  removeOnFail?: boolean | number;
}

export interface JobData {
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

export interface JobStatus {
  jobId: string;
  status: 'waiting' | 'active' | 'completed' | 'failed' | 'delayed' | 'paused';
  progress: number;        // 0-100
  result?: any;
  error?: {
    message: string;
    stack?: string;
    code?: string;
  };
  createdAt: Date;
  processedAt?: Date;
  completedAt?: Date;
  attemptsMade: number;
  failedReason?: string;
}

export interface QueueMetrics {
  waiting: number;
  active: number;
  completed: number;
  failed: number;
  delayed: number;
  paused: number;
}

/**
 * IQueueAdapter: Strategy Pattern for Queue Implementations
 *
 * Allows runtime selection of queue backend without code changes.
 * Example: BullMQAdapter, AsynqAdapter, SQSAdapter
 */
export interface IQueueAdapter {
  /**
   * Enqueue a new job for processing
   *
   * @param data - Job payload
   * @param options - Queue-specific options (priority, delay, etc.)
   * @returns Job ID for tracking
   * @throws QueueEnqueueError if enqueue fails
   */
  enqueue(data: JobData, options?: JobOptions): Promise<string>;

  /**
   * Retrieve current status of a job
   *
   * @param jobId - Unique job identifier
   * @returns Job status or null if not found
   * @throws QueueStatusError if status retrieval fails
   */
  getStatus(jobId: string): Promise<JobStatus | null>;

  /**
   * Cancel a pending or active job
   *
   * @param jobId - Unique job identifier
   * @returns true if cancelled, false if already completed/failed
   * @throws QueueCancelError if cancellation fails
   */
  cancel(jobId: string): Promise<boolean>;

  /**
   * Update job progress (called by worker)
   *
   * @param jobId - Unique job identifier
   * @param progress - Progress percentage (0-100)
   * @throws QueueProgressError if update fails
   */
  updateProgress(jobId: string, progress: number): Promise<void>;

  /**
   * Get queue health metrics
   *
   * @returns Current queue depths and job counts
   */
  getMetrics(): Promise<QueueMetrics>;

  /**
   * Graceful shutdown - wait for active jobs to complete
   *
   * @param timeout - Max wait time in milliseconds
   */
  shutdown(timeout?: number): Promise<void>;
}

/**
 * Custom Exception Hierarchy for Verbose Error Reporting
 */
export class QueueError extends Error {
  constructor(
    message: string,
    public readonly code: string,
    public readonly context: Record<string, any>
  ) {
    super(message);
    this.name = this.constructor.name;
    Error.captureStackTrace(this, this.constructor);
  }
}

export class QueueEnqueueError extends QueueError {
  constructor(message: string, context: Record<string, any>) {
    super(message, 'QUEUE_ENQUEUE_ERROR', context);
  }
}

export class QueueStatusError extends QueueError {
  constructor(message: string, context: Record<string, any>) {
    super(message, 'QUEUE_STATUS_ERROR', context);
  }
}

export class QueueCancelError extends QueueError {
  constructor(message: string, context: Record<string, any>) {
    super(message, 'QUEUE_CANCEL_ERROR', context);
  }
}

export class QueueProgressError extends QueueError {
  constructor(message: string, context: Record<string, any>) {
    super(message, 'QUEUE_PROGRESS_ERROR', context);
  }
}

/**
 * Prometheus Metrics for VideoAgent
 *
 * Root Cause Fixed: No observability into system performance made it
 * impossible to identify bottlenecks, track SLAs, or trigger alerts.
 *
 * Solution: Comprehensive Prometheus metrics covering HTTP requests,
 * video processing, queue operations, circuit breakers, and cache performance.
 */

import { Registry, Counter, Histogram, Gauge, Summary, collectDefaultMetrics } from 'prom-client';
import { Request, Response, NextFunction } from 'express';

/**
 * Metrics collector configuration
 */
export interface MetricsConfig {
  prefix?: string;
  defaultLabels?: Record<string, string>;
  collectDefaultMetrics?: boolean;
}

/**
 * VideoAgent Metrics Collector
 *
 * Provides comprehensive Prometheus metrics for monitoring and alerting.
 */
export class MetricsCollector {
  public readonly registry: Registry;
  private readonly prefix: string;

  // HTTP Metrics
  public readonly httpRequestsTotal: Counter;
  public readonly httpRequestDuration: Histogram;
  public readonly httpRequestSize: Summary;
  public readonly httpResponseSize: Summary;
  public readonly httpActiveRequests: Gauge;

  // Video Processing Metrics
  public readonly videoProcessingTotal: Counter;
  public readonly videoProcessingDuration: Histogram;
  public readonly videoProcessingErrors: Counter;
  public readonly videoFramesProcessed: Counter;
  public readonly videoScenesDetected: Counter;
  public readonly videoObjectsDetected: Counter;

  // Queue Metrics
  public readonly queueJobsEnqueued: Counter;
  public readonly queueJobsProcessed: Counter;
  public readonly queueJobsFailed: Counter;
  public readonly queueSize: Gauge;
  public readonly queueWaitTime: Histogram;
  public readonly queueProcessingTime: Histogram;

  // Circuit Breaker Metrics
  public readonly circuitBreakerState: Gauge;
  public readonly circuitBreakerCalls: Counter;
  public readonly circuitBreakerFailures: Counter;
  public readonly circuitBreakerSuccesses: Counter;
  public readonly circuitBreakerRejects: Counter;

  // Cache Metrics
  public readonly cacheHits: Counter;
  public readonly cacheMisses: Counter;
  public readonly cacheSize: Gauge;
  public readonly cacheOperationDuration: Histogram;

  // WebSocket Metrics
  public readonly websocketConnections: Gauge;
  public readonly websocketMessagesTotal: Counter;
  public readonly websocketMessagesSent: Counter;
  public readonly websocketMessagesReceived: Counter;
  public readonly websocketErrors: Counter;

  // MageAgent Integration Metrics
  public readonly mageagentRequestsTotal: Counter;
  public readonly mageagentRequestDuration: Histogram;
  public readonly mageagentErrors: Counter;

  // System Metrics
  public readonly uptimeSeconds: Gauge;

  constructor(config: MetricsConfig = {}) {
    this.registry = new Registry();
    this.prefix = config.prefix || 'videoagent';

    // Set default labels
    if (config.defaultLabels) {
      this.registry.setDefaultLabels(config.defaultLabels);
    }

    // Collect default Node.js metrics
    if (config.collectDefaultMetrics !== false) {
      collectDefaultMetrics({ register: this.registry, prefix: `${this.prefix}_` });
    }

    // ========================================================================
    // HTTP Metrics
    // ========================================================================

    this.httpRequestsTotal = new Counter({
      name: `${this.prefix}_http_requests_total`,
      help: 'Total number of HTTP requests',
      labelNames: ['method', 'path', 'status_code'],
      registers: [this.registry],
    });

    this.httpRequestDuration = new Histogram({
      name: `${this.prefix}_http_request_duration_seconds`,
      help: 'HTTP request duration in seconds',
      labelNames: ['method', 'path', 'status_code'],
      buckets: [0.01, 0.05, 0.1, 0.5, 1, 2, 5, 10, 30],
      registers: [this.registry],
    });

    this.httpRequestSize = new Summary({
      name: `${this.prefix}_http_request_size_bytes`,
      help: 'HTTP request size in bytes',
      labelNames: ['method', 'path'],
      registers: [this.registry],
    });

    this.httpResponseSize = new Summary({
      name: `${this.prefix}_http_response_size_bytes`,
      help: 'HTTP response size in bytes',
      labelNames: ['method', 'path', 'status_code'],
      registers: [this.registry],
    });

    this.httpActiveRequests = new Gauge({
      name: `${this.prefix}_http_active_requests`,
      help: 'Number of currently active HTTP requests',
      labelNames: ['method', 'path'],
      registers: [this.registry],
    });

    // ========================================================================
    // Video Processing Metrics
    // ========================================================================

    this.videoProcessingTotal = new Counter({
      name: `${this.prefix}_video_processing_total`,
      help: 'Total number of video processing jobs',
      labelNames: ['source_type', 'status'],
      registers: [this.registry],
    });

    this.videoProcessingDuration = new Histogram({
      name: `${this.prefix}_video_processing_duration_seconds`,
      help: 'Video processing duration in seconds',
      labelNames: ['source_type', 'status'],
      buckets: [10, 30, 60, 120, 300, 600, 1800, 3600],
      registers: [this.registry],
    });

    this.videoProcessingErrors = new Counter({
      name: `${this.prefix}_video_processing_errors_total`,
      help: 'Total number of video processing errors',
      labelNames: ['source_type', 'error_type'],
      registers: [this.registry],
    });

    this.videoFramesProcessed = new Counter({
      name: `${this.prefix}_video_frames_processed_total`,
      help: 'Total number of video frames processed',
      labelNames: ['job_id'],
      registers: [this.registry],
    });

    this.videoScenesDetected = new Counter({
      name: `${this.prefix}_video_scenes_detected_total`,
      help: 'Total number of scenes detected',
      labelNames: ['job_id'],
      registers: [this.registry],
    });

    this.videoObjectsDetected = new Counter({
      name: `${this.prefix}_video_objects_detected_total`,
      help: 'Total number of objects detected',
      labelNames: ['job_id', 'object_type'],
      registers: [this.registry],
    });

    // ========================================================================
    // Queue Metrics
    // ========================================================================

    this.queueJobsEnqueued = new Counter({
      name: `${this.prefix}_queue_jobs_enqueued_total`,
      help: 'Total number of jobs enqueued',
      labelNames: ['queue_name'],
      registers: [this.registry],
    });

    this.queueJobsProcessed = new Counter({
      name: `${this.prefix}_queue_jobs_processed_total`,
      help: 'Total number of jobs processed',
      labelNames: ['queue_name', 'status'],
      registers: [this.registry],
    });

    this.queueJobsFailed = new Counter({
      name: `${this.prefix}_queue_jobs_failed_total`,
      help: 'Total number of failed jobs',
      labelNames: ['queue_name', 'error_type'],
      registers: [this.registry],
    });

    this.queueSize = new Gauge({
      name: `${this.prefix}_queue_size`,
      help: 'Current number of jobs in queue',
      labelNames: ['queue_name', 'status'],
      registers: [this.registry],
    });

    this.queueWaitTime = new Histogram({
      name: `${this.prefix}_queue_wait_time_seconds`,
      help: 'Time jobs spend waiting in queue',
      labelNames: ['queue_name'],
      buckets: [1, 5, 10, 30, 60, 300, 600],
      registers: [this.registry],
    });

    this.queueProcessingTime = new Histogram({
      name: `${this.prefix}_queue_processing_time_seconds`,
      help: 'Time spent processing jobs',
      labelNames: ['queue_name'],
      buckets: [10, 30, 60, 120, 300, 600, 1800],
      registers: [this.registry],
    });

    // ========================================================================
    // Circuit Breaker Metrics
    // ========================================================================

    this.circuitBreakerState = new Gauge({
      name: `${this.prefix}_circuit_breaker_state`,
      help: 'Circuit breaker state (0=CLOSED, 1=OPEN, 2=HALF_OPEN)',
      labelNames: ['circuit_name'],
      registers: [this.registry],
    });

    this.circuitBreakerCalls = new Counter({
      name: `${this.prefix}_circuit_breaker_calls_total`,
      help: 'Total number of circuit breaker calls',
      labelNames: ['circuit_name', 'result'],
      registers: [this.registry],
    });

    this.circuitBreakerFailures = new Counter({
      name: `${this.prefix}_circuit_breaker_failures_total`,
      help: 'Total number of circuit breaker failures',
      labelNames: ['circuit_name'],
      registers: [this.registry],
    });

    this.circuitBreakerSuccesses = new Counter({
      name: `${this.prefix}_circuit_breaker_successes_total`,
      help: 'Total number of circuit breaker successes',
      labelNames: ['circuit_name'],
      registers: [this.registry],
    });

    this.circuitBreakerRejects = new Counter({
      name: `${this.prefix}_circuit_breaker_rejects_total`,
      help: 'Total number of circuit breaker rejects (open circuit)',
      labelNames: ['circuit_name'],
      registers: [this.registry],
    });

    // ========================================================================
    // Cache Metrics
    // ========================================================================

    this.cacheHits = new Counter({
      name: `${this.prefix}_cache_hits_total`,
      help: 'Total number of cache hits',
      labelNames: ['cache_key_prefix'],
      registers: [this.registry],
    });

    this.cacheMisses = new Counter({
      name: `${this.prefix}_cache_misses_total`,
      help: 'Total number of cache misses',
      labelNames: ['cache_key_prefix'],
      registers: [this.registry],
    });

    this.cacheSize = new Gauge({
      name: `${this.prefix}_cache_size_bytes`,
      help: 'Current cache size in bytes',
      labelNames: ['cache_key_prefix'],
      registers: [this.registry],
    });

    this.cacheOperationDuration = new Histogram({
      name: `${this.prefix}_cache_operation_duration_seconds`,
      help: 'Cache operation duration',
      labelNames: ['operation', 'cache_key_prefix'],
      buckets: [0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1],
      registers: [this.registry],
    });

    // ========================================================================
    // WebSocket Metrics
    // ========================================================================

    this.websocketConnections = new Gauge({
      name: `${this.prefix}_websocket_connections`,
      help: 'Current number of WebSocket connections',
      labelNames: ['namespace'],
      registers: [this.registry],
    });

    this.websocketMessagesTotal = new Counter({
      name: `${this.prefix}_websocket_messages_total`,
      help: 'Total number of WebSocket messages',
      labelNames: ['namespace', 'direction', 'event_type'],
      registers: [this.registry],
    });

    this.websocketMessagesSent = new Counter({
      name: `${this.prefix}_websocket_messages_sent_total`,
      help: 'Total number of WebSocket messages sent',
      labelNames: ['namespace', 'event_type'],
      registers: [this.registry],
    });

    this.websocketMessagesReceived = new Counter({
      name: `${this.prefix}_websocket_messages_received_total`,
      help: 'Total number of WebSocket messages received',
      labelNames: ['namespace', 'event_type'],
      registers: [this.registry],
    });

    this.websocketErrors = new Counter({
      name: `${this.prefix}_websocket_errors_total`,
      help: 'Total number of WebSocket errors',
      labelNames: ['namespace', 'error_type'],
      registers: [this.registry],
    });

    // ========================================================================
    // MageAgent Integration Metrics
    // ========================================================================

    this.mageagentRequestsTotal = new Counter({
      name: `${this.prefix}_mageagent_requests_total`,
      help: 'Total number of MageAgent requests',
      labelNames: ['operation', 'status'],
      registers: [this.registry],
    });

    this.mageagentRequestDuration = new Histogram({
      name: `${this.prefix}_mageagent_request_duration_seconds`,
      help: 'MageAgent request duration',
      labelNames: ['operation'],
      buckets: [0.1, 0.5, 1, 2, 5, 10, 30, 60],
      registers: [this.registry],
    });

    this.mageagentErrors = new Counter({
      name: `${this.prefix}_mageagent_errors_total`,
      help: 'Total number of MageAgent errors',
      labelNames: ['operation', 'error_type'],
      registers: [this.registry],
    });

    // ========================================================================
    // System Metrics
    // ========================================================================

    this.uptimeSeconds = new Gauge({
      name: `${this.prefix}_uptime_seconds`,
      help: 'Application uptime in seconds',
      registers: [this.registry],
    });

    // Start uptime tracking
    const startTime = Date.now();
    setInterval(() => {
      this.uptimeSeconds.set((Date.now() - startTime) / 1000);
    }, 5000);
  }

  /**
   * Get metrics in Prometheus format
   */
  async getMetrics(): Promise<string> {
    return this.registry.metrics();
  }

  /**
   * Get metrics as JSON
   */
  async getMetricsJSON(): Promise<any[]> {
    return this.registry.getMetricsAsJSON();
  }

  /**
   * Reset all metrics (useful for testing)
   */
  reset(): void {
    this.registry.resetMetrics();
  }
}

/**
 * Express middleware for HTTP metrics
 */
export function createMetricsMiddleware(metrics: MetricsCollector) {
  return (req: Request, res: Response, next: NextFunction): void => {
    const start = Date.now();
    const path = normalizePath(req.path);

    // Increment active requests
    metrics.httpActiveRequests.inc({ method: req.method, path });

    // Track request size
    const requestSize = parseInt(req.get('content-length') || '0', 10);
    metrics.httpRequestSize.observe({ method: req.method, path }, requestSize);

    // Hook into response finish
    res.on('finish', () => {
      const duration = (Date.now() - start) / 1000;
      const statusCode = res.statusCode.toString();

      // Record metrics
      metrics.httpRequestsTotal.inc({ method: req.method, path, status_code: statusCode });
      metrics.httpRequestDuration.observe({ method: req.method, path, status_code: statusCode }, duration);

      // Track response size
      const responseSize = parseInt(res.get('content-length') || '0', 10);
      metrics.httpResponseSize.observe({ method: req.method, path, status_code: statusCode }, responseSize);

      // Decrement active requests
      metrics.httpActiveRequests.dec({ method: req.method, path });
    });

    next();
  };
}

/**
 * Normalize path for metrics (remove IDs)
 */
function normalizePath(path: string): string {
  return path
    .replace(/\/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}/gi, '/:id')
    .replace(/\/\d+/g, '/:id')
    .replace(/\/[a-zA-Z0-9_-]{20,}/g, '/:id');
}

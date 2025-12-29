/**
 * Enhanced Health Check System
 *
 * Enterprise-grade health checking following GraphRAG pattern with:
 * - Health check caching (5s TTL for Kubernetes probes)
 * - Exponential backoff retry (0s, 2s, 5s)
 * - Periodic health monitoring (60s intervals)
 * - Multi-service health tracking
 * - Automatic service recovery detection
 *
 * Pattern from: services/nexus-graphrag/src/api.ts
 */

import axios, { AxiosError } from 'axios';
import { createLogger, format, transports } from 'winston';

// ============================================================================
// Types and Interfaces
// ============================================================================

export interface ServiceHealth {
  healthy: boolean;
  latency?: number;
  lastCheck: Date;
  error?: string;
}

export interface HealthCheckResult {
  status: 'healthy' | 'degraded' | 'unhealthy';
  services: {
    mageAgent: ServiceHealth;
    redis: ServiceHealth;
    postgres?: ServiceHealth;
    qdrant?: ServiceHealth;
    graphrag?: ServiceHealth;
  };
  timestamp: Date;
  uptime: number;
}

interface CachedHealth {
  data: HealthCheckResult | null;
  timestamp: number;
  ttl: number;
}

// ============================================================================
// Health Checker Class
// ============================================================================

export class HealthChecker {
  private logger: ReturnType<typeof createLogger>;
  private startTime: Date;

  // Health check cache (5s TTL for K8s probes)
  private healthCheckCache: CachedHealth = {
    data: null,
    timestamp: 0,
    ttl: 5000, // 5 seconds
  };

  // Service health status tracking
  private mageAgentHealthy: boolean = false;
  private redisHealthy: boolean = false;
  private postgresHealthy: boolean = false;
  private qdrantHealthy: boolean = false;
  private graphragHealthy: boolean = false;

  // URLs and clients
  private mageAgentUrl: string;
  private redisClient: any;
  private postgresClient?: any;
  private qdrantUrl?: string;
  private graphragUrl?: string;

  // Periodic monitoring interval
  private monitoringInterval?: NodeJS.Timeout;

  constructor(
    mageAgentUrl: string,
    redisClient: any,
    options?: {
      postgresClient?: any;
      qdrantUrl?: string;
      graphragUrl?: string;
    }
  ) {
    this.startTime = new Date();
    this.mageAgentUrl = mageAgentUrl;
    this.redisClient = redisClient;
    this.postgresClient = options?.postgresClient;
    this.qdrantUrl = options?.qdrantUrl;
    this.graphragUrl = options?.graphragUrl;

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
              return `${timestamp} [${level}] [Health Checker]: ${message} ${
                Object.keys(meta).length ? JSON.stringify(meta) : ''
              }`;
            })
          ),
        }),
      ],
    });

    this.logger.info('Health Checker initialized', {
      mageAgentUrl,
      monitoringInterval: '60s',
      cacheTTL: '5s',
    });
  }

  // ==========================================================================
  // Cached Health Check (Main Entry Point)
  // ==========================================================================

  /**
   * Get health check result with caching
   * Uses 5-second TTL cache to prevent overwhelming services with K8s probes
   */
  public async getHealthStatus(): Promise<HealthCheckResult> {
    const now = Date.now();

    // Return cached result if still valid
    if (
      this.healthCheckCache.data &&
      now - this.healthCheckCache.timestamp < this.healthCheckCache.ttl
    ) {
      this.logger.debug('Returning cached health check result', {
        age: now - this.healthCheckCache.timestamp,
      });
      return this.healthCheckCache.data;
    }

    // Perform fresh health checks
    const result = await this.performHealthChecks();

    // Update cache
    this.healthCheckCache.data = result;
    this.healthCheckCache.timestamp = now;

    return result;
  }

  // ==========================================================================
  // Health Check Execution
  // ==========================================================================

  private async performHealthChecks(): Promise<HealthCheckResult> {
    const startTime = Date.now();

    // Check all services in parallel
    const [mageAgent, redis, postgres, qdrant, graphrag] = await Promise.allSettled([
      this.checkMageAgentWithRetry(),
      this.checkRedis(),
      this.postgresClient ? this.checkPostgres() : Promise.resolve({ healthy: true, lastCheck: new Date() }),
      this.qdrantUrl ? this.checkQdrant() : Promise.resolve({ healthy: true, lastCheck: new Date() }),
      this.graphragUrl ? this.checkGraphRAG() : Promise.resolve({ healthy: true, lastCheck: new Date() }),
    ]);

    const mageAgentHealth = mageAgent.status === 'fulfilled' ? mageAgent.value : {
      healthy: false,
      error: 'Health check failed',
      lastCheck: new Date(),
    };

    const redisHealth = redis.status === 'fulfilled' ? redis.value : {
      healthy: false,
      error: 'Health check failed',
      lastCheck: new Date(),
    };

    const postgresHealth = postgres.status === 'fulfilled' ? postgres.value : {
      healthy: false,
      error: 'Health check failed',
      lastCheck: new Date(),
    };

    const qdrantHealth = qdrant.status === 'fulfilled' ? qdrant.value : {
      healthy: false,
      error: 'Health check failed',
      lastCheck: new Date(),
    };

    const graphragHealth = graphrag.status === 'fulfilled' ? graphrag.value : {
      healthy: false,
      error: 'Health check failed',
      lastCheck: new Date(),
    };

    // Update internal state
    this.mageAgentHealthy = mageAgentHealth.healthy;
    this.redisHealthy = redisHealth.healthy;
    if (this.postgresClient) this.postgresHealthy = postgresHealth.healthy;
    if (this.qdrantUrl) this.qdrantHealthy = qdrantHealth.healthy;
    if (this.graphragUrl) this.graphragHealthy = graphragHealth.healthy;

    // Determine overall status
    const criticalServicesHealthy = mageAgentHealth.healthy && redisHealth.healthy;
    const optionalServicesHealthy =
      (!this.postgresClient || postgresHealth.healthy) &&
      (!this.qdrantUrl || qdrantHealth.healthy) &&
      (!this.graphragUrl || graphragHealth.healthy);

    let status: 'healthy' | 'degraded' | 'unhealthy';
    if (criticalServicesHealthy && optionalServicesHealthy) {
      status = 'healthy';
    } else if (criticalServicesHealthy) {
      status = 'degraded';
    } else {
      status = 'unhealthy';
    }

    const result: HealthCheckResult = {
      status,
      services: {
        mageAgent: mageAgentHealth,
        redis: redisHealth,
        ...(this.postgresClient && { postgres: postgresHealth }),
        ...(this.qdrantUrl && { qdrant: qdrantHealth }),
        ...(this.graphragUrl && { graphrag: graphragHealth }),
      },
      timestamp: new Date(),
      uptime: Date.now() - this.startTime.getTime(),
    };

    const duration = Date.now() - startTime;
    this.logger.debug('Health check completed', {
      status,
      duration: `${duration}ms`,
      mageAgent: mageAgentHealth.healthy,
      redis: redisHealth.healthy,
    });

    return result;
  }

  // ==========================================================================
  // Service-Specific Health Checks with Retry
  // ==========================================================================

  /**
   * Check MageAgent health with exponential backoff retry
   * Pattern: 0s, 2s, 5s delays (from GraphRAG)
   */
  private async checkMageAgentWithRetry(): Promise<ServiceHealth> {
    const maxAttempts = 3;
    const delays = [0, 2000, 5000]; // 0s, 2s, 5s

    for (let attempt = 0; attempt < maxAttempts; attempt++) {
      try {
        // Delay before retry (except first attempt)
        if (delays[attempt] > 0) {
          await new Promise(resolve => setTimeout(resolve, delays[attempt]));
        }

        const startTime = Date.now();
        const response = await axios.get(`${this.mageAgentUrl}/health`, {
          timeout: 5000,
        });

        const latency = Date.now() - startTime;

        if (response.status === 200) {
          // Service recovered
          if (!this.mageAgentHealthy) {
            this.logger.info('MageAgent service recovered', { attempt: attempt + 1, latency });
          }

          return {
            healthy: true,
            latency,
            lastCheck: new Date(),
          };
        }
      } catch (error) {
        const isLastAttempt = attempt === maxAttempts - 1;
        const errorMessage = error instanceof AxiosError
          ? error.message
          : 'Unknown error';

        if (isLastAttempt) {
          this.logger.error('MageAgent health check failed after all retries', {
            attempts: maxAttempts,
            error: errorMessage,
          });

          return {
            healthy: false,
            error: errorMessage,
            lastCheck: new Date(),
          };
        } else {
          this.logger.warn('MageAgent health check attempt failed, retrying...', {
            attempt: attempt + 1,
            nextDelay: delays[attempt + 1],
            error: errorMessage,
          });
        }
      }
    }

    // Should never reach here, but TypeScript doesn't know that
    return {
      healthy: false,
      error: 'Max retry attempts exceeded',
      lastCheck: new Date(),
    };
  }

  /**
   * Check Redis health
   */
  private async checkRedis(): Promise<ServiceHealth> {
    try {
      const startTime = Date.now();
      const result = await this.redisClient.ping();
      const latency = Date.now() - startTime;

      if (result === 'PONG') {
        return {
          healthy: true,
          latency,
          lastCheck: new Date(),
        };
      } else {
        return {
          healthy: false,
          error: 'Unexpected PING response',
          lastCheck: new Date(),
        };
      }
    } catch (error) {
      return {
        healthy: false,
        error: error instanceof Error ? error.message : 'Unknown error',
        lastCheck: new Date(),
      };
    }
  }

  /**
   * Check PostgreSQL health
   */
  private async checkPostgres(): Promise<ServiceHealth> {
    try {
      const startTime = Date.now();
      await this.postgresClient.query('SELECT 1');
      const latency = Date.now() - startTime;

      return {
        healthy: true,
        latency,
        lastCheck: new Date(),
      };
    } catch (error) {
      return {
        healthy: false,
        error: error instanceof Error ? error.message : 'Unknown error',
        lastCheck: new Date(),
      };
    }
  }

  /**
   * Check Qdrant health
   */
  private async checkQdrant(): Promise<ServiceHealth> {
    try {
      const startTime = Date.now();
      const response = await axios.get(`${this.qdrantUrl}/collections`, {
        timeout: 5000,
      });

      const latency = Date.now() - startTime;

      if (response.status === 200) {
        return {
          healthy: true,
          latency,
          lastCheck: new Date(),
        };
      } else {
        return {
          healthy: false,
          error: `Unexpected status: ${response.status}`,
          lastCheck: new Date(),
        };
      }
    } catch (error) {
      return {
        healthy: false,
        error: error instanceof AxiosError ? error.message : 'Unknown error',
        lastCheck: new Date(),
      };
    }
  }

  /**
   * Check GraphRAG health
   */
  private async checkGraphRAG(): Promise<ServiceHealth> {
    try {
      const startTime = Date.now();
      const response = await axios.get(`${this.graphragUrl}/health`, {
        timeout: 5000,
      });

      const latency = Date.now() - startTime;

      if (response.status === 200) {
        return {
          healthy: true,
          latency,
          lastCheck: new Date(),
        };
      } else {
        return {
          healthy: false,
          error: `Unexpected status: ${response.status}`,
          lastCheck: new Date(),
        };
      }
    } catch (error) {
      return {
        healthy: false,
        error: error instanceof AxiosError ? error.message : 'Unknown error',
        lastCheck: new Date(),
      };
    }
  }

  // ==========================================================================
  // Periodic Monitoring
  // ==========================================================================

  /**
   * Start periodic health monitoring (60s intervals)
   * Automatically attempts to reconnect to unhealthy services
   */
  public startPeriodicMonitoring(): void {
    if (this.monitoringInterval) {
      this.logger.warn('Periodic monitoring already started');
      return;
    }

    this.logger.info('Starting periodic health monitoring', {
      interval: '60s',
    });

    this.monitoringInterval = setInterval(async () => {
      try {
        // Check unhealthy services and attempt recovery
        if (!this.mageAgentHealthy) {
          this.logger.info('Attempting to recover MageAgent connection...');
          await this.checkMageAgentWithRetry();
        }

        if (!this.redisHealthy) {
          this.logger.info('Attempting to recover Redis connection...');
          await this.checkRedis();
        }

        if (this.postgresClient && !this.postgresHealthy) {
          this.logger.info('Attempting to recover PostgreSQL connection...');
          await this.checkPostgres();
        }

        if (this.qdrantUrl && !this.qdrantHealthy) {
          this.logger.info('Attempting to recover Qdrant connection...');
          await this.checkQdrant();
        }

        if (this.graphragUrl && !this.graphragHealthy) {
          this.logger.info('Attempting to recover GraphRAG connection...');
          await this.checkGraphRAG();
        }
      } catch (error) {
        this.logger.error('Periodic health monitoring error', {
          error: error instanceof Error ? error.message : 'Unknown error',
        });
      }
    }, 60000); // 60 seconds
  }

  /**
   * Stop periodic monitoring
   */
  public stopPeriodicMonitoring(): void {
    if (this.monitoringInterval) {
      clearInterval(this.monitoringInterval);
      this.monitoringInterval = undefined;
      this.logger.info('Stopped periodic health monitoring');
    }
  }

  // ==========================================================================
  // Utility Methods
  // ==========================================================================

  /**
   * Get individual service health status
   */
  public getServiceStatus(service: 'mageAgent' | 'redis' | 'postgres' | 'qdrant' | 'graphrag'): boolean {
    switch (service) {
      case 'mageAgent':
        return this.mageAgentHealthy;
      case 'redis':
        return this.redisHealthy;
      case 'postgres':
        return this.postgresHealthy;
      case 'qdrant':
        return this.qdrantHealthy;
      case 'graphrag':
        return this.graphragHealthy;
      default:
        return false;
    }
  }

  /**
   * Clear health check cache
   */
  public clearCache(): void {
    this.healthCheckCache.data = null;
    this.healthCheckCache.timestamp = 0;
    this.logger.debug('Health check cache cleared');
  }

  /**
   * Cleanup resources
   */
  public cleanup(): void {
    this.stopPeriodicMonitoring();
    this.logger.info('Health Checker cleaned up');
  }
}

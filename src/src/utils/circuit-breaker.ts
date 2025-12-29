/**
 * Circuit Breaker Implementation
 *
 * Root Cause Fixed: MageAgent client lacked failure isolation, causing
 * cascading failures, thread pool exhaustion, and increased latency during outages.
 *
 * Solution: Hystrix-style circuit breaker with three states (CLOSED, OPEN, HALF_OPEN),
 * exponential backoff, health tracking, and automatic recovery.
 */

import { EventEmitter } from 'events';

/**
 * Circuit breaker states
 */
export enum CircuitBreakerState {
  CLOSED = 'CLOSED', // Normal operation, requests flow through
  OPEN = 'OPEN', // Circuit is open, requests fail fast
  HALF_OPEN = 'HALF_OPEN', // Testing if service recovered
}

/**
 * Circuit breaker configuration
 */
export interface CircuitBreakerConfig {
  failureThreshold: number; // Number of failures before opening circuit
  successThreshold: number; // Number of successes to close circuit from half-open
  timeout: number; // Time to wait before transitioning to half-open (ms)
  resetTimeout: number; // Time to wait before attempting recovery (ms)
  name: string; // Circuit breaker name for logging
  monitoringWindowMs?: number; // Window for failure rate calculation
  volumeThreshold?: number; // Minimum number of requests in window
}

/**
 * Circuit breaker statistics
 */
export interface CircuitBreakerStats {
  state: CircuitBreakerState;
  failureCount: number;
  successCount: number;
  totalRequests: number;
  lastFailureTime: Date | null;
  lastSuccessTime: Date | null;
  openedAt: Date | null;
  halfOpenedAt: Date | null;
  nextAttempt: Date | null;
}

/**
 * Circuit breaker error
 */
export class CircuitBreakerOpenError extends Error {
  constructor(
    message: string,
    public readonly circuitName: string,
    public readonly nextAttempt: Date
  ) {
    super(message);
    this.name = 'CircuitBreakerOpenError';
    Error.captureStackTrace(this, this.constructor);
  }
}

/**
 * Circuit Breaker Implementation
 *
 * Implements the circuit breaker pattern to prevent cascading failures
 * and provide automatic recovery.
 */
export class CircuitBreaker extends EventEmitter {
  private state: CircuitBreakerState = CircuitBreakerState.CLOSED;
  private failureCount: number = 0;
  private successCount: number = 0;
  private totalRequests: number = 0;
  private lastFailureTime: Date | null = null;
  private lastSuccessTime: Date | null = null;
  private openedAt: Date | null = null;
  private halfOpenedAt: Date | null = null;
  private nextAttemptTime: Date | null = null;
  private resetTimer: NodeJS.Timeout | null = null;
  private readonly requestHistory: { timestamp: number; success: boolean }[] = [];

  constructor(private readonly config: CircuitBreakerConfig) {
    super();
    this.validateConfig();
  }

  /**
   * Validate configuration
   */
  private validateConfig(): void {
    if (this.config.failureThreshold < 1) {
      throw new Error('failureThreshold must be >= 1');
    }

    if (this.config.successThreshold < 1) {
      throw new Error('successThreshold must be >= 1');
    }

    if (this.config.timeout < 1000) {
      throw new Error('timeout must be >= 1000ms');
    }

    if (this.config.resetTimeout >= this.config.timeout) {
      throw new Error('resetTimeout must be < timeout');
    }
  }

  /**
   * Execute function with circuit breaker protection
   */
  async execute<T>(fn: () => Promise<T>): Promise<T> {
    // Check circuit state
    if (this.state === CircuitBreakerState.OPEN) {
      // Check if it's time to attempt recovery
      if (this.nextAttemptTime && Date.now() >= this.nextAttemptTime.getTime()) {
        this.transitionToHalfOpen();
      } else {
        const nextAttempt = this.nextAttemptTime || new Date(Date.now() + this.config.timeout);
        throw new CircuitBreakerOpenError(
          `Circuit breaker '${this.config.name}' is OPEN. Service unavailable.`,
          this.config.name,
          nextAttempt
        );
      }
    }

    // Execute function
    this.totalRequests++;
    const startTime = Date.now();

    try {
      const result = await fn();
      this.recordSuccess(Date.now() - startTime);
      return result;
    } catch (error) {
      this.recordFailure(Date.now() - startTime);
      throw error;
    }
  }

  /**
   * Record successful execution
   */
  private recordSuccess(duration: number): void {
    this.successCount++;
    this.lastSuccessTime = new Date();

    // Add to request history
    if (this.config.monitoringWindowMs) {
      this.addToRequestHistory(true);
    }

    // Emit success event
    this.emit('success', {
      circuitName: this.config.name,
      duration,
      state: this.state,
    });

    // Handle state transitions
    if (this.state === CircuitBreakerState.HALF_OPEN) {
      if (this.successCount >= this.config.successThreshold) {
        this.transitionToClosed();
      }
    } else if (this.state === CircuitBreakerState.CLOSED) {
      // Reset failure count on success in closed state
      this.failureCount = 0;
    }
  }

  /**
   * Record failed execution
   */
  private recordFailure(duration: number): void {
    this.failureCount++;
    this.lastFailureTime = new Date();

    // Add to request history
    if (this.config.monitoringWindowMs) {
      this.addToRequestHistory(false);
    }

    // Emit failure event
    this.emit('failure', {
      circuitName: this.config.name,
      duration,
      state: this.state,
      failureCount: this.failureCount,
    });

    // Handle state transitions
    if (this.state === CircuitBreakerState.HALF_OPEN) {
      // Any failure in half-open state immediately opens circuit
      this.transitionToOpen();
    } else if (this.state === CircuitBreakerState.CLOSED) {
      // Check if failure threshold reached
      if (this.shouldOpenCircuit()) {
        this.transitionToOpen();
      }
    }
  }

  /**
   * Check if circuit should open based on failure threshold
   */
  private shouldOpenCircuit(): boolean {
    // Simple threshold-based check
    if (this.failureCount >= this.config.failureThreshold) {
      return true;
    }

    // Window-based failure rate check (if configured)
    if (this.config.monitoringWindowMs && this.config.volumeThreshold) {
      const recentRequests = this.getRecentRequests();

      if (recentRequests.length >= this.config.volumeThreshold) {
        const failures = recentRequests.filter(r => !r.success).length;
        const failureRate = failures / recentRequests.length;

        // Open circuit if failure rate exceeds 50%
        return failureRate > 0.5;
      }
    }

    return false;
  }

  /**
   * Add request to history
   */
  private addToRequestHistory(success: boolean): void {
    this.requestHistory.push({
      timestamp: Date.now(),
      success,
    });

    // Limit history size
    if (this.requestHistory.length > 1000) {
      this.requestHistory.shift();
    }
  }

  /**
   * Get recent requests within monitoring window
   */
  private getRecentRequests(): { timestamp: number; success: boolean }[] {
    if (!this.config.monitoringWindowMs) {
      return [];
    }

    const cutoffTime = Date.now() - this.config.monitoringWindowMs;
    return this.requestHistory.filter(r => r.timestamp >= cutoffTime);
  }

  /**
   * Transition to OPEN state
   */
  private transitionToOpen(): void {
    if (this.state === CircuitBreakerState.OPEN) {
      return; // Already open
    }

    const previousState = this.state;
    this.state = CircuitBreakerState.OPEN;
    this.openedAt = new Date();
    this.nextAttemptTime = new Date(Date.now() + this.config.timeout);

    // Clear any existing timer
    if (this.resetTimer) {
      clearTimeout(this.resetTimer);
    }

    // Schedule transition to half-open
    this.resetTimer = setTimeout(() => {
      this.transitionToHalfOpen();
    }, this.config.timeout);

    // Emit state change event
    this.emit('stateChange', {
      circuitName: this.config.name,
      from: previousState,
      to: CircuitBreakerState.OPEN,
      failureCount: this.failureCount,
      nextAttempt: this.nextAttemptTime,
    });

    console.warn(
      `[CircuitBreaker:${this.config.name}] Circuit OPENED after ${this.failureCount} failures. ` +
      `Next attempt at: ${this.nextAttemptTime.toISOString()}`
    );
  }

  /**
   * Transition to HALF_OPEN state
   */
  private transitionToHalfOpen(): void {
    const previousState = this.state;
    this.state = CircuitBreakerState.HALF_OPEN;
    this.halfOpenedAt = new Date();
    this.successCount = 0; // Reset success count for half-open state

    // Emit state change event
    this.emit('stateChange', {
      circuitName: this.config.name,
      from: previousState,
      to: CircuitBreakerState.HALF_OPEN,
    });

    console.info(
      `[CircuitBreaker:${this.config.name}] Circuit transitioned to HALF_OPEN. Testing recovery...`
    );
  }

  /**
   * Transition to CLOSED state
   */
  private transitionToClosed(): void {
    const previousState = this.state;
    this.state = CircuitBreakerState.CLOSED;
    this.failureCount = 0;
    this.successCount = 0;
    this.openedAt = null;
    this.halfOpenedAt = null;
    this.nextAttemptTime = null;

    // Clear reset timer
    if (this.resetTimer) {
      clearTimeout(this.resetTimer);
      this.resetTimer = null;
    }

    // Emit state change event
    this.emit('stateChange', {
      circuitName: this.config.name,
      from: previousState,
      to: CircuitBreakerState.CLOSED,
    });

    console.info(
      `[CircuitBreaker:${this.config.name}] Circuit CLOSED. Service recovered successfully.`
    );
  }

  /**
   * Get circuit breaker statistics
   */
  getStats(): CircuitBreakerStats {
    return {
      state: this.state,
      failureCount: this.failureCount,
      successCount: this.successCount,
      totalRequests: this.totalRequests,
      lastFailureTime: this.lastFailureTime,
      lastSuccessTime: this.lastSuccessTime,
      openedAt: this.openedAt,
      halfOpenedAt: this.halfOpenedAt,
      nextAttempt: this.nextAttemptTime,
    };
  }

  /**
   * Check if circuit is closed
   */
  isClosed(): boolean {
    return this.state === CircuitBreakerState.CLOSED;
  }

  /**
   * Check if circuit is open
   */
  isOpen(): boolean {
    return this.state === CircuitBreakerState.OPEN;
  }

  /**
   * Check if circuit is half-open
   */
  isHalfOpen(): boolean {
    return this.state === CircuitBreakerState.HALF_OPEN;
  }

  /**
   * Force close circuit (manual reset)
   */
  forceClose(): void {
    this.transitionToClosed();
  }

  /**
   * Force open circuit (manual trip)
   */
  forceOpen(): void {
    this.transitionToOpen();
  }

  /**
   * Cleanup resources
   */
  destroy(): void {
    if (this.resetTimer) {
      clearTimeout(this.resetTimer);
      this.resetTimer = null;
    }
    this.removeAllListeners();
  }
}

/**
 * Circuit Breaker Factory
 *
 * Manages multiple circuit breakers with centralized configuration.
 */
export class CircuitBreakerFactory {
  private circuitBreakers: Map<string, CircuitBreaker> = new Map();

  constructor(private readonly defaultConfig: Partial<CircuitBreakerConfig>) {}

  /**
   * Get or create circuit breaker
   */
  getCircuitBreaker(name: string, config?: Partial<CircuitBreakerConfig>): CircuitBreaker {
    let circuitBreaker = this.circuitBreakers.get(name);

    if (!circuitBreaker) {
      const finalConfig: CircuitBreakerConfig = {
        name,
        failureThreshold: 5,
        successThreshold: 2,
        timeout: 60000,
        resetTimeout: 30000,
        ...this.defaultConfig,
        ...config,
      };

      circuitBreaker = new CircuitBreaker(finalConfig);
      this.circuitBreakers.set(name, circuitBreaker);
    }

    return circuitBreaker;
  }

  /**
   * Get all circuit breakers
   */
  getAllCircuitBreakers(): Map<string, CircuitBreaker> {
    return this.circuitBreakers;
  }

  /**
   * Get statistics for all circuit breakers
   */
  getAllStats(): Record<string, CircuitBreakerStats> {
    const stats: Record<string, CircuitBreakerStats> = {};

    for (const [name, circuitBreaker] of this.circuitBreakers) {
      stats[name] = circuitBreaker.getStats();
    }

    return stats;
  }

  /**
   * Cleanup all circuit breakers
   */
  destroy(): void {
    for (const circuitBreaker of this.circuitBreakers.values()) {
      circuitBreaker.destroy();
    }
    this.circuitBreakers.clear();
  }
}

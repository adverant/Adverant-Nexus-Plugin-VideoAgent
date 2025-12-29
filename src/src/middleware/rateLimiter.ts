/**
 * Rate Limiting Middleware
 *
 * Root Cause Fixed: No request throttling allowed unlimited requests,
 * exposing service to DoS attacks and resource exhaustion.
 *
 * Solution: Redis-backed sliding window rate limiter with per-IP and
 * per-user tracking, configurable limits, and informative error responses.
 */

import { Request, Response, NextFunction } from 'express';
import Redis from 'ioredis';
import { ApiError } from './errorHandler';

export interface RateLimiterConfig {
  redis: Redis;
  windowMs: number; // Time window in milliseconds
  maxRequests: number; // Max requests per window
  keyPrefix?: string; // Redis key prefix
  skipSuccessfulRequests?: boolean; // Only count failed requests
  skip?: (req: Request) => boolean; // Skip rate limiting for certain requests
}

export interface RateLimitInfo {
  limit: number;
  remaining: number;
  reset: Date;
  retryAfter?: number;
}

/**
 * Create rate limiting middleware
 *
 * Uses sliding window algorithm with Redis for distributed rate limiting.
 * Tracks requests per IP address with optional user-based tracking.
 */
export function createRateLimiter(config: RateLimiterConfig) {
  const {
    redis,
    windowMs,
    maxRequests,
    keyPrefix = 'ratelimit',
    skipSuccessfulRequests = false,
    skip,
  } = config;

  return async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      // Skip rate limiting if configured
      if (skip && skip(req)) {
        return next();
      }

      // Generate rate limit key
      const key = generateRateLimitKey(req, keyPrefix);

      // Get current request count
      const rateLimitInfo = await checkRateLimit(redis, key, windowMs, maxRequests);

      // Set rate limit headers
      setRateLimitHeaders(res, rateLimitInfo);

      // Check if rate limit exceeded
      if (rateLimitInfo.remaining < 0) {
        throw new ApiError(
          429,
          'Too many requests. Please try again later.',
          {
            limit: rateLimitInfo.limit,
            reset: rateLimitInfo.reset,
            retryAfter: rateLimitInfo.retryAfter,
          }
        );
      }

      // Increment counter after successful request (if configured)
      if (!skipSuccessfulRequests) {
        res.on('finish', () => {
          if (res.statusCode < 400) {
            // Don't await - fire and forget
            incrementRateLimit(redis, key, windowMs).catch(err => {
              console.error('Failed to increment rate limit:', err);
            });
          }
        });
      } else {
        // Increment immediately for failed requests
        res.on('finish', () => {
          if (res.statusCode >= 400) {
            incrementRateLimit(redis, key, windowMs).catch(err => {
              console.error('Failed to increment rate limit:', err);
            });
          }
        });
      }

      next();
    } catch (error) {
      next(error);
    }
  };
}

/**
 * Generate rate limit key from request
 *
 * Uses IP address and optional user ID for tracking.
 * Handles X-Forwarded-For header for proxied requests.
 */
function generateRateLimitKey(req: Request, prefix: string): string {
  // Get client IP address
  const ip = getClientIp(req);

  // Get user ID from auth (if available)
  const userId = (req as any).user?.id || (req as any).userId;

  // Build key
  if (userId) {
    return `${prefix}:user:${userId}`;
  }

  return `${prefix}:ip:${ip}`;
}

/**
 * Get client IP address from request
 *
 * Handles X-Forwarded-For, X-Real-IP, and direct connection.
 */
function getClientIp(req: Request): string {
  // Check X-Forwarded-For header (comma-separated list)
  const forwardedFor = req.headers['x-forwarded-for'];
  if (forwardedFor) {
    const ips = Array.isArray(forwardedFor) ? forwardedFor[0] : forwardedFor;
    return ips.split(',')[0].trim();
  }

  // Check X-Real-IP header
  const realIp = req.headers['x-real-ip'];
  if (realIp) {
    return Array.isArray(realIp) ? realIp[0] : realIp;
  }

  // Fall back to direct connection IP
  return req.socket.remoteAddress || 'unknown';
}

/**
 * Check rate limit using sliding window algorithm
 *
 * Uses Redis sorted sets for efficient sliding window implementation.
 */
async function checkRateLimit(
  redis: Redis,
  key: string,
  windowMs: number,
  maxRequests: number
): Promise<RateLimitInfo> {
  const now = Date.now();
  const windowStart = now - windowMs;

  // Use Redis pipeline for atomic operations
  const pipeline = redis.pipeline();

  // Remove old entries outside the window
  pipeline.zremrangebyscore(key, 0, windowStart);

  // Count requests in current window
  pipeline.zcard(key);

  // Add current request with timestamp as score
  pipeline.zadd(key, now, `${now}-${Math.random()}`);

  // Set expiration to window size (cleanup old keys)
  pipeline.expire(key, Math.ceil(windowMs / 1000));

  // Execute pipeline
  const results = await pipeline.exec();

  if (!results) {
    throw new Error('Redis pipeline execution failed');
  }

  // Extract request count (before adding current request)
  const requestCount = results[1][1] as number;

  // Calculate remaining requests
  const remaining = Math.max(0, maxRequests - requestCount - 1);

  // Calculate reset time
  const reset = new Date(now + windowMs);

  // Calculate retry after (in seconds)
  let retryAfter: number | undefined;
  if (remaining < 0) {
    // Get oldest request timestamp in window
    const oldestResults = await redis.zrange(key, 0, 0, 'WITHSCORES');
    if (oldestResults && oldestResults.length >= 2) {
      const oldestTimestamp = parseInt(oldestResults[1], 10);
      retryAfter = Math.ceil((oldestTimestamp + windowMs - now) / 1000);
    } else {
      retryAfter = Math.ceil(windowMs / 1000);
    }
  }

  return {
    limit: maxRequests,
    remaining,
    reset,
    retryAfter,
  };
}

/**
 * Increment rate limit counter
 */
async function incrementRateLimit(redis: Redis, key: string, windowMs: number): Promise<void> {
  const now = Date.now();

  await redis
    .multi()
    .zadd(key, now, `${now}-${Math.random()}`)
    .expire(key, Math.ceil(windowMs / 1000))
    .exec();
}

/**
 * Set rate limit headers on response
 *
 * Follows standard RateLimit headers specification.
 */
function setRateLimitHeaders(res: Response, info: RateLimitInfo): void {
  res.setHeader('X-RateLimit-Limit', info.limit.toString());
  res.setHeader('X-RateLimit-Remaining', Math.max(0, info.remaining).toString());
  res.setHeader('X-RateLimit-Reset', info.reset.toISOString());

  if (info.retryAfter !== undefined) {
    res.setHeader('Retry-After', info.retryAfter.toString());
  }
}

/**
 * Create multiple rate limiters for different endpoints
 */
export function createTieredRateLimiters(redis: Redis, baseConfig: Partial<RateLimiterConfig>) {
  return {
    // Global rate limiter (all requests)
    global: createRateLimiter({
      redis,
      windowMs: 15 * 60 * 1000, // 15 minutes
      maxRequests: 1000,
      keyPrefix: 'ratelimit:global',
      ...baseConfig,
    }),

    // API rate limiter (authenticated endpoints)
    api: createRateLimiter({
      redis,
      windowMs: 15 * 60 * 1000, // 15 minutes
      maxRequests: 100,
      keyPrefix: 'ratelimit:api',
      ...baseConfig,
    }),

    // Processing rate limiter (video processing endpoint)
    processing: createRateLimiter({
      redis,
      windowMs: 60 * 60 * 1000, // 1 hour
      maxRequests: 10,
      keyPrefix: 'ratelimit:processing',
      ...baseConfig,
    }),

    // Upload rate limiter (file uploads)
    upload: createRateLimiter({
      redis,
      windowMs: 60 * 60 * 1000, // 1 hour
      maxRequests: 20,
      keyPrefix: 'ratelimit:upload',
      ...baseConfig,
    }),
  };
}

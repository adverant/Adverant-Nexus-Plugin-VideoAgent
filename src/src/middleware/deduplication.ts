/**
 * Request Deduplication Middleware
 *
 * Root Cause Fixed: Duplicate job submissions from client retries or
 * double-clicks caused unnecessary processing and cost.
 *
 * Solution: SHA-256 fingerprinting with Redis storage, configurable
 * deduplication window, and informative conflict responses.
 */

import { Request, Response, NextFunction } from 'express';
import Redis from 'ioredis';
import { createHash } from 'crypto';
import { ApiError } from './errorHandler';
import { getRequestId } from './correlationId';

/**
 * Deduplication configuration
 */
export interface DeduplicationConfig {
  redis: Redis;
  windowMs?: number; // Deduplication window in milliseconds
  keyPrefix?: string; // Redis key prefix
  fingerprintFields?: string[]; // Fields to include in fingerprint
  excludePaths?: string[]; // Paths to exclude from deduplication
  onDuplicate?: 'reject' | 'return_original'; // How to handle duplicates
}

/**
 * Duplicate request information
 */
interface DuplicateInfo {
  originalRequestId: string;
  timestamp: Date;
  response?: any;
}

/**
 * Create request deduplication middleware
 *
 * Detects duplicate requests based on content fingerprinting and
 * prevents redundant processing.
 */
export function createDeduplicationMiddleware(config: DeduplicationConfig) {
  const {
    redis,
    windowMs = 5000, // 5 seconds default
    keyPrefix = 'dedup',
    fingerprintFields = ['method', 'path', 'body', 'userId'],
    excludePaths = ['/health', '/metrics'],
    onDuplicate = 'reject',
  } = config;

  return async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    try {
      // Skip deduplication for excluded paths
      if (shouldSkipDeduplication(req.path, excludePaths)) {
        return next();
      }

      // Skip deduplication for GET requests (idempotent)
      if (req.method === 'GET') {
        return next();
      }

      // Generate request fingerprint
      const fingerprint = generateFingerprint(req, fingerprintFields);
      const dedupKey = buildKey(keyPrefix, fingerprint);

      // Check if request is duplicate
      const duplicateInfo = await checkDuplicate(redis, dedupKey);

      if (duplicateInfo) {
        // Duplicate request detected
        handleDuplicate(req, res, duplicateInfo, onDuplicate);
        return;
      }

      // Store request fingerprint
      const requestId = getRequestId(req) || 'unknown';
      await storeFingerprint(redis, dedupKey, requestId, windowMs);

      // Store response for potential duplicate requests (if configured)
      if (onDuplicate === 'return_original') {
        interceptResponse(req, res, redis, dedupKey, windowMs);
      }

      next();
    } catch (error) {
      // Don't fail request on deduplication errors
      console.error('Deduplication middleware error:', error);
      next();
    }
  };
}

/**
 * Generate request fingerprint
 *
 * Creates a SHA-256 hash from specified request fields.
 */
function generateFingerprint(req: Request, fields: string[]): string {
  const fingerprintData: Record<string, any> = {};

  for (const field of fields) {
    switch (field) {
      case 'method':
        fingerprintData.method = req.method;
        break;

      case 'path':
        fingerprintData.path = req.path;
        break;

      case 'body':
        // Only include body for POST/PUT/PATCH
        if (['POST', 'PUT', 'PATCH'].includes(req.method)) {
          fingerprintData.body = sanitizeBody(req.body);
        }
        break;

      case 'userId':
        // Extract user ID from request
        fingerprintData.userId = extractUserId(req);
        break;

      case 'query':
        fingerprintData.query = req.query;
        break;

      case 'headers':
        // Only include specific headers
        fingerprintData.headers = {
          'content-type': req.get('content-type'),
          'user-agent': req.get('user-agent'),
        };
        break;

      default:
        // Custom field extraction
        if (req.body && field in req.body) {
          fingerprintData[field] = req.body[field];
        }
    }
  }

  // Sort keys for consistent hashing
  const sortedData = sortObjectKeys(fingerprintData);

  // Generate SHA-256 hash
  return createHash('sha256')
    .update(JSON.stringify(sortedData))
    .digest('hex');
}

/**
 * Sanitize request body for fingerprinting
 *
 * Removes timestamps and other fields that should not affect deduplication.
 */
function sanitizeBody(body: any): any {
  if (!body || typeof body !== 'object') {
    return body;
  }

  const sanitized = { ...body };

  // Remove timestamp fields
  const timestampFields = [
    'timestamp',
    'createdAt',
    'updatedAt',
    'enqueuedAt',
    '_timestamp',
  ];

  for (const field of timestampFields) {
    delete sanitized[field];
  }

  // Remove request IDs (should not affect deduplication)
  const idFields = ['requestId', 'correlationId', 'traceId'];
  for (const field of idFields) {
    delete sanitized[field];
  }

  return sanitized;
}

/**
 * Extract user ID from request
 */
function extractUserId(req: Request): string | undefined {
  // Try different sources for user ID
  return (
    (req as any).user?.id ||
    (req as any).userId ||
    req.body?.userId ||
    req.query?.userId as string ||
    req.get('x-user-id')
  );
}

/**
 * Sort object keys recursively for consistent hashing
 */
function sortObjectKeys(obj: any): any {
  if (obj === null || typeof obj !== 'object') {
    return obj;
  }

  if (Array.isArray(obj)) {
    return obj.map(sortObjectKeys);
  }

  const sorted: Record<string, any> = {};
  const keys = Object.keys(obj).sort();

  for (const key of keys) {
    sorted[key] = sortObjectKeys(obj[key]);
  }

  return sorted;
}

/**
 * Build Redis key for deduplication
 */
function buildKey(prefix: string, fingerprint: string): string {
  return `${prefix}:${fingerprint}`;
}

/**
 * Check if request is duplicate
 */
async function checkDuplicate(redis: Redis, key: string): Promise<DuplicateInfo | null> {
  try {
    const value = await redis.get(key);
    if (!value) {
      return null;
    }

    return JSON.parse(value) as DuplicateInfo;
  } catch (error) {
    console.error('Error checking duplicate:', error);
    return null;
  }
}

/**
 * Store request fingerprint
 */
async function storeFingerprint(
  redis: Redis,
  key: string,
  requestId: string,
  ttlMs: number
): Promise<void> {
  const info: DuplicateInfo = {
    originalRequestId: requestId,
    timestamp: new Date(),
  };

  const ttlSeconds = Math.ceil(ttlMs / 1000);
  await redis.setex(key, ttlSeconds, JSON.stringify(info));
}

/**
 * Handle duplicate request
 */
function handleDuplicate(
  req: Request,
  res: Response,
  duplicateInfo: DuplicateInfo,
  strategy: 'reject' | 'return_original'
): void {
  if (strategy === 'reject') {
    // Reject duplicate request
    throw new ApiError(
      409,
      'Duplicate request detected. Please wait before retrying.',
      {
        originalRequestId: duplicateInfo.originalRequestId,
        originalTimestamp: duplicateInfo.timestamp,
        currentRequestId: getRequestId(req),
      }
    );
  } else if (strategy === 'return_original' && duplicateInfo.response) {
    // Return original response
    res.status(200).json({
      ...duplicateInfo.response,
      _deduplication: {
        duplicate: true,
        originalRequestId: duplicateInfo.originalRequestId,
        originalTimestamp: duplicateInfo.timestamp,
      },
    });
  } else {
    // No stored response, reject
    throw new ApiError(
      409,
      'Duplicate request detected. Original request is still processing.',
      {
        originalRequestId: duplicateInfo.originalRequestId,
        originalTimestamp: duplicateInfo.timestamp,
      }
    );
  }
}

/**
 * Intercept response to store for duplicate requests
 */
function interceptResponse(
  req: Request,
  res: Response,
  redis: Redis,
  dedupKey: string,
  ttlMs: number
): void {
  const originalJson = res.json.bind(res);

  res.json = function (body: any): Response {
    // Store response for duplicate requests
    storeResponse(redis, dedupKey, body, ttlMs).catch(err => {
      console.error('Error storing response for deduplication:', err);
    });

    return originalJson(body);
  };
}

/**
 * Store response for duplicate requests
 */
async function storeResponse(
  redis: Redis,
  key: string,
  response: any,
  ttlMs: number
): Promise<void> {
  try {
    const existing = await redis.get(key);
    if (!existing) {
      return;
    }

    const info = JSON.parse(existing) as DuplicateInfo;
    info.response = response;

    const ttlSeconds = Math.ceil(ttlMs / 1000);
    await redis.setex(key, ttlSeconds, JSON.stringify(info));
  } catch (error) {
    console.error('Error storing response:', error);
  }
}

/**
 * Check if path should skip deduplication
 */
function shouldSkipDeduplication(path: string, excludePaths: string[]): boolean {
  for (const excludePath of excludePaths) {
    if (path.startsWith(excludePath)) {
      return true;
    }
  }
  return false;
}

/**
 * Create idempotency key based request deduplication
 *
 * Alternative implementation using Idempotency-Key header (Stripe-style).
 */
export function createIdempotencyKeyMiddleware(redis: Redis, ttlMs: number = 86400000) {
  return async (req: Request, res: Response, next: NextFunction): Promise<void> => {
    // Only for POST/PUT/PATCH
    if (!['POST', 'PUT', 'PATCH'].includes(req.method)) {
      return next();
    }

    // Check for Idempotency-Key header
    const idempotencyKey = req.get('idempotency-key');
    if (!idempotencyKey) {
      return next();
    }

    // Validate idempotency key format (UUID or similar)
    if (!/^[a-zA-Z0-9_-]{16,64}$/.test(idempotencyKey)) {
      throw new ApiError(400, 'Invalid Idempotency-Key format');
    }

    const key = `idempotency:${idempotencyKey}`;

    try {
      // Check if key exists
      const stored = await redis.get(key);

      if (stored) {
        // Return stored response
        const response = JSON.parse(stored);
        res.status(response.statusCode || 200).json(response.body);
        return;
      }

      // Store new request
      const originalJson = res.json.bind(res);
      res.json = function (body: any): Response {
        // Store response
        const response = {
          statusCode: res.statusCode,
          body,
        };

        redis.setex(key, Math.ceil(ttlMs / 1000), JSON.stringify(response)).catch(err => {
          console.error('Error storing idempotency key:', err);
        });

        return originalJson(body);
      };

      next();
    } catch (error) {
      console.error('Idempotency key middleware error:', error);
      next();
    }
  };
}

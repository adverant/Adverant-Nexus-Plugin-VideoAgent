/**
 * Input Sanitization Middleware
 *
 * Root Cause Fixed: User-provided strings were not validated or sanitized,
 * enabling XSS attacks, SQL injection, and path traversal vulnerabilities.
 *
 * Solution: Multi-layered sanitization with HTML escaping, SQL injection
 * prevention, path traversal protection, and URL validation.
 */

import { Request, Response, NextFunction } from 'express';
import validator from 'validator';
import { ApiError } from './errorHandler';

/**
 * Sanitization configuration
 */
export interface SanitizationConfig {
  stripTags?: boolean; // Remove HTML tags
  escapeHtml?: boolean; // Escape HTML entities
  trimWhitespace?: boolean; // Trim leading/trailing whitespace
  normalizeUrl?: boolean; // Normalize URLs
  maxStringLength?: number; // Max string length
  allowedProtocols?: string[]; // Allowed URL protocols
  allowedMimeTypes?: string[]; // Allowed MIME types
}

/**
 * Default sanitization configuration
 */
const DEFAULT_CONFIG: Required<SanitizationConfig> = {
  stripTags: true,
  escapeHtml: true,
  trimWhitespace: true,
  normalizeUrl: true,
  maxStringLength: 10000,
  allowedProtocols: ['http', 'https'],
  allowedMimeTypes: [
    'video/mp4',
    'video/mpeg',
    'video/quicktime',
    'video/x-msvideo',
    'video/webm',
    'video/x-flv',
  ],
};

/**
 * Create sanitization middleware
 */
export function createSanitizationMiddleware(config: SanitizationConfig = {}) {
  const finalConfig = { ...DEFAULT_CONFIG, ...config };

  return (req: Request, res: Response, next: NextFunction): void => {
    try {
      // Sanitize request body
      if (req.body && typeof req.body === 'object') {
        req.body = sanitizeObject(req.body, finalConfig);
      }

      // Sanitize query parameters
      if (req.query && typeof req.query === 'object') {
        req.query = sanitizeObject(req.query as any, finalConfig) as any;
      }

      // Sanitize route parameters
      if (req.params && typeof req.params === 'object') {
        req.params = sanitizeObject(req.params, finalConfig);
      }

      next();
    } catch (error) {
      next(new ApiError(400, 'Input validation failed', {
        error: error instanceof Error ? error.message : String(error),
      }));
    }
  };
}

/**
 * Sanitize an object recursively
 */
function sanitizeObject<T extends Record<string, any>>(
  obj: T,
  config: Required<SanitizationConfig>
): T {
  const sanitized: any = {};

  for (const [key, value] of Object.entries(obj)) {
    // Sanitize key (prevent prototype pollution)
    const sanitizedKey = sanitizeString(key, config);

    // Skip dangerous keys
    if (isDangerousKey(sanitizedKey)) {
      continue;
    }

    // Sanitize value based on type
    if (value === null || value === undefined) {
      sanitized[sanitizedKey] = value;
    } else if (typeof value === 'string') {
      sanitized[sanitizedKey] = sanitizeString(value, config);
    } else if (typeof value === 'number' || typeof value === 'boolean') {
      sanitized[sanitizedKey] = value;
    } else if (Array.isArray(value)) {
      sanitized[sanitizedKey] = value.map(item =>
        typeof item === 'object' ? sanitizeObject(item, config) : sanitizeValue(item, config)
      );
    } else if (typeof value === 'object') {
      sanitized[sanitizedKey] = sanitizeObject(value, config);
    } else {
      sanitized[sanitizedKey] = value;
    }
  }

  return sanitized as T;
}

/**
 * Sanitize a single value
 */
function sanitizeValue(value: any, config: Required<SanitizationConfig>): any {
  if (typeof value === 'string') {
    return sanitizeString(value, config);
  }
  return value;
}

/**
 * Sanitize a string
 */
function sanitizeString(str: string, config: Required<SanitizationConfig>): string {
  let sanitized = str;

  // Trim whitespace
  if (config.trimWhitespace) {
    sanitized = sanitized.trim();
  }

  // Check max length
  if (config.maxStringLength && sanitized.length > config.maxStringLength) {
    throw new Error(`String exceeds maximum length of ${config.maxStringLength} characters`);
  }

  // Strip HTML tags
  if (config.stripTags) {
    sanitized = validator.stripLow(sanitized);
    sanitized = sanitized.replace(/<[^>]*>/g, '');
  }

  // Escape HTML entities
  if (config.escapeHtml) {
    sanitized = validator.escape(sanitized);
  }

  // Normalize whitespace
  sanitized = sanitized.replace(/\s+/g, ' ');

  return sanitized;
}

/**
 * Check if key is dangerous (prototype pollution prevention)
 */
function isDangerousKey(key: string): boolean {
  const dangerous = ['__proto__', 'constructor', 'prototype'];
  return dangerous.includes(key.toLowerCase());
}

/**
 * Validate and sanitize URL
 */
export function sanitizeUrl(url: string, allowedProtocols: string[] = ['http', 'https']): string {
  // Basic validation
  if (!validator.isURL(url, {
    protocols: allowedProtocols,
    require_protocol: true,
    require_valid_protocol: true,
  })) {
    throw new Error('Invalid URL format');
  }

  // Parse URL
  const parsedUrl = new URL(url);

  // Check protocol
  const protocol = parsedUrl.protocol.replace(':', '');
  if (!allowedProtocols.includes(protocol)) {
    throw new Error(`Protocol '${protocol}' not allowed. Allowed protocols: ${allowedProtocols.join(', ')}`);
  }

  // Prevent SSRF - block private IP ranges
  if (isPrivateIP(parsedUrl.hostname)) {
    throw new Error('Cannot access private IP addresses or localhost');
  }

  // Prevent file:// protocol
  if (protocol === 'file') {
    throw new Error('File protocol not allowed');
  }

  // Normalize URL
  return parsedUrl.toString();
}

/**
 * Check if hostname is a private IP address
 */
function isPrivateIP(hostname: string): boolean {
  // Localhost
  if (hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '::1') {
    return true;
  }

  // Private IPv4 ranges
  const privateIPv4Ranges = [
    /^10\./,
    /^172\.(1[6-9]|2[0-9]|3[0-1])\./,
    /^192\.168\./,
    /^169\.254\./, // Link-local
  ];

  for (const range of privateIPv4Ranges) {
    if (range.test(hostname)) {
      return true;
    }
  }

  // Private IPv6 ranges
  if (hostname.includes(':')) {
    const privateIPv6Ranges = [
      /^fe80:/i, // Link-local
      /^fc00:/i, // Unique local
      /^fd00:/i, // Unique local
    ];

    for (const range of privateIPv6Ranges) {
      if (range.test(hostname)) {
        return true;
      }
    }
  }

  return false;
}

/**
 * Validate and sanitize file path
 */
export function sanitizeFilePath(path: string): string {
  // Check for path traversal
  if (path.includes('..') || path.includes('~')) {
    throw new Error('Path traversal detected');
  }

  // Check for absolute paths
  if (path.startsWith('/') || path.match(/^[a-zA-Z]:/)) {
    throw new Error('Absolute paths not allowed');
  }

  // Normalize path separators
  let sanitized = path.replace(/\\/g, '/');

  // Remove leading slashes
  sanitized = sanitized.replace(/^\/+/, '');

  // Remove multiple slashes
  sanitized = sanitized.replace(/\/+/g, '/');

  // Remove trailing slashes
  sanitized = sanitized.replace(/\/+$/, '');

  return sanitized;
}

/**
 * Validate user ID format
 */
export function validateUserId(userId: string): string {
  const trimmed = userId.trim();

  // Must be alphanumeric with hyphens or underscores
  if (!/^[a-zA-Z0-9_-]+$/.test(trimmed)) {
    throw new Error('Invalid user ID format. Only alphanumeric characters, hyphens, and underscores allowed.');
  }

  // Check length
  if (trimmed.length < 3 || trimmed.length > 64) {
    throw new Error('User ID must be between 3 and 64 characters');
  }

  return trimmed;
}

/**
 * Validate job ID format (UUID)
 */
export function validateJobId(jobId: string): string {
  const trimmed = jobId.trim();

  if (!validator.isUUID(trimmed)) {
    throw new Error('Invalid job ID format. Must be a valid UUID.');
  }

  return trimmed;
}

/**
 * Validate and sanitize processing options
 */
export function validateProcessingOptions(options: any): any {
  if (!options || typeof options !== 'object') {
    throw new Error('Processing options must be an object');
  }

  // Validate boolean fields
  const booleanFields = [
    'extractFrames',
    'extractAudio',
    'transcribeAudio',
    'detectScenes',
    'detectObjects',
    'extractText',
    'classifyContent',
    'generateSummary',
  ];

  for (const field of booleanFields) {
    if (field in options && typeof options[field] !== 'boolean') {
      throw new Error(`${field} must be a boolean`);
    }
  }

  // Validate enum fields
  if (options.frameSamplingMode &&
    !['keyframes', 'uniform', 'scene-based'].includes(options.frameSamplingMode)) {
    throw new Error('Invalid frameSamplingMode. Must be: keyframes, uniform, or scene-based');
  }

  if (options.qualityPreference &&
    !['speed', 'balanced', 'accuracy'].includes(options.qualityPreference)) {
    throw new Error('Invalid qualityPreference. Must be: speed, balanced, or accuracy');
  }

  // Validate numeric fields
  if (options.maxFrames && (!Number.isInteger(options.maxFrames) || options.maxFrames <= 0)) {
    throw new Error('maxFrames must be a positive integer');
  }

  if (options.frameSampleRate &&
    (typeof options.frameSampleRate !== 'number' || options.frameSampleRate <= 0)) {
    throw new Error('frameSampleRate must be a positive number');
  }

  // Validate array fields
  if (options.targetLanguages) {
    if (!Array.isArray(options.targetLanguages)) {
      throw new Error('targetLanguages must be an array');
    }

    if (options.targetLanguages.length > 10) {
      throw new Error('Maximum 10 target languages allowed');
    }

    for (const lang of options.targetLanguages) {
      if (typeof lang !== 'string' || !validator.isISO6391(lang)) {
        throw new Error(`Invalid language code: ${lang}. Must be ISO 639-1 format.`);
      }
    }
  }

  return options;
}

/**
 * Validate MIME type
 */
export function validateMimeType(mimeType: string, allowedTypes: string[]): void {
  if (!allowedTypes.includes(mimeType)) {
    throw new Error(
      `Invalid MIME type: ${mimeType}. Allowed types: ${allowedTypes.join(', ')}`
    );
  }
}

/**
 * Sanitize metadata object
 */
export function sanitizeMetadata(metadata: any): Record<string, string> {
  if (!metadata || typeof metadata !== 'object') {
    return {};
  }

  const sanitized: Record<string, string> = {};

  for (const [key, value] of Object.entries(metadata)) {
    // Sanitize key
    const sanitizedKey = validator.escape(String(key).trim());

    // Skip dangerous keys
    if (isDangerousKey(sanitizedKey)) {
      continue;
    }

    // Convert value to string and sanitize
    const sanitizedValue = validator.escape(String(value).trim());

    // Limit length
    if (sanitizedKey.length > 100 || sanitizedValue.length > 1000) {
      continue;
    }

    sanitized[sanitizedKey] = sanitizedValue;
  }

  return sanitized;
}

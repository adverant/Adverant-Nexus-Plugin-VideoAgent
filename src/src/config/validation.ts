/**
 * Configuration Validation with Zod Schema
 *
 * Root Cause Fixed: Environment variables were loaded without validation,
 * causing runtime errors and security risks from malformed configuration.
 *
 * Solution: Strict Zod schema validation with type safety and clear error messages.
 */

import { z } from 'zod';
import { VideoAgentConfig } from '../types';

/**
 * Comprehensive configuration schema with validation rules
 */
const ConfigSchema = z.object({
  // Server configuration
  port: z.number()
    .int('PORT must be an integer')
    .min(1024, 'PORT must be >= 1024 (reserved ports)')
    .max(65535, 'PORT must be <= 65535')
    .default(3000),

  nodeEnv: z.enum(['development', 'staging', 'production'])
    .default('development'),

  // Redis configuration
  redisUrl: z.string()
    .url('REDIS_URL must be a valid URL')
    .regex(/^redis:\/\//, 'REDIS_URL must start with redis://')
    .default('redis://localhost:6379'),

  // PostgreSQL configuration
  postgresUrl: z.string()
    .url('POSTGRES_URL must be a valid URL')
    .regex(/^postgresql:\/\//, 'POSTGRES_URL must start with postgresql://')
    .default('postgresql://unified_nexus:graphrag123@localhost:5432/nexus_graphrag'),

  // MageAgent configuration
  mageAgentUrl: z.string()
    .url('MAGEAGENT_URL must be a valid URL')
    .default('http://localhost:3000'),

  // Storage configuration
  tempDir: z.string()
    .min(1, 'TEMP_DIR cannot be empty')
    .default('/tmp/videoagent')
    .refine(path => !path.includes('..'), 'TEMP_DIR cannot contain path traversal'),

  maxVideoSize: z.number()
    .int()
    .positive('MAX_VIDEO_SIZE must be positive')
    .max(10 * 1024 * 1024 * 1024, 'MAX_VIDEO_SIZE must be <= 10GB')
    .default(2 * 1024 * 1024 * 1024),

  // Google Drive configuration (optional)
  enableGoogleDrive: z.boolean()
    .default(false),

  googleClientId: z.string()
    .optional()
    .refine((val) => !val || val.endsWith('.apps.googleusercontent.com'),
      'Invalid Google Client ID format'),

  googleClientSecret: z.string()
    .optional(),

  googleRedirectUri: z.string()
    .url('GOOGLE_REDIRECT_URI must be a valid URL')
    .optional(),

  // CORS configuration (environment-specific)
  corsOrigin: z.union([
    z.literal('*'), // Development only
    z.string().url(), // Single origin
    z.array(z.string().url()), // Multiple origins
  ]).default('*'),

  // Rate limiting configuration
  rateLimitWindowMs: z.number()
    .int()
    .positive()
    .default(15 * 60 * 1000), // 15 minutes

  rateLimitMaxRequests: z.number()
    .int()
    .positive()
    .default(100),

  // Session configuration
  sessionTimeoutMs: z.number()
    .int()
    .positive()
    .default(30 * 60 * 1000), // 30 minutes

  sessionCleanupIntervalMs: z.number()
    .int()
    .positive()
    .default(5 * 60 * 1000), // 5 minutes

  // Circuit breaker configuration
  circuitBreakerFailureThreshold: z.number()
    .int()
    .min(1)
    .default(5),

  circuitBreakerTimeoutMs: z.number()
    .int()
    .positive()
    .default(60 * 1000), // 1 minute

  circuitBreakerResetTimeoutMs: z.number()
    .int()
    .positive()
    .default(30 * 1000), // 30 seconds

  // Logging configuration
  logLevel: z.enum(['error', 'warn', 'info', 'debug'])
    .default('info'),

  // Cache configuration
  cacheTTLSeconds: z.number()
    .int()
    .positive()
    .default(60),
});

export type ValidatedConfig = z.infer<typeof ConfigSchema> & VideoAgentConfig;

/**
 * Load and validate configuration from environment variables
 *
 * @throws {ConfigurationError} If validation fails with detailed error messages
 */
export function loadAndValidateConfig(): ValidatedConfig {
  const rawConfig = {
    port: parseInt(process.env.PORT || '3000', 10),
    nodeEnv: process.env.NODE_ENV || 'development',
    redisUrl: process.env.REDIS_URL || 'redis://localhost:6379',
    postgresUrl: process.env.POSTGRES_URL || 'postgresql://unified_nexus:graphrag123@localhost:5432/nexus_graphrag',
    mageAgentUrl: process.env.MAGEAGENT_URL || 'http://localhost:3000',
    tempDir: process.env.TEMP_DIR || '/tmp/videoagent',
    maxVideoSize: parseInt(process.env.MAX_VIDEO_SIZE || '2147483648', 10),
    enableGoogleDrive: process.env.ENABLE_GOOGLE_DRIVE === 'true',
    googleClientId: process.env.GOOGLE_CLIENT_ID,
    googleClientSecret: process.env.GOOGLE_CLIENT_SECRET,
    googleRedirectUri: process.env.GOOGLE_REDIRECT_URI,
    corsOrigin: parseCorsOrigin(process.env.CORS_ORIGIN),
    rateLimitWindowMs: parseInt(process.env.RATE_LIMIT_WINDOW_MS || '900000', 10),
    rateLimitMaxRequests: parseInt(process.env.RATE_LIMIT_MAX_REQUESTS || '100', 10),
    sessionTimeoutMs: parseInt(process.env.SESSION_TIMEOUT_MS || '1800000', 10),
    sessionCleanupIntervalMs: parseInt(process.env.SESSION_CLEANUP_INTERVAL_MS || '300000', 10),
    circuitBreakerFailureThreshold: parseInt(process.env.CIRCUIT_BREAKER_FAILURE_THRESHOLD || '5', 10),
    circuitBreakerTimeoutMs: parseInt(process.env.CIRCUIT_BREAKER_TIMEOUT_MS || '60000', 10),
    circuitBreakerResetTimeoutMs: parseInt(process.env.CIRCUIT_BREAKER_RESET_TIMEOUT_MS || '30000', 10),
    logLevel: process.env.LOG_LEVEL || 'info',
    cacheTTLSeconds: parseInt(process.env.CACHE_TTL_SECONDS || '60', 10),
  };

  // Validate configuration
  const result = ConfigSchema.safeParse(rawConfig);

  if (!result.success) {
    const errors = result.error.issues.map(issue =>
      `${issue.path.join('.')}: ${issue.message}`
    ).join('\n');

    throw new ConfigurationError(
      `Configuration validation failed:\n${errors}`
    );
  }

  const validatedConfig = result.data;

  // Additional cross-field validations
  validateCrossFieldConstraints(validatedConfig);

  // Warn about insecure configurations in production
  warnAboutInsecureConfig(validatedConfig);

  return validatedConfig as ValidatedConfig;
}

/**
 * Parse CORS origin from environment variable
 */
function parseCorsOrigin(corsOrigin?: string): string | string[] | '*' {
  if (!corsOrigin || corsOrigin === '*') {
    return '*';
  }

  if (corsOrigin.includes(',')) {
    return corsOrigin.split(',').map(origin => origin.trim());
  }

  return corsOrigin;
}

/**
 * Validate cross-field constraints
 */
function validateCrossFieldConstraints(config: z.infer<typeof ConfigSchema>): void {
  // Google Drive validation
  if (config.enableGoogleDrive) {
    if (!config.googleClientId || !config.googleClientSecret) {
      throw new ConfigurationError(
        'Google Drive is enabled but GOOGLE_CLIENT_ID or GOOGLE_CLIENT_SECRET is missing'
      );
    }
  }

  // Circuit breaker validation
  if (config.circuitBreakerResetTimeoutMs >= config.circuitBreakerTimeoutMs) {
    throw new ConfigurationError(
      'CIRCUIT_BREAKER_RESET_TIMEOUT_MS must be less than CIRCUIT_BREAKER_TIMEOUT_MS'
    );
  }

  // Session cleanup validation
  if (config.sessionCleanupIntervalMs >= config.sessionTimeoutMs) {
    throw new ConfigurationError(
      'SESSION_CLEANUP_INTERVAL_MS must be less than SESSION_TIMEOUT_MS'
    );
  }
}

/**
 * Warn about insecure configurations in production
 */
function warnAboutInsecureConfig(config: z.infer<typeof ConfigSchema>): void {
  if (config.nodeEnv === 'production') {
    if (config.corsOrigin === '*') {
      console.warn(
        '⚠️  WARNING: CORS is set to "*" in production. ' +
        'This is a security risk. Set CORS_ORIGIN to specific domains.'
      );
    }

    if (config.rateLimitMaxRequests > 1000) {
      console.warn(
        '⚠️  WARNING: Rate limit is very high (' + config.rateLimitMaxRequests + ' requests). ' +
        'Consider lowering RATE_LIMIT_MAX_REQUESTS in production.'
      );
    }

    if (config.logLevel === 'debug') {
      console.warn(
        '⚠️  WARNING: Log level is set to "debug" in production. ' +
        'This may expose sensitive information.'
      );
    }
  }
}

/**
 * Custom configuration error class
 */
export class ConfigurationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = 'ConfigurationError';
    Error.captureStackTrace(this, this.constructor);
  }
}

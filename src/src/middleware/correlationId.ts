/**
 * Request Correlation ID Middleware
 *
 * Root Cause Fixed: Missing request correlation IDs made distributed tracing
 * impossible, hindering debugging of issues across services.
 *
 * Solution: Generate or extract correlation IDs for all requests, propagate
 * through service calls, and include in all logs and responses.
 */

import { Request, Response, NextFunction } from 'express';
import { v4 as uuidv4 } from 'uuid';

/**
 * Correlation ID header names (case-insensitive)
 */
export const CORRELATION_ID_HEADER = 'x-correlation-id';
export const REQUEST_ID_HEADER = 'x-request-id';

/**
 * Extended Request interface with correlation ID
 */
export interface RequestWithCorrelation extends Request {
  correlationId: string;
  requestId: string;
}

/**
 * Create correlation ID middleware
 *
 * Generates or extracts correlation IDs from incoming requests and
 * adds them to request object and response headers.
 */
export function createCorrelationIdMiddleware() {
  return (req: Request, res: Response, next: NextFunction): void => {
    // Extract or generate correlation ID
    const correlationId = extractOrGenerateCorrelationId(req);

    // Generate unique request ID
    const requestId = uuidv4();

    // Add to request object
    (req as RequestWithCorrelation).correlationId = correlationId;
    (req as RequestWithCorrelation).requestId = requestId;

    // Add to response headers for client tracking
    res.setHeader(CORRELATION_ID_HEADER, correlationId);
    res.setHeader(REQUEST_ID_HEADER, requestId);

    // Log request with correlation IDs
    console.log(
      `[${correlationId}][${requestId}] ${req.method} ${req.path} - Client: ${getClientInfo(req)}`
    );

    // Add timing information
    const startTime = Date.now();

    // Log response when finished
    res.on('finish', () => {
      const duration = Date.now() - startTime;
      const logLevel = res.statusCode >= 500 ? 'ERROR' :
        res.statusCode >= 400 ? 'WARN' : 'INFO';

      console.log(
        `[${correlationId}][${requestId}] ${req.method} ${req.path} - ` +
        `Status: ${res.statusCode} - Duration: ${duration}ms - Level: ${logLevel}`
      );
    });

    next();
  };
}

/**
 * Extract correlation ID from request headers or generate new one
 */
function extractOrGenerateCorrelationId(req: Request): string {
  // Check standard correlation ID header
  const correlationIdHeader = req.headers[CORRELATION_ID_HEADER] ||
    req.headers[REQUEST_ID_HEADER];

  if (correlationIdHeader) {
    const correlationId = Array.isArray(correlationIdHeader)
      ? correlationIdHeader[0]
      : correlationIdHeader;

    // Validate correlation ID format
    if (isValidCorrelationId(correlationId)) {
      return correlationId;
    }
  }

  // Check other common headers
  const otherHeaders = [
    'x-trace-id',
    'x-b3-traceid', // Zipkin
    'traceparent', // W3C Trace Context
  ];

  for (const header of otherHeaders) {
    const value = req.headers[header];
    if (value) {
      const id = Array.isArray(value) ? value[0] : value;
      if (isValidCorrelationId(id)) {
        return id;
      }
    }
  }

  // Generate new correlation ID
  return uuidv4();
}

/**
 * Validate correlation ID format
 */
function isValidCorrelationId(id: string): boolean {
  // UUID format or alphanumeric string
  const uuidRegex = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
  const alphanumericRegex = /^[a-zA-Z0-9_-]{8,64}$/;

  return uuidRegex.test(id) || alphanumericRegex.test(id);
}

/**
 * Get client information from request
 */
function getClientInfo(req: Request): string {
  const ip = getClientIp(req);
  const userAgent = req.headers['user-agent'] || 'unknown';

  return `IP=${ip}, UA=${truncate(userAgent, 50)}`;
}

/**
 * Get client IP address
 */
function getClientIp(req: Request): string {
  // Check X-Forwarded-For header
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

  // Fall back to socket address
  return req.socket.remoteAddress || 'unknown';
}

/**
 * Truncate string to max length
 */
function truncate(str: string, maxLength: number): string {
  if (str.length <= maxLength) {
    return str;
  }
  return str.substring(0, maxLength - 3) + '...';
}

/**
 * Get correlation ID from request
 */
export function getCorrelationId(req: Request): string | undefined {
  return (req as RequestWithCorrelation).correlationId;
}

/**
 * Get request ID from request
 */
export function getRequestId(req: Request): string | undefined {
  return (req as RequestWithCorrelation).requestId;
}

/**
 * Create logger with correlation ID context
 */
export function createCorrelatedLogger(req: Request) {
  const correlationId = getCorrelationId(req) || 'unknown';
  const requestId = getRequestId(req) || 'unknown';

  return {
    debug: (message: string, ...args: any[]) => {
      console.debug(`[${correlationId}][${requestId}] DEBUG: ${message}`, ...args);
    },
    info: (message: string, ...args: any[]) => {
      console.info(`[${correlationId}][${requestId}] INFO: ${message}`, ...args);
    },
    warn: (message: string, ...args: any[]) => {
      console.warn(`[${correlationId}][${requestId}] WARN: ${message}`, ...args);
    },
    error: (message: string, error?: Error, ...args: any[]) => {
      console.error(
        `[${correlationId}][${requestId}] ERROR: ${message}`,
        error ? error.stack : '',
        ...args
      );
    },
  };
}

/**
 * Propagate correlation ID to external service calls
 *
 * Use this function when making HTTP requests to other services
 * to maintain distributed tracing.
 */
export function getCorrelationHeaders(req: Request): Record<string, string> {
  const correlationId = getCorrelationId(req);
  const requestId = getRequestId(req);

  const headers: Record<string, string> = {};

  if (correlationId) {
    headers[CORRELATION_ID_HEADER] = correlationId;
  }

  if (requestId) {
    headers[REQUEST_ID_HEADER] = requestId;
  }

  return headers;
}

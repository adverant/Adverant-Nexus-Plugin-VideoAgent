/**
 * JWT Validator for VideoAgent WebSocket Authentication
 * Validates JWT tokens issued by Nexus Auth Service
 */

import jwt from 'jsonwebtoken';
import { createLogger } from './logger';

const logger = createLogger('JWTValidator');

export interface JWTClaims {
  user_id: string;
  email: string;
  subscription_tier: string;
  permissions: string[];
  iss: string;
  sub: string;
  exp: number;
  iat: number;
  nbf: number;
  jti: string;
}

export class JWTValidator {
  private secret: string;
  private issuer: string;

  constructor(secret?: string, issuer: string = 'nexus-auth-service') {
    this.secret = secret || process.env.JWT_SECRET || '';
    this.issuer = issuer;

    if (!this.secret) {
      logger.error('JWT_SECRET not configured! Authentication will fail.');
      throw new Error('JWT_SECRET environment variable is required');
    }

    if (this.secret.length < 32) {
      logger.error('JWT_SECRET is too short! Must be at least 32 characters.');
      throw new Error('JWT_SECRET must be at least 32 characters');
    }

    logger.info('JWT Validator initialized');
  }

  /**
   * Validate JWT token and return claims
   * @param token JWT token string
   * @returns Decoded JWT claims
   * @throws Error if token is invalid, expired, or malformed
   */
  validateToken(token: string): JWTClaims {
    try {
      // Verify and decode token with constant-time comparison
      const decoded = jwt.verify(token, this.secret, {
        algorithms: ['HS256'],
        issuer: this.issuer,
        clockTolerance: 5 // Allow 5 seconds clock skew
      }) as JWTClaims;

      // Additional validation checks
      if (!decoded.user_id) {
        throw new Error('Token missing user_id claim');
      }

      if (!decoded.email) {
        throw new Error('Token missing email claim');
      }

      // Verify expiration explicitly (jwt.verify already checks, but double-check)
      const now = Math.floor(Date.now() / 1000);
      if (decoded.exp && decoded.exp < now) {
        throw new Error('Token has expired');
      }

      // Verify not before
      if (decoded.nbf && decoded.nbf > now) {
        throw new Error('Token not yet valid');
      }

      logger.debug(`Token validated successfully for user: ${decoded.user_id}`);
      return decoded;

    } catch (error: unknown) {
      if (error instanceof jwt.TokenExpiredError) {
        logger.warn(`Token validation failed: Token expired`);
        throw new Error('Token has expired');
      } else if (error instanceof jwt.JsonWebTokenError) {
        logger.warn(`Token validation failed: ${error.message}`);
        throw new Error(`Invalid token: ${error.message}`);
      } else if (error instanceof jwt.NotBeforeError) {
        logger.warn(`Token validation failed: Token not yet valid`);
        throw new Error('Token not yet valid');
      } else if (error instanceof Error) {
        logger.error(`Token validation failed: ${error.message}`);
        throw error;
      } else {
        logger.error(`Token validation failed: Unknown error`);
        throw new Error('Token validation failed');
      }
    }
  }

  /**
   * Extract token from Authorization header
   * Supports: "Bearer <token>" format
   * @param authHeader Authorization header value
   * @returns Extracted token or null
   */
  extractFromHeader(authHeader: string | undefined): string | null {
    if (!authHeader) {
      return null;
    }

    // Support "Bearer <token>" format
    const parts = authHeader.split(' ');
    if (parts.length === 2 && parts[0] === 'Bearer') {
      return parts[1];
    }

    // Support direct token (no "Bearer" prefix)
    if (parts.length === 1) {
      return parts[0];
    }

    return null;
  }

  /**
   * Extract token from query parameter
   * @param queryToken Query parameter value
   * @returns Extracted token or null
   */
  extractFromQuery(queryToken: string | undefined): string | null {
    if (!queryToken || queryToken.trim() === '') {
      return null;
    }
    return queryToken;
  }

  /**
   * Extract user context from JWT claims
   * @param claims JWT claims
   * @returns User context object
   */
  extractUserContext(claims: JWTClaims) {
    return {
      userId: claims.user_id,
      email: claims.email,
      subscriptionTier: claims.subscription_tier,
      permissions: claims.permissions || [],
      jti: claims.jti
    };
  }
}

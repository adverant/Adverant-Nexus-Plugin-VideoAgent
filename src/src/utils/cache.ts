/**
 * Redis Query Cache Layer
 *
 * Root Cause Fixed: Repeated database queries for identical data caused
 * unnecessary load and increased response times.
 *
 * Solution: Redis-backed caching layer with TTL, pattern invalidation,
 * and type-safe operations. Implements Facade pattern for clean abstraction.
 */

import Redis from 'ioredis';
import { createHash } from 'crypto';

/**
 * Cache configuration
 */
export interface CacheConfig {
  redis: Redis;
  defaultTTL?: number; // Default TTL in seconds
  keyPrefix?: string; // Key prefix for namespacing
  enableCompression?: boolean; // Compress large values
  enableStats?: boolean; // Track cache statistics
}

/**
 * Cache statistics
 */
export interface CacheStats {
  hits: number;
  misses: number;
  sets: number;
  deletes: number;
  invalidations: number;
  hitRate: number;
}

/**
 * Cache entry metadata
 */
interface CacheEntry<T> {
  value: T;
  timestamp: number;
  ttl: number;
}

/**
 * Query Cache Implementation
 *
 * Provides type-safe Redis caching with automatic serialization,
 * TTL management, and pattern-based invalidation.
 */
export class QueryCache {
  private readonly redis: Redis;
  private readonly defaultTTL: number;
  private readonly keyPrefix: string;
  private readonly enableCompression: boolean;
  private readonly enableStats: boolean;

  // Statistics
  private stats: CacheStats = {
    hits: 0,
    misses: 0,
    sets: 0,
    deletes: 0,
    invalidations: 0,
    hitRate: 0,
  };

  constructor(config: CacheConfig) {
    this.redis = config.redis;
    this.defaultTTL = config.defaultTTL || 60;
    this.keyPrefix = config.keyPrefix || 'cache';
    this.enableCompression = config.enableCompression || false;
    this.enableStats = config.enableStats !== false;
  }

  /**
   * Get value from cache
   *
   * Returns null if key doesn't exist or has expired.
   */
  async get<T>(key: string): Promise<T | null> {
    try {
      const fullKey = this.buildKey(key);
      const cached = await this.redis.get(fullKey);

      if (!cached) {
        this.incrementMiss();
        return null;
      }

      // Parse cached entry
      const entry = this.deserialize<CacheEntry<T>>(cached);

      if (!entry) {
        this.incrementMiss();
        return null;
      }

      // Check if expired (defensive check)
      const now = Date.now();
      if (entry.ttl > 0 && now - entry.timestamp > entry.ttl * 1000) {
        await this.delete(key);
        this.incrementMiss();
        return null;
      }

      this.incrementHit();
      return entry.value;
    } catch (error) {
      console.error('Cache get error:', error);
      this.incrementMiss();
      return null;
    }
  }

  /**
   * Set value in cache with optional TTL
   */
  async set<T>(key: string, value: T, ttl?: number): Promise<void> {
    try {
      const fullKey = this.buildKey(key);
      const effectiveTTL = ttl !== undefined ? ttl : this.defaultTTL;

      // Create cache entry with metadata
      const entry: CacheEntry<T> = {
        value,
        timestamp: Date.now(),
        ttl: effectiveTTL,
      };

      const serialized = this.serialize(entry);

      if (effectiveTTL > 0) {
        await this.redis.setex(fullKey, effectiveTTL, serialized);
      } else {
        // TTL of 0 means no expiration
        await this.redis.set(fullKey, serialized);
      }

      this.incrementSet();
    } catch (error) {
      console.error('Cache set error:', error);
      throw error;
    }
  }

  /**
   * Get or set pattern: Try cache first, compute and cache if miss
   */
  async getOrSet<T>(
    key: string,
    factory: () => Promise<T>,
    ttl?: number
  ): Promise<T> {
    // Try cache first
    const cached = await this.get<T>(key);
    if (cached !== null) {
      return cached;
    }

    // Cache miss - compute value
    const value = await factory();

    // Cache the computed value
    await this.set(key, value, ttl);

    return value;
  }

  /**
   * Delete key from cache
   */
  async delete(key: string): Promise<void> {
    try {
      const fullKey = this.buildKey(key);
      await this.redis.del(fullKey);
      this.incrementDelete();
    } catch (error) {
      console.error('Cache delete error:', error);
      throw error;
    }
  }

  /**
   * Check if key exists in cache
   */
  async exists(key: string): Promise<boolean> {
    try {
      const fullKey = this.buildKey(key);
      const exists = await this.redis.exists(fullKey);
      return exists === 1;
    } catch (error) {
      console.error('Cache exists error:', error);
      return false;
    }
  }

  /**
   * Invalidate all keys matching a pattern
   *
   * Pattern examples:
   * - "user:*" - All user-related keys
   * - "job:123:*" - All keys for job 123
   * - "*:status" - All status keys
   */
  async invalidatePattern(pattern: string): Promise<number> {
    try {
      const fullPattern = this.buildKey(pattern);
      const keys = await this.redis.keys(fullPattern);

      if (keys.length === 0) {
        return 0;
      }

      // Delete in batches to avoid blocking Redis
      const batchSize = 100;
      let deleted = 0;

      for (let i = 0; i < keys.length; i += batchSize) {
        const batch = keys.slice(i, i + batchSize);
        await this.redis.del(...batch);
        deleted += batch.length;
      }

      this.stats.invalidations += deleted;
      return deleted;
    } catch (error) {
      console.error('Cache invalidate pattern error:', error);
      throw error;
    }
  }

  /**
   * Clear all cache entries (use with caution!)
   */
  async clear(): Promise<void> {
    try {
      const pattern = this.buildKey('*');
      const keys = await this.redis.keys(pattern);

      if (keys.length > 0) {
        await this.redis.del(...keys);
      }

      this.resetStats();
    } catch (error) {
      console.error('Cache clear error:', error);
      throw error;
    }
  }

  /**
   * Get time to live for a key
   */
  async ttl(key: string): Promise<number> {
    try {
      const fullKey = this.buildKey(key);
      return await this.redis.ttl(fullKey);
    } catch (error) {
      console.error('Cache ttl error:', error);
      return -2; // Key doesn't exist
    }
  }

  /**
   * Refresh TTL for a key without changing the value
   */
  async touch(key: string, ttl?: number): Promise<void> {
    try {
      const fullKey = this.buildKey(key);
      const effectiveTTL = ttl !== undefined ? ttl : this.defaultTTL;

      if (effectiveTTL > 0) {
        await this.redis.expire(fullKey, effectiveTTL);
      }
    } catch (error) {
      console.error('Cache touch error:', error);
      throw error;
    }
  }

  /**
   * Get cache statistics
   */
  getStats(): CacheStats {
    if (!this.enableStats) {
      return {
        hits: 0,
        misses: 0,
        sets: 0,
        deletes: 0,
        invalidations: 0,
        hitRate: 0,
      };
    }

    const total = this.stats.hits + this.stats.misses;
    const hitRate = total > 0 ? this.stats.hits / total : 0;

    return {
      ...this.stats,
      hitRate: Math.round(hitRate * 10000) / 100, // Percentage with 2 decimals
    };
  }

  /**
   * Reset cache statistics
   */
  resetStats(): void {
    this.stats = {
      hits: 0,
      misses: 0,
      sets: 0,
      deletes: 0,
      invalidations: 0,
      hitRate: 0,
    };
  }

  /**
   * Generate cache key from complex parameters
   *
   * Useful for caching query results based on multiple parameters.
   */
  static generateKey(prefix: string, params: Record<string, any>): string {
    // Sort keys for consistent hashing
    const sortedKeys = Object.keys(params).sort();
    const paramsString = sortedKeys
      .map(key => `${key}=${JSON.stringify(params[key])}`)
      .join('&');

    // Hash to keep keys manageable
    const hash = createHash('sha256')
      .update(paramsString)
      .digest('hex')
      .substring(0, 16);

    return `${prefix}:${hash}`;
  }

  /**
   * Build full cache key with prefix
   */
  private buildKey(key: string): string {
    return `${this.keyPrefix}:${key}`;
  }

  /**
   * Serialize value for storage
   */
  private serialize<T>(value: T): string {
    try {
      return JSON.stringify(value);
    } catch (error) {
      console.error('Serialization error:', error);
      throw new Error('Failed to serialize cache value');
    }
  }

  /**
   * Deserialize value from storage
   */
  private deserialize<T>(value: string): T | null {
    try {
      return JSON.parse(value) as T;
    } catch (error) {
      console.error('Deserialization error:', error);
      return null;
    }
  }

  /**
   * Update statistics
   */
  private incrementHit(): void {
    if (this.enableStats) {
      this.stats.hits++;
    }
  }

  private incrementMiss(): void {
    if (this.enableStats) {
      this.stats.misses++;
    }
  }

  private incrementSet(): void {
    if (this.enableStats) {
      this.stats.sets++;
    }
  }

  private incrementDelete(): void {
    if (this.enableStats) {
      this.stats.deletes++;
    }
  }
}

/**
 * Cache decorator for class methods
 *
 * Usage:
 * @Cacheable('user', 60)
 * async getUser(userId: string): Promise<User> {
 *   return await database.getUser(userId);
 * }
 */
export function Cacheable(keyPrefix: string, ttl?: number) {
  return function (
    target: any,
    propertyKey: string,
    descriptor: PropertyDescriptor
  ) {
    const originalMethod = descriptor.value;

    descriptor.value = async function (...args: any[]) {
      const cache: QueryCache = (this as any).cache;

      if (!cache) {
        // No cache available, call original method
        return originalMethod.apply(this, args);
      }

      // Generate cache key from arguments
      const cacheKey = QueryCache.generateKey(keyPrefix, { args });

      // Try cache first
      return cache.getOrSet(
        cacheKey,
        () => originalMethod.apply(this, args),
        ttl
      );
    };

    return descriptor;
  };
}

/**
 * Cache invalidation decorator
 *
 * Usage:
 * @InvalidateCache('user:*')
 * async updateUser(userId: string, data: UserData): Promise<void> {
 *   await database.updateUser(userId, data);
 * }
 */
export function InvalidateCache(pattern: string) {
  return function (
    target: any,
    propertyKey: string,
    descriptor: PropertyDescriptor
  ) {
    const originalMethod = descriptor.value;

    descriptor.value = async function (...args: any[]) {
      const result = await originalMethod.apply(this, args);

      const cache: QueryCache = (this as any).cache;
      if (cache) {
        await cache.invalidatePattern(pattern);
      }

      return result;
    };

    return descriptor;
  };
}

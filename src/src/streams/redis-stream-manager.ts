/**
 * Redis Streams Manager for Real-Time Video Processing
 * Handles producing and consuming video frames via Redis Streams
 */

import Redis from 'ioredis';
import { createLogger } from '../utils/logger';

const logger = createLogger('RedisStreamManager');

export interface StreamMessage {
  clientId: string;
  sessionId: string;
  userId: string;
  frame: {
    type: 'frame';
    timestamp: number;
    frameNumber: number;
    data: string;
    metadata: {
      width: number;
      height: number;
      format: string;
    };
  };
  receivedAt: number;
}

export interface ResultMessage {
  type: 'partial' | 'refined' | 'final';
  frameNumber: number;
  timestamp: number;
  detections?: any[];
  scene?: any;
  objects?: any[];
  latency: number;
  error?: string;
}

export type ResultCallback = (result: ResultMessage) => void;

export class RedisStreamManager {
  private redis: Redis;
  private consumerRedis: Redis;
  private subscribers: Map<string, ResultCallback>;
  private consumerGroup: string = 'videoagent-api';
  private consumerName: string = `api-${Date.now()}`;
  private isConsuming: boolean = false;

  constructor(private redisUrl: string) {
    this.subscribers = new Map();
  }

  /**
   * Initialize Redis connections
   */
  async initialize(): Promise<void> {
    logger.info('Initializing Redis Streams manager');

    // Connection for publishing
    this.redis = new Redis(this.redisUrl, {
      enableReadyCheck: true,
      maxRetriesPerRequest: 3,
      retryStrategy: (times: number) => {
        const delay = Math.min(times * 50, 2000);
        return delay;
      }
    });

    // Separate connection for consuming (blocking operations)
    this.consumerRedis = new Redis(this.redisUrl, {
      enableReadyCheck: true,
      maxRetriesPerRequest: 3
    });

    // Wait for connections
    await Promise.all([
      new Promise((resolve) => this.redis.once('ready', resolve)),
      new Promise((resolve) => this.consumerRedis.once('ready', resolve))
    ]);

    // Create consumer group if it doesn't exist
    await this.createConsumerGroup();

    logger.info('Redis Streams manager initialized');
  }

  /**
   * Create consumer group for results stream
   */
  private async createConsumerGroup(): Promise<void> {
    try {
      await this.consumerRedis.xgroup(
        'CREATE',
        'videoagent:results',
        this.consumerGroup,
        '0',
        'MKSTREAM'
      );
      logger.info(`Created consumer group: ${this.consumerGroup}`);
    } catch (error: any) {
      if (error.message.includes('BUSYGROUP')) {
        logger.info(`Consumer group ${this.consumerGroup} already exists`);
      } else {
        logger.error('Error creating consumer group:', error);
        throw error;
      }
    }
  }

  /**
   * Publish frame to processing stream
   */
  async publishFrame(streamId: string, message: StreamMessage): Promise<string> {
    const streamKey = `videoagent:frames:${streamId}`;

    try {
      // Serialize frame data
      const serialized = {
        clientId: message.clientId,
        sessionId: message.sessionId,
        userId: message.userId,
        frameType: message.frame.type,
        timestamp: message.frame.timestamp.toString(),
        frameNumber: message.frame.frameNumber.toString(),
        frameData: message.frame.data,
        width: message.frame.metadata.width.toString(),
        height: message.frame.metadata.height.toString(),
        format: message.frame.metadata.format,
        receivedAt: message.receivedAt.toString()
      };

      // Add to stream with auto-generated ID
      const messageId = await this.redis.xadd(
        streamKey,
        'MAXLEN', '~', '1000', // Keep ~1000 messages (trim old ones)
        '*', // Auto-generate ID
        ...this.flattenObject(serialized)
      );

      logger.debug(`Published frame ${message.frame.frameNumber} to ${streamKey}: ${messageId}`);
      return messageId as string;

    } catch (error) {
      logger.error(`Error publishing frame to ${streamKey}:`, error);
      throw error;
    }
  }

  /**
   * Subscribe to processing results for a stream
   */
  subscribeToResults(streamId: string, callback: ResultCallback): void {
    logger.info(`Subscribing to results for stream: ${streamId}`);
    this.subscribers.set(streamId, callback);

    // Start consuming if not already running
    if (!this.isConsuming) {
      this.startConsuming();
    }
  }

  /**
   * Unsubscribe from results for a stream
   */
  unsubscribeFromResults(streamId: string): void {
    logger.info(`Unsubscribing from results for stream: ${streamId}`);
    this.subscribers.delete(streamId);
  }

  /**
   * Start consuming results from Redis Streams
   */
  private async startConsuming(): Promise<void> {
    if (this.isConsuming) return;

    this.isConsuming = true;
    logger.info('Starting Redis Streams consumer');

    while (this.isConsuming) {
      try {
        // Read new messages from the stream
        const results = await this.consumerRedis.xreadgroup(
          'GROUP', this.consumerGroup, this.consumerName,
          'COUNT', '10', // Read up to 10 messages at once
          'BLOCK', '1000', // Block for 1 second
          'STREAMS', 'videoagent:results', '>'
        );

        if (!results) continue;

        // Process each message
        for (const [streamKey, messages] of results) {
          for (const [messageId, fields] of messages) {
            await this.processResultMessage(messageId as string, fields as string[]);
          }
        }

      } catch (error) {
        if (this.isConsuming) {
          logger.error('Error consuming from Redis Streams:', error);
          await new Promise(resolve => setTimeout(resolve, 1000)); // Wait before retrying
        }
      }
    }

    logger.info('Stopped Redis Streams consumer');
  }

  /**
   * Process a result message
   */
  private async processResultMessage(messageId: string, fields: string[]): Promise<void> {
    try {
      // Parse fields into object
      const data = this.parseFields(fields);

      // Extract streamId from the result
      const streamId = data.streamId;
      if (!streamId) {
        logger.warn(`Result message ${messageId} missing streamId`);
        return;
      }

      // Check if we have a subscriber for this stream
      const callback = this.subscribers.get(streamId);
      if (!callback) {
        logger.debug(`No subscriber for stream ${streamId}, skipping`);
        await this.acknowledgeMessage(messageId);
        return;
      }

      // Build result object
      const result: ResultMessage = {
        type: data.type as any,
        frameNumber: parseInt(data.frameNumber || '0'),
        timestamp: parseInt(data.timestamp || '0'),
        latency: parseInt(data.latency || '0')
      };

      // Parse optional fields
      if (data.detections) {
        try {
          result.detections = JSON.parse(data.detections);
        } catch (e) {
          logger.warn('Failed to parse detections JSON');
        }
      }

      if (data.scene) {
        try {
          result.scene = JSON.parse(data.scene);
        } catch (e) {
          logger.warn('Failed to parse scene JSON');
        }
      }

      if (data.objects) {
        try {
          result.objects = JSON.parse(data.objects);
        } catch (e) {
          logger.warn('Failed to parse objects JSON');
        }
      }

      if (data.error) {
        result.error = data.error;
      }

      // Send to callback
      callback(result);

      // Acknowledge message
      await this.acknowledgeMessage(messageId);

    } catch (error) {
      logger.error(`Error processing result message ${messageId}:`, error);
    }
  }

  /**
   * Acknowledge a message (mark as processed)
   */
  private async acknowledgeMessage(messageId: string): Promise<void> {
    try {
      await this.consumerRedis.xack('videoagent:results', this.consumerGroup, messageId);
    } catch (error) {
      logger.error(`Error acknowledging message ${messageId}:`, error);
    }
  }

  /**
   * Flatten object into Redis field-value pairs
   */
  private flattenObject(obj: Record<string, any>): string[] {
    const pairs: string[] = [];
    for (const [key, value] of Object.entries(obj)) {
      pairs.push(key, String(value));
    }
    return pairs;
  }

  /**
   * Parse Redis field-value pairs into object
   */
  private parseFields(fields: string[]): Record<string, string> {
    const obj: Record<string, string> = {};
    for (let i = 0; i < fields.length; i += 2) {
      obj[fields[i]] = fields[i + 1];
    }
    return obj;
  }

  /**
   * Get stream statistics
   */
  async getStreamStats(streamId: string): Promise<any> {
    const frameStreamKey = `videoagent:frames:${streamId}`;

    try {
      const info = await this.redis.xinfo('STREAM', frameStreamKey);
      return this.parseStreamInfo(info);
    } catch (error) {
      logger.error(`Error getting stats for ${frameStreamKey}:`, error);
      return null;
    }
  }

  /**
   * Parse XINFO STREAM response
   */
  private parseStreamInfo(info: any[]): any {
    const parsed: any = {};
    for (let i = 0; i < info.length; i += 2) {
      parsed[info[i]] = info[i + 1];
    }
    return parsed;
  }

  /**
   * Publish result to results stream (for testing)
   */
  async publishResult(streamId: string, result: ResultMessage): Promise<string> {
    try {
      const serialized: any = {
        streamId,
        type: result.type,
        frameNumber: result.frameNumber.toString(),
        timestamp: result.timestamp.toString(),
        latency: result.latency.toString()
      };

      if (result.detections) {
        serialized.detections = JSON.stringify(result.detections);
      }
      if (result.scene) {
        serialized.scene = JSON.stringify(result.scene);
      }
      if (result.objects) {
        serialized.objects = JSON.stringify(result.objects);
      }
      if (result.error) {
        serialized.error = result.error;
      }

      const messageId = await this.redis.xadd(
        'videoagent:results',
        'MAXLEN', '~', '10000',
        '*',
        ...this.flattenObject(serialized)
      );

      return messageId as string;

    } catch (error) {
      logger.error('Error publishing result:', error);
      throw error;
    }
  }

  /**
   * Shutdown Redis connections
   */
  async shutdown(): Promise<void> {
    logger.info('Shutting down Redis Streams manager');

    this.isConsuming = false;
    this.subscribers.clear();

    await Promise.all([
      this.redis.quit(),
      this.consumerRedis.quit()
    ]);

    logger.info('Redis Streams manager shut down');
  }
}

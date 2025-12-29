import { WebSocketServer, WebSocket } from 'ws';
import { Server } from 'http';
import { Redis } from 'ioredis';
import { ProgressUpdate } from '../types';

/**
 * ProgressServer - WebSocket server for real-time progress updates
 * Listens to Redis pub/sub channels and broadcasts to connected clients
 */
export class ProgressServer {
  private wss: WebSocketServer;
  private redis: Redis;
  private subscribers: Map<string, Set<WebSocket>>;

  constructor(server: Server, redisUrl: string) {
    // Create WebSocket server
    this.wss = new WebSocketServer({
      server,
      path: '/ws/progress',
    });

    // Initialize Redis subscriber
    this.redis = new Redis(redisUrl);

    // Track job subscriptions
    this.subscribers = new Map();

    // Setup WebSocket handlers
    this.setupWebSocketHandlers();
  }

  /**
   * Setup WebSocket connection handlers
   */
  private setupWebSocketHandlers(): void {
    this.wss.on('connection', (ws: WebSocket) => {
      console.log('WebSocket client connected');

      // Handle incoming messages (subscription requests)
      ws.on('message', (data: Buffer) => {
        try {
          const message = JSON.parse(data.toString());
          this.handleMessage(ws, message);
        } catch (error) {
          ws.send(JSON.stringify({
            type: 'error',
            message: 'Invalid message format',
          }));
        }
      });

      // Handle client disconnect
      ws.on('close', () => {
        console.log('WebSocket client disconnected');
        this.unsubscribeAll(ws);
      });

      // Handle errors
      ws.on('error', (error) => {
        console.error('WebSocket error:', error);
        this.unsubscribeAll(ws);
      });

      // Send welcome message
      ws.send(JSON.stringify({
        type: 'connected',
        message: 'Connected to VideoAgent progress server',
      }));
    });
  }

  /**
   * Handle client messages
   */
  private async handleMessage(ws: WebSocket, message: any): Promise<void> {
    const { type, jobId } = message;

    switch (type) {
      case 'subscribe':
        if (jobId) {
          await this.subscribe(ws, jobId);
        }
        break;

      case 'unsubscribe':
        if (jobId) {
          this.unsubscribe(ws, jobId);
        }
        break;

      case 'ping':
        ws.send(JSON.stringify({ type: 'pong' }));
        break;

      default:
        ws.send(JSON.stringify({
          type: 'error',
          message: `Unknown message type: ${type}`,
        }));
    }
  }

  /**
   * Subscribe client to job progress updates
   */
  private async subscribe(ws: WebSocket, jobId: string): Promise<void> {
    // Add client to subscribers
    if (!this.subscribers.has(jobId)) {
      this.subscribers.set(jobId, new Set());

      // Subscribe to Redis channel for this job
      const channel = `videoagent:progress:${jobId}`;
      await this.redis.subscribe(channel);

      // Listen for messages on this channel
      this.redis.on('message', (ch, message) => {
        if (ch === channel) {
          this.broadcastToJob(jobId, message);
        }
      });
    }

    this.subscribers.get(jobId)!.add(ws);

    // Send confirmation
    ws.send(JSON.stringify({
      type: 'subscribed',
      jobId,
      message: `Subscribed to progress updates for job ${jobId}`,
    }));
  }

  /**
   * Unsubscribe client from job progress updates
   */
  private unsubscribe(ws: WebSocket, jobId: string): void {
    const subs = this.subscribers.get(jobId);
    if (subs) {
      subs.delete(ws);

      // If no more subscribers, unsubscribe from Redis
      if (subs.size === 0) {
        this.subscribers.delete(jobId);
        const channel = `videoagent:progress:${jobId}`;
        this.redis.unsubscribe(channel);
      }
    }

    ws.send(JSON.stringify({
      type: 'unsubscribed',
      jobId,
      message: `Unsubscribed from job ${jobId}`,
    }));
  }

  /**
   * Unsubscribe client from all jobs
   */
  private unsubscribeAll(ws: WebSocket): void {
    for (const [jobId, subs] of this.subscribers.entries()) {
      if (subs.has(ws)) {
        this.unsubscribe(ws, jobId);
      }
    }
  }

  /**
   * Broadcast progress update to all subscribers of a job
   */
  private broadcastToJob(jobId: string, message: string): void {
    const subs = this.subscribers.get(jobId);
    if (!subs) {
      return;
    }

    // Parse and validate progress update
    let progressUpdate: ProgressUpdate;
    try {
      progressUpdate = JSON.parse(message);
    } catch (error) {
      console.error('Invalid progress update:', error);
      return;
    }

    // Broadcast to all subscribers
    const payload = JSON.stringify({
      type: 'progress',
      data: progressUpdate,
    });

    for (const ws of subs) {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(payload);
      }
    }
  }

  /**
   * Get connection statistics
   */
  getStats(): {
    connectedClients: number;
    activeSubscriptions: number;
    jobsTracked: number;
  } {
    let totalSubs = 0;
    for (const subs of this.subscribers.values()) {
      totalSubs += subs.size;
    }

    return {
      connectedClients: this.wss.clients.size,
      activeSubscriptions: totalSubs,
      jobsTracked: this.subscribers.size,
    };
  }

  /**
   * Close WebSocket server and Redis connections
   */
  async close(): Promise<void> {
    // Close all WebSocket connections
    for (const client of this.wss.clients) {
      client.close();
    }

    // Close WebSocket server
    await new Promise<void>((resolve, reject) => {
      this.wss.close((err) => {
        if (err) reject(err);
        else resolve();
      });
    });

    // Close Redis connection
    await this.redis.quit();
  }
}

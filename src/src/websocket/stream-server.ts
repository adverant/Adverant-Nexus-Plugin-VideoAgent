/**
 * WebSocket Stream Server for Real-Time Video Processing
 * Handles WebSocket connections for live video frame streaming
 */

import WebSocket, { WebSocketServer } from 'ws';
import { IncomingMessage } from 'http';
import { RedisStreamManager } from '../streams/redis-stream-manager';
import { createLogger } from '../utils/logger';
import { JWTValidator } from '../utils/jwt-validator';

const logger = createLogger('StreamServer');

export interface StreamFrame {
  type: 'frame';
  timestamp: number;
  frameNumber: number;
  data: string; // base64-encoded frame
  metadata: {
    width: number;
    height: number;
    format: string;
  };
}

export interface StreamResult {
  type: 'partial' | 'refined' | 'final';
  frameNumber: number;
  timestamp: number;
  detections?: any[];
  scene?: {
    type: string;
    confidence: number;
  };
  objects?: any[];
  latency: number;
  error?: string;
}

export interface ClientConnection {
  id: string;
  ws: WebSocket;
  userId: string;
  sessionId: string;
  streamId: string;
  authenticated: boolean;
  connectedAt: Date;
  lastActivity: Date;
  frameCount: number;
  bytesReceived: number;
  bytesSent: number;
}

export class StreamServer {
  private wss: WebSocketServer;
  private clients: Map<string, ClientConnection>;
  private redisManager: RedisStreamManager;
  private heartbeatInterval: NodeJS.Timeout | null;
  private jwtValidator: JWTValidator;

  constructor(
    private port: number = 8081,
    private redisUrl: string = 'redis://localhost:6379'
  ) {
    this.clients = new Map();
    this.redisManager = new RedisStreamManager(redisUrl);
    this.heartbeatInterval = null;
    this.jwtValidator = new JWTValidator();
  }

  /**
   * Initialize WebSocket server
   */
  async initialize(): Promise<void> {
    logger.info(`Initializing WebSocket server on port ${this.port}`);

    // Initialize Redis Streams manager
    await this.redisManager.initialize();

    // Create WebSocket server
    this.wss = new WebSocketServer({
      port: this.port,
      maxPayload: 10 * 1024 * 1024, // 10MB max frame size
      perMessageDeflate: true // Enable compression
    });

    // Set up connection handler
    this.wss.on('connection', this.handleConnection.bind(this));

    // Start heartbeat to detect dead connections
    this.startHeartbeat();

    logger.info(`WebSocket server listening on port ${this.port}`);
  }

  /**
   * Handle new WebSocket connection
   * CRITICAL: JWT validation implemented - unauthorized connections rejected
   */
  private handleConnection(ws: WebSocket, req: IncomingMessage): void {
    const clientId = this.generateClientId();
    const url = new URL(req.url || '', `http://${req.headers.host}`);

    // Extract JWT token from Authorization header or query parameter
    const authHeader = req.headers.authorization;
    const queryToken = url.searchParams.get('token');

    let token = this.jwtValidator.extractFromHeader(authHeader);
    if (!token) {
      token = this.jwtValidator.extractFromQuery(queryToken);
    }

    // CRITICAL: Validate JWT token - reject if missing or invalid
    if (!token) {
      logger.warn(`Connection rejected: Missing JWT token (clientId: ${clientId})`);
      ws.close(1008, 'Unauthorized: Missing authentication token');
      return;
    }

    let claims;
    try {
      claims = this.jwtValidator.validateToken(token);
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Invalid token';
      logger.warn(`Connection rejected: ${errorMessage} (clientId: ${clientId})`);
      ws.close(1008, `Unauthorized: ${errorMessage}`);
      return;
    }

    // Extract user context from validated JWT claims
    const userContext = this.jwtValidator.extractUserContext(claims);
    const sessionId = url.searchParams.get('sessionId') || this.generateSessionId();

    logger.info(`New WebSocket connection: ${clientId} (user: ${userContext.userId}, email: ${userContext.email})`);

    // Create client connection object with authenticated user context
    const client: ClientConnection = {
      id: clientId,
      ws,
      userId: userContext.userId,
      sessionId,
      streamId: `stream_${clientId}`,
      authenticated: true, // Pre-authenticated via JWT
      connectedAt: new Date(),
      lastActivity: new Date(),
      frameCount: 0,
      bytesReceived: 0,
      bytesSent: 0
    };

    this.clients.set(clientId, client);

    // Set up event handlers
    ws.on('message', (data: Buffer) => this.handleMessage(clientId, data));
    ws.on('close', () => this.handleClose(clientId));
    ws.on('error', (error) => this.handleError(clientId, error));
    ws.on('pong', () => this.handlePong(clientId));

    // Send welcome message with authentication confirmation
    this.sendMessage(clientId, {
      type: 'connected',
      clientId,
      sessionId,
      authenticated: true,
      user: {
        userId: userContext.userId,
        email: userContext.email,
        tier: userContext.subscriptionTier
      },
      timestamp: Date.now()
    });

    logger.info(`Client ${clientId} authenticated successfully as ${userContext.email}`);
  }

  /**
   * Handle incoming message from client
   */
  private async handleMessage(clientId: string, data: Buffer): Promise<void> {
    const client = this.clients.get(clientId);
    if (!client) return;

    try {
      client.lastActivity = new Date();
      client.bytesReceived += data.length;

      const message = JSON.parse(data.toString());

      // Handle different message types
      switch (message.type) {
        case 'auth':
          await this.handleAuth(clientId, message);
          break;

        case 'frame':
          await this.handleFrame(clientId, message as StreamFrame);
          break;

        case 'ping':
          this.sendMessage(clientId, { type: 'pong', timestamp: Date.now() });
          break;

        default:
          logger.warn(`Unknown message type: ${message.type}`);
      }
    } catch (error) {
      logger.error(`Error handling message from ${clientId}:`, error);
      this.sendError(clientId, 'Failed to process message');
    }
  }

  /**
   * Handle authentication
   * CRITICAL: JWT validation implemented - validates token in auth message
   */
  private async handleAuth(clientId: string, message: any): Promise<void> {
    const client = this.clients.get(clientId);
    if (!client) return;

    // Extract token from auth message
    const token = message.token;

    if (!token) {
      logger.warn(`Authentication failed for ${clientId}: Missing token`);
      this.sendMessage(clientId, {
        type: 'authenticated',
        success: false,
        error: 'Missing authentication token',
        timestamp: Date.now()
      });
      // Close connection after failed auth
      client.ws.close(1008, 'Unauthorized: Missing authentication token');
      return;
    }

    // Validate JWT token
    try {
      const claims = this.jwtValidator.validateToken(token);
      const userContext = this.jwtValidator.extractUserContext(claims);

      // Update client with authenticated user context
      client.authenticated = true;
      client.userId = userContext.userId;

      this.sendMessage(clientId, {
        type: 'authenticated',
        success: true,
        user: {
          userId: userContext.userId,
          email: userContext.email,
          tier: userContext.subscriptionTier
        },
        timestamp: Date.now()
      });

      logger.info(`Client ${clientId} authenticated successfully as ${userContext.email}`);
    } catch (error) {
      const errorMessage = error instanceof Error ? error.message : 'Invalid token';
      logger.warn(`Authentication failed for ${clientId}: ${errorMessage}`);

      this.sendMessage(clientId, {
        type: 'authenticated',
        success: false,
        error: errorMessage,
        timestamp: Date.now()
      });

      // Close connection after failed auth
      client.ws.close(1008, `Unauthorized: ${errorMessage}`);
    }
  }

  /**
   * Handle incoming video frame
   */
  private async handleFrame(clientId: string, frame: StreamFrame): Promise<void> {
    const client = this.clients.get(clientId);
    if (!client) return;

    if (!client.authenticated) {
      this.sendError(clientId, 'Not authenticated');
      return;
    }

    client.frameCount++;

    const startTime = Date.now();

    try {
      // Send frame to Redis Streams for processing
      await this.redisManager.publishFrame(client.streamId, {
        clientId,
        sessionId: client.sessionId,
        userId: client.userId,
        frame,
        receivedAt: startTime
      });

      // Send acknowledgment to client
      this.sendMessage(clientId, {
        type: 'ack',
        frameNumber: frame.frameNumber,
        timestamp: Date.now(),
        queuePosition: client.frameCount
      });

      // Subscribe to results for this stream (if not already subscribed)
      await this.subscribeToResults(clientId, client.streamId);

    } catch (error) {
      logger.error(`Error processing frame from ${clientId}:`, error);
      this.sendError(clientId, 'Failed to process frame');
    }
  }

  /**
   * Subscribe to processing results for a stream
   */
  private async subscribeToResults(clientId: string, streamId: string): Promise<void> {
    const client = this.clients.get(clientId);
    if (!client) return;

    // Check if already subscribed
    if ((client as any).subscribed) return;
    (client as any).subscribed = true;

    // Start listening for results
    this.redisManager.subscribeToResults(streamId, (result: StreamResult) => {
      this.sendResult(clientId, result);
    });
  }

  /**
   * Send processing result to client
   */
  private sendResult(clientId: string, result: StreamResult): void {
    const client = this.clients.get(clientId);
    if (!client) return;

    this.sendMessage(clientId, result);
  }

  /**
   * Send message to client
   */
  private sendMessage(clientId: string, message: any): void {
    const client = this.clients.get(clientId);
    if (!client || client.ws.readyState !== WebSocket.OPEN) return;

    try {
      const data = JSON.stringify(message);
      client.ws.send(data);
      client.bytesSent += data.length;
    } catch (error) {
      logger.error(`Error sending message to ${clientId}:`, error);
    }
  }

  /**
   * Send error message to client
   */
  private sendError(clientId: string, error: string): void {
    this.sendMessage(clientId, {
      type: 'error',
      error,
      timestamp: Date.now()
    });
  }

  /**
   * Handle connection close
   */
  private handleClose(clientId: string): void {
    const client = this.clients.get(clientId);
    if (!client) return;

    logger.info(`Client ${clientId} disconnected`);

    // Cleanup
    this.redisManager.unsubscribeFromResults(client.streamId);
    this.clients.delete(clientId);

    // Log statistics
    const duration = Date.now() - client.connectedAt.getTime();
    logger.info(`Client ${clientId} stats: ${client.frameCount} frames, ${duration}ms duration`);
  }

  /**
   * Handle WebSocket error
   */
  private handleError(clientId: string, error: Error): void {
    logger.error(`WebSocket error for ${clientId}:`, error);
  }

  /**
   * Handle pong (heartbeat response)
   */
  private handlePong(clientId: string): void {
    const client = this.clients.get(clientId);
    if (client) {
      client.lastActivity = new Date();
    }
  }

  /**
   * Start heartbeat to detect dead connections
   */
  private startHeartbeat(): void {
    this.heartbeatInterval = setInterval(() => {
      const now = Date.now();
      const timeout = 30000; // 30 seconds

      this.clients.forEach((client, clientId) => {
        const inactive = now - client.lastActivity.getTime();

        if (inactive > timeout) {
          logger.warn(`Client ${clientId} inactive for ${inactive}ms, terminating`);
          client.ws.terminate();
          this.clients.delete(clientId);
        } else if (client.ws.readyState === WebSocket.OPEN) {
          // Send ping
          client.ws.ping();
        }
      });
    }, 15000); // Check every 15 seconds
  }

  /**
   * Authenticate client (deprecated - JWT validation now done at connection time)
   * This method is no longer needed as authentication is handled during handleConnection
   */
  private authenticateClient(clientId: string): void {
    // DEPRECATED: Authentication is now enforced at connection time via JWT validation
    // This method is kept for backward compatibility but does nothing
    // All connections are now pre-authenticated via JWT before being accepted
    logger.debug(`authenticateClient called for ${clientId} (deprecated - already authenticated via JWT)`);
  }

  /**
   * Generate unique client ID
   */
  private generateClientId(): string {
    return `client_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
  }

  /**
   * Generate unique session ID
   */
  private generateSessionId(): string {
    return `session_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`;
  }

  /**
   * Get server statistics
   */
  getStats() {
    return {
      connectedClients: this.clients.size,
      totalFramesReceived: Array.from(this.clients.values()).reduce((sum, c) => sum + c.frameCount, 0),
      totalBytesReceived: Array.from(this.clients.values()).reduce((sum, c) => sum + c.bytesReceived, 0),
      totalBytesSent: Array.from(this.clients.values()).reduce((sum, c) => sum + c.bytesSent, 0)
    };
  }

  /**
   * Shutdown server gracefully
   */
  async shutdown(): Promise<void> {
    logger.info('Shutting down WebSocket server');

    // Stop heartbeat
    if (this.heartbeatInterval) {
      clearInterval(this.heartbeatInterval);
    }

    // Close all client connections
    this.clients.forEach((client) => {
      client.ws.close(1000, 'Server shutting down');
    });

    // Close WebSocket server
    await new Promise<void>((resolve) => {
      this.wss.close(() => {
        logger.info('WebSocket server closed');
        resolve();
      });
    });

    // Shutdown Redis manager
    await this.redisManager.shutdown();
  }
}

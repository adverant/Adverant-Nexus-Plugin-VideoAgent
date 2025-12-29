/**
 * StreamClient - Browser-friendly WebSocket client for real-time video streaming
 *
 * Features:
 * - Automatic reconnection with exponential backoff
 * - Frame buffering to handle network jitter
 * - Adaptive quality based on bandwidth estimation
 * - Connection state management with event emitters
 * - Heartbeat monitoring
 *
 * Usage:
 * ```typescript
 * const client = new StreamClient('ws://localhost:8081', 'user123', 'session456');
 *
 * client.on('connected', () => console.log('Connected!'));
 * client.on('result', (result) => console.log('Result:', result));
 * client.on('error', (error) => console.error('Error:', error));
 *
 * await client.connect();
 * client.sendFrame({ type: 'video', data: frameData, ... });
 * ```
 */

export interface StreamFrame {
  type: 'video' | 'audio' | 'image';
  timestamp: number;
  frameNumber: number;
  data: string; // Base64 encoded
  metadata: {
    width: number;
    height: number;
    format: string;
    quality?: number;
  };
}

export interface StreamResult {
  streamId: string;
  frameNumber: number;
  timestamp: number;
  type: 'vision' | 'transcription' | 'classification';
  data: any;
  processingTime: number;
}

export interface StreamClientConfig {
  serverUrl: string;
  userId: string;
  sessionId?: string;
  maxReconnectAttempts?: number;
  reconnectDelay?: number;
  maxReconnectDelay?: number;
  bufferSize?: number;
  heartbeatInterval?: number;
  adaptiveQuality?: boolean;
}

export type ConnectionState = 'disconnected' | 'connecting' | 'connected' | 'reconnecting' | 'failed';

export type StreamClientEvent =
  | 'connected'
  | 'disconnected'
  | 'reconnecting'
  | 'failed'
  | 'result'
  | 'error'
  | 'stats';

type EventCallback = (data?: any) => void;

export interface ClientStats {
  framesBuffered: number;
  framesSent: number;
  resultsReceived: number;
  reconnectCount: number;
  currentLatency: number;
  averageLatency: number;
  estimatedBandwidth: number; // Bytes per second
}

export class StreamClient {
  private ws: WebSocket | null = null;
  private state: ConnectionState = 'disconnected';
  private reconnectAttempts = 0;
  private currentReconnectDelay: number;
  private frameBuffer: Array<StreamFrame> = [];
  private authenticated = false;
  private heartbeatTimer: number | null = null;
  private lastPongTime = 0;
  private eventHandlers: Map<StreamClientEvent, Set<EventCallback>> = new Map();

  // Statistics
  private stats: ClientStats = {
    framesBuffered: 0,
    framesSent: 0,
    resultsReceived: 0,
    reconnectCount: 0,
    currentLatency: 0,
    averageLatency: 0,
    estimatedBandwidth: 0
  };

  // Latency tracking
  private latencySamples: number[] = [];
  private maxLatencySamples = 100;

  // Bandwidth estimation
  private bandwidthSamples: Array<{ bytes: number; time: number }> = [];
  private maxBandwidthSamples = 20;
  private lastBandwidthCheck = 0;

  // Configuration
  private readonly serverUrl: string;
  private readonly userId: string;
  private readonly sessionId: string;
  private readonly maxReconnectAttempts: number;
  private readonly reconnectDelay: number;
  private readonly maxReconnectDelay: number;
  private readonly bufferSize: number;
  private readonly heartbeatInterval: number;
  private readonly adaptiveQuality: boolean;

  constructor(config: StreamClientConfig) {
    this.serverUrl = config.serverUrl;
    this.userId = config.userId;
    this.sessionId = config.sessionId || this.generateSessionId();
    this.maxReconnectAttempts = config.maxReconnectAttempts || 10;
    this.reconnectDelay = config.reconnectDelay || 1000; // 1 second
    this.maxReconnectDelay = config.maxReconnectDelay || 30000; // 30 seconds
    this.currentReconnectDelay = this.reconnectDelay;
    this.bufferSize = config.bufferSize || 100; // Max 100 frames buffered
    this.heartbeatInterval = config.heartbeatInterval || 15000; // 15 seconds
    this.adaptiveQuality = config.adaptiveQuality !== false; // Default true

    // Initialize event handler maps
    const events: StreamClientEvent[] = ['connected', 'disconnected', 'reconnecting', 'failed', 'result', 'error', 'stats'];
    events.forEach(event => this.eventHandlers.set(event, new Set()));
  }

  /**
   * Connect to WebSocket server with authentication
   */
  async connect(): Promise<void> {
    if (this.state === 'connected' || this.state === 'connecting') {
      console.warn('StreamClient: Already connected or connecting');
      return;
    }

    return new Promise((resolve, reject) => {
      try {
        this.state = 'connecting';
        this.ws = new WebSocket(this.serverUrl);

        // Connection opened
        this.ws.onopen = () => {
          console.log('StreamClient: WebSocket connection opened');
          this.authenticate()
            .then(() => {
              this.state = 'connected';
              this.authenticated = true;
              this.reconnectAttempts = 0;
              this.currentReconnectDelay = this.reconnectDelay;
              this.startHeartbeat();
              this.flushFrameBuffer();
              this.emit('connected');
              resolve();
            })
            .catch((err) => {
              console.error('StreamClient: Authentication failed:', err);
              this.disconnect();
              reject(err);
            });
        };

        // Message received
        this.ws.onmessage = (event) => {
          this.handleMessage(event.data);
        };

        // Connection closed
        this.ws.onclose = (event) => {
          console.log('StreamClient: WebSocket connection closed', event.code, event.reason);
          this.handleDisconnect();
        };

        // Connection error
        this.ws.onerror = (error) => {
          console.error('StreamClient: WebSocket error:', error);
          this.emit('error', { message: 'WebSocket connection error', error });

          if (this.state === 'connecting') {
            reject(new Error('WebSocket connection failed'));
          }
        };

        // Timeout for connection
        setTimeout(() => {
          if (this.state === 'connecting') {
            this.disconnect();
            reject(new Error('Connection timeout'));
          }
        }, 10000); // 10 second timeout

      } catch (error) {
        console.error('StreamClient: Failed to create WebSocket:', error);
        this.state = 'failed';
        reject(error);
      }
    });
  }

  /**
   * Authenticate with server
   */
  private async authenticate(): Promise<void> {
    return new Promise((resolve, reject) => {
      const authMessage = {
        type: 'auth',
        userId: this.userId,
        sessionId: this.sessionId,
        timestamp: Date.now()
      };

      // Wait for auth response
      const authHandler = (data: any) => {
        if (data.type === 'auth') {
          if (data.success) {
            resolve();
          } else {
            reject(new Error(data.error || 'Authentication failed'));
          }
        }
      };

      // Temporary one-time handler
      this.on('_auth', authHandler);

      // Send auth message
      this.send(authMessage);

      // Timeout
      setTimeout(() => {
        this.off('_auth', authHandler);
        reject(new Error('Authentication timeout'));
      }, 5000);
    });
  }

  /**
   * Send frame to server
   */
  sendFrame(frame: StreamFrame): void {
    if (!this.authenticated || this.state !== 'connected') {
      // Buffer frame if not connected
      if (this.frameBuffer.length < this.bufferSize) {
        this.frameBuffer.push(frame);
        this.stats.framesBuffered++;
      } else {
        console.warn('StreamClient: Frame buffer full, dropping frame', frame.frameNumber);
      }
      return;
    }

    // Apply adaptive quality if enabled
    const qualityFrame = this.adaptiveQuality ? this.applyAdaptiveQuality(frame) : frame;

    // Send frame
    const message = {
      type: 'frame',
      frame: qualityFrame,
      timestamp: Date.now()
    };

    this.send(message);
    this.stats.framesSent++;

    // Track bandwidth
    this.trackBandwidth(JSON.stringify(message).length);
  }

  /**
   * Apply adaptive quality based on bandwidth
   */
  private applyAdaptiveQuality(frame: StreamFrame): StreamFrame {
    const bandwidth = this.stats.estimatedBandwidth;

    // No adjustment if bandwidth unknown or high
    if (bandwidth === 0 || bandwidth > 1024 * 1024) { // > 1 MB/s
      return frame;
    }

    // Low bandwidth: reduce quality
    if (bandwidth < 256 * 1024) { // < 256 KB/s
      return {
        ...frame,
        metadata: {
          ...frame.metadata,
          quality: 50 // Low quality
        }
      };
    }

    // Medium bandwidth: medium quality
    if (bandwidth < 512 * 1024) { // < 512 KB/s
      return {
        ...frame,
        metadata: {
          ...frame.metadata,
          quality: 70 // Medium quality
        }
      };
    }

    // Default: high quality
    return {
      ...frame,
      metadata: {
        ...frame.metadata,
        quality: 90 // High quality
      }
    };
  }

  /**
   * Disconnect from server
   */
  disconnect(): void {
    this.stopHeartbeat();

    if (this.ws) {
      this.ws.close(1000, 'Client disconnecting');
      this.ws = null;
    }

    this.authenticated = false;
    this.state = 'disconnected';
    this.emit('disconnected');
  }

  /**
   * Handle incoming messages
   */
  private handleMessage(data: string): void {
    try {
      const message = JSON.parse(data);

      switch (message.type) {
        case 'auth':
          // Handled by authenticate() method
          this.emit('_auth' as any, message);
          break;

        case 'pong':
          // Update latency
          this.lastPongTime = Date.now();
          const latency = this.lastPongTime - message.timestamp;
          this.updateLatency(latency);
          break;

        case 'result':
          // Processing result received
          this.stats.resultsReceived++;
          const result: StreamResult = message.result;
          this.emit('result', result);
          break;

        case 'error':
          // Server error
          this.emit('error', { message: message.error, code: message.code });
          break;

        case 'stats':
          // Server statistics update
          this.emit('stats', message.stats);
          break;

        default:
          console.warn('StreamClient: Unknown message type:', message.type);
      }
    } catch (error) {
      console.error('StreamClient: Failed to parse message:', error);
      this.emit('error', { message: 'Failed to parse server message', error });
    }
  }

  /**
   * Handle disconnection
   */
  private handleDisconnect(): void {
    this.stopHeartbeat();
    this.authenticated = false;

    if (this.reconnectAttempts < this.maxReconnectAttempts) {
      this.reconnect();
    } else {
      this.state = 'failed';
      this.emit('failed', { message: 'Max reconnection attempts reached' });
    }
  }

  /**
   * Reconnect with exponential backoff
   */
  private reconnect(): void {
    this.state = 'reconnecting';
    this.reconnectAttempts++;
    this.stats.reconnectCount++;

    console.log(`StreamClient: Reconnecting (attempt ${this.reconnectAttempts}/${this.maxReconnectAttempts})...`);
    this.emit('reconnecting', { attempt: this.reconnectAttempts, maxAttempts: this.maxReconnectAttempts });

    setTimeout(() => {
      this.connect().catch((error) => {
        console.error('StreamClient: Reconnection failed:', error);
      });
    }, this.currentReconnectDelay);

    // Exponential backoff (capped at maxReconnectDelay)
    this.currentReconnectDelay = Math.min(
      this.currentReconnectDelay * 2,
      this.maxReconnectDelay
    );
  }

  /**
   * Start heartbeat mechanism
   */
  private startHeartbeat(): void {
    this.stopHeartbeat();

    this.heartbeatTimer = window.setInterval(() => {
      if (this.state === 'connected' && this.authenticated) {
        const pingMessage = {
          type: 'ping',
          timestamp: Date.now()
        };
        this.send(pingMessage);

        // Check for stale connection (no pong in 30 seconds)
        if (this.lastPongTime > 0 && Date.now() - this.lastPongTime > 30000) {
          console.warn('StreamClient: Connection appears stale, reconnecting...');
          this.disconnect();
          this.reconnect();
        }
      }
    }, this.heartbeatInterval);
  }

  /**
   * Stop heartbeat mechanism
   */
  private stopHeartbeat(): void {
    if (this.heartbeatTimer) {
      clearInterval(this.heartbeatTimer);
      this.heartbeatTimer = null;
    }
  }

  /**
   * Flush frame buffer after reconnection
   */
  private flushFrameBuffer(): void {
    if (this.frameBuffer.length === 0) {
      return;
    }

    console.log(`StreamClient: Flushing ${this.frameBuffer.length} buffered frames`);

    // Send buffered frames
    while (this.frameBuffer.length > 0) {
      const frame = this.frameBuffer.shift();
      if (frame) {
        this.sendFrame(frame);
      }
    }

    this.stats.framesBuffered = 0;
  }

  /**
   * Send message to server
   */
  private send(message: any): void {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      console.warn('StreamClient: Cannot send message, WebSocket not open');
      return;
    }

    try {
      this.ws.send(JSON.stringify(message));
    } catch (error) {
      console.error('StreamClient: Failed to send message:', error);
      this.emit('error', { message: 'Failed to send message', error });
    }
  }

  /**
   * Update latency statistics
   */
  private updateLatency(latency: number): void {
    this.stats.currentLatency = latency;

    this.latencySamples.push(latency);
    if (this.latencySamples.length > this.maxLatencySamples) {
      this.latencySamples.shift();
    }

    // Calculate average latency
    const sum = this.latencySamples.reduce((a, b) => a + b, 0);
    this.stats.averageLatency = sum / this.latencySamples.length;
  }

  /**
   * Track bandwidth usage
   */
  private trackBandwidth(bytes: number): void {
    const now = Date.now();

    this.bandwidthSamples.push({ bytes, time: now });

    // Remove samples older than 10 seconds
    this.bandwidthSamples = this.bandwidthSamples.filter(
      sample => now - sample.time < 10000
    );

    // Keep only recent samples
    if (this.bandwidthSamples.length > this.maxBandwidthSamples) {
      this.bandwidthSamples.shift();
    }

    // Calculate bandwidth (bytes per second)
    if (this.bandwidthSamples.length >= 2) {
      const firstSample = this.bandwidthSamples[0];
      const lastSample = this.bandwidthSamples[this.bandwidthSamples.length - 1];
      const timeDiff = (lastSample.time - firstSample.time) / 1000; // Convert to seconds

      if (timeDiff > 0) {
        const totalBytes = this.bandwidthSamples.reduce((sum, sample) => sum + sample.bytes, 0);
        this.stats.estimatedBandwidth = totalBytes / timeDiff;
      }
    }
  }

  /**
   * Register event handler
   */
  on(event: StreamClientEvent | string, callback: EventCallback): void {
    if (!this.eventHandlers.has(event as StreamClientEvent)) {
      this.eventHandlers.set(event as StreamClientEvent, new Set());
    }
    this.eventHandlers.get(event as StreamClientEvent)!.add(callback);
  }

  /**
   * Unregister event handler
   */
  off(event: StreamClientEvent | string, callback: EventCallback): void {
    const handlers = this.eventHandlers.get(event as StreamClientEvent);
    if (handlers) {
      handlers.delete(callback);
    }
  }

  /**
   * Emit event
   */
  private emit(event: StreamClientEvent | string, data?: any): void {
    const handlers = this.eventHandlers.get(event as StreamClientEvent);
    if (handlers) {
      handlers.forEach(callback => {
        try {
          callback(data);
        } catch (error) {
          console.error(`StreamClient: Error in ${event} handler:`, error);
        }
      });
    }
  }

  /**
   * Get current connection state
   */
  getState(): ConnectionState {
    return this.state;
  }

  /**
   * Get client statistics
   */
  getStats(): ClientStats {
    return { ...this.stats };
  }

  /**
   * Check if client is connected
   */
  isConnected(): boolean {
    return this.state === 'connected' && this.authenticated;
  }

  /**
   * Generate unique session ID
   */
  private generateSessionId(): string {
    return `session-${Date.now()}-${Math.random().toString(36).substring(2, 11)}`;
  }
}

/**
 * BufferManager - Manages frame buffering for smooth streaming and jitter handling
 *
 * Features:
 * - Circular buffer for incoming frames
 * - Jitter absorption (handle network delays)
 * - Overflow/underflow detection
 * - Buffer health monitoring
 * - Automatic buffer size adjustment
 * - Statistics tracking
 *
 * Buffer States:
 * - Healthy: 30-70% full
 * - Underflow: <30% full (network issues or slow processing)
 * - Overflow: >70% full (processing can't keep up)
 * - Critical: <10% or >90% (requires immediate action)
 */

export interface BufferedFrame<T = any> {
  id: string;
  frame: T;
  timestamp: number;
  size: number; // Bytes
  priority: number; // 0-1
  addedAt: number;
}

export interface BufferConfig {
  maxSize?: number; // Max frames (default: 100)
  minSize?: number; // Min frames before consuming (default: 10)
  targetSize?: number; // Target buffer size (default: 50)
  maxWaitTime?: number; // Max time in buffer (ms, default: 5000)
  overflowStrategy?: OverflowStrategy; // Default: 'drop-oldest'
  underflowStrategy?: UnderflowStrategy; // Default: 'wait'
  enableAutoResize?: boolean; // Auto-adjust buffer size (default: true)
}

export type OverflowStrategy = 'drop-oldest' | 'drop-newest' | 'drop-lowest-priority';
export type UnderflowStrategy = 'wait' | 'repeat-last' | 'skip';
export type BufferHealth = 'healthy' | 'underflow' | 'overflow' | 'critical-low' | 'critical-high';

export class BufferManager<T = any> {
  private buffer: BufferedFrame<T>[] = [];
  private maxSize: number;
  private minSize: number;
  private targetSize: number;
  private maxWaitTime: number;
  private overflowStrategy: OverflowStrategy;
  private underflowStrategy: UnderflowStrategy;
  private enableAutoResize: boolean;

  // State
  private lastConsumedFrame: BufferedFrame<T> | null = null;
  private bufferHealth: BufferHealth = 'healthy';

  // Statistics
  private stats = {
    totalAdded: 0,
    totalConsumed: 0,
    totalDropped: 0,
    overflowDrops: 0,
    timeoutDrops: 0,
    averageBufferSize: 0,
    currentBufferSize: 0,
    peakBufferSize: 0,
    underflowCount: 0,
    overflowCount: 0,
    averageWaitTime: 0, // Milliseconds
  };

  constructor(config: BufferConfig = {}) {
    this.maxSize = config.maxSize || 100;
    this.minSize = config.minSize || 10;
    this.targetSize = config.targetSize || 50;
    this.maxWaitTime = config.maxWaitTime || 5000;
    this.overflowStrategy = config.overflowStrategy || 'drop-oldest';
    this.underflowStrategy = config.underflowStrategy || 'wait';
    this.enableAutoResize = config.enableAutoResize !== false;
  }

  /**
   * Add frame to buffer
   */
  addFrame(id: string, frame: T, size: number, priority: number = 0.5): boolean {
    const bufferedFrame: BufferedFrame<T> = {
      id,
      frame,
      timestamp: Date.now(),
      size,
      priority,
      addedAt: Date.now(),
    };

    // Check for overflow
    if (this.buffer.length >= this.maxSize) {
      this.handleOverflow(bufferedFrame);
      return false; // Frame not added
    }

    // Add to buffer
    this.buffer.push(bufferedFrame);
    this.stats.totalAdded++;
    this.stats.currentBufferSize = this.buffer.length;

    // Update peak
    if (this.buffer.length > this.stats.peakBufferSize) {
      this.stats.peakBufferSize = this.buffer.length;
    }

    // Update buffer health
    this.updateBufferHealth();

    // Clean expired frames
    this.cleanExpiredFrames();

    // Auto-resize if enabled
    if (this.enableAutoResize) {
      this.autoResizeBuffer();
    }

    return true;
  }

  /**
   * Consume next frame from buffer
   */
  consumeFrame(): BufferedFrame<T> | null {
    // Handle underflow
    if (this.buffer.length < this.minSize) {
      return this.handleUnderflow();
    }

    // Get next frame
    const frame = this.buffer.shift();

    if (frame) {
      this.lastConsumedFrame = frame;
      this.stats.totalConsumed++;
      this.stats.currentBufferSize = this.buffer.length;

      // Update wait time statistics
      const waitTime = Date.now() - frame.addedAt;
      this.updateAverageWaitTime(waitTime);

      // Update buffer health
      this.updateBufferHealth();
    }

    return frame || null;
  }

  /**
   * Peek at next frame without consuming
   */
  peekFrame(): BufferedFrame<T> | null {
    return this.buffer[0] || null;
  }

  /**
   * Handle buffer overflow
   */
  private handleOverflow(newFrame: BufferedFrame<T>): void {
    this.stats.overflowCount++;
    this.stats.overflowDrops++;

    switch (this.overflowStrategy) {
      case 'drop-oldest':
        // Drop oldest frame (FIFO)
        const dropped = this.buffer.shift();
        if (dropped) {
          this.stats.totalDropped++;
          console.log(`BufferManager: Dropped oldest frame (overflow) - ID: ${dropped.id}`);
        }
        // Add new frame
        this.buffer.push(newFrame);
        this.stats.totalAdded++;
        break;

      case 'drop-newest':
        // Drop the new frame
        this.stats.totalDropped++;
        console.log(`BufferManager: Dropped newest frame (overflow) - ID: ${newFrame.id}`);
        break;

      case 'drop-lowest-priority':
        // Find and drop lowest priority frame
        const lowestIndex = this.findLowestPriorityFrame();
        if (lowestIndex >= 0) {
          const droppedFrame = this.buffer.splice(lowestIndex, 1)[0];
          this.stats.totalDropped++;
          console.log(
            `BufferManager: Dropped low priority frame (overflow) - ID: ${droppedFrame.id}, Priority: ${droppedFrame.priority}`
          );
          // Add new frame
          this.buffer.push(newFrame);
          this.stats.totalAdded++;
        } else {
          // If all frames have same priority, drop oldest
          this.stats.totalDropped++;
          console.log(`BufferManager: Dropped oldest frame (overflow) - ID: ${newFrame.id}`);
        }
        break;
    }

    this.stats.currentBufferSize = this.buffer.length;
  }

  /**
   * Handle buffer underflow
   */
  private handleUnderflow(): BufferedFrame<T> | null {
    this.stats.underflowCount++;

    switch (this.underflowStrategy) {
      case 'wait':
        // Return null and wait for more frames
        console.log('BufferManager: Underflow - waiting for more frames');
        return null;

      case 'repeat-last':
        // Return last consumed frame again
        if (this.lastConsumedFrame) {
          console.log(
            `BufferManager: Underflow - repeating last frame (ID: ${this.lastConsumedFrame.id})`
          );
          return { ...this.lastConsumedFrame, timestamp: Date.now() };
        }
        return null;

      case 'skip':
        // Skip and return whatever is available
        if (this.buffer.length > 0) {
          console.log('BufferManager: Underflow - consuming available frame despite min size');
          return this.buffer.shift() || null;
        }
        return null;

      default:
        return null;
    }
  }

  /**
   * Clean expired frames from buffer
   */
  private cleanExpiredFrames(): void {
    const now = Date.now();
    let droppedCount = 0;

    this.buffer = this.buffer.filter((frame) => {
      const age = now - frame.addedAt;
      if (age > this.maxWaitTime) {
        droppedCount++;
        this.stats.totalDropped++;
        this.stats.timeoutDrops++;
        return false;
      }
      return true;
    });

    if (droppedCount > 0) {
      console.log(`BufferManager: Dropped ${droppedCount} expired frames (timeout: ${this.maxWaitTime}ms)`);
      this.stats.currentBufferSize = this.buffer.length;
    }
  }

  /**
   * Find lowest priority frame in buffer
   */
  private findLowestPriorityFrame(): number {
    if (this.buffer.length === 0) {
      return -1;
    }

    let lowestIndex = 0;
    let lowestPriority = this.buffer[0].priority;

    for (let i = 1; i < this.buffer.length; i++) {
      if (this.buffer[i].priority < lowestPriority) {
        lowestIndex = i;
        lowestPriority = this.buffer[i].priority;
      }
    }

    return lowestIndex;
  }

  /**
   * Update buffer health status
   */
  private updateBufferHealth(): void {
    const fillPercent = (this.buffer.length / this.maxSize) * 100;

    if (fillPercent < 10) {
      this.bufferHealth = 'critical-low';
    } else if (fillPercent < 30) {
      this.bufferHealth = 'underflow';
    } else if (fillPercent > 90) {
      this.bufferHealth = 'critical-high';
    } else if (fillPercent > 70) {
      this.bufferHealth = 'overflow';
    } else {
      this.bufferHealth = 'healthy';
    }
  }

  /**
   * Auto-resize buffer based on usage patterns
   */
  private autoResizeBuffer(): void {
    // Only resize if we have enough data
    if (this.stats.totalAdded < 100) {
      return;
    }

    // Calculate average buffer size
    this.stats.averageBufferSize =
      (this.stats.totalAdded - this.stats.totalConsumed) / this.stats.totalAdded;

    // If consistently near max, increase buffer size
    if (this.stats.averageBufferSize > this.maxSize * 0.8) {
      const newMaxSize = Math.min(this.maxSize * 1.5, 500); // Cap at 500
      if (newMaxSize > this.maxSize) {
        console.log(`BufferManager: Auto-resize - increasing max size from ${this.maxSize} to ${newMaxSize}`);
        this.maxSize = newMaxSize;
        this.targetSize = Math.floor(this.maxSize * 0.5);
      }
    }

    // If consistently low, decrease buffer size
    if (this.stats.averageBufferSize < this.maxSize * 0.2 && this.maxSize > 50) {
      const newMaxSize = Math.max(this.maxSize * 0.8, 50); // Minimum 50
      if (newMaxSize < this.maxSize) {
        console.log(`BufferManager: Auto-resize - decreasing max size from ${this.maxSize} to ${newMaxSize}`);
        this.maxSize = newMaxSize;
        this.targetSize = Math.floor(this.maxSize * 0.5);
      }
    }
  }

  /**
   * Update average wait time
   */
  private updateAverageWaitTime(waitTime: number): void {
    const total = this.stats.totalConsumed;
    this.stats.averageWaitTime =
      (this.stats.averageWaitTime * (total - 1) + waitTime) / total;
  }

  /**
   * Get buffer health status
   */
  getHealth(): BufferHealth {
    return this.bufferHealth;
  }

  /**
   * Get buffer fill percentage
   */
  getFillPercentage(): number {
    return (this.buffer.length / this.maxSize) * 100;
  }

  /**
   * Check if buffer is ready for consumption
   */
  isReady(): boolean {
    return this.buffer.length >= this.minSize;
  }

  /**
   * Get current buffer size
   */
  getSize(): number {
    return this.buffer.length;
  }

  /**
   * Get buffer capacity
   */
  getCapacity(): number {
    return this.maxSize;
  }

  /**
   * Clear all frames from buffer
   */
  clear(): void {
    const droppedCount = this.buffer.length;
    this.buffer = [];
    this.stats.currentBufferSize = 0;
    this.stats.totalDropped += droppedCount;
    console.log(`BufferManager: Cleared buffer (dropped ${droppedCount} frames)`);
  }

  /**
   * Get current statistics
   */
  getStats() {
    return {
      ...this.stats,
      health: this.bufferHealth,
      fillPercentage: this.getFillPercentage(),
      isReady: this.isReady(),
      maxSize: this.maxSize,
      minSize: this.minSize,
      targetSize: this.targetSize,
    };
  }

  /**
   * Reset buffer and statistics
   */
  reset(): void {
    this.buffer = [];
    this.lastConsumedFrame = null;
    this.bufferHealth = 'healthy';
    this.stats = {
      totalAdded: 0,
      totalConsumed: 0,
      totalDropped: 0,
      overflowDrops: 0,
      timeoutDrops: 0,
      averageBufferSize: 0,
      currentBufferSize: 0,
      peakBufferSize: 0,
      underflowCount: 0,
      overflowCount: 0,
      averageWaitTime: 0,
    };

    console.log('BufferManager: Reset');
  }

  /**
   * Set overflow strategy
   */
  setOverflowStrategy(strategy: OverflowStrategy): void {
    this.overflowStrategy = strategy;
    console.log(`BufferManager: Overflow strategy set to ${strategy}`);
  }

  /**
   * Set underflow strategy
   */
  setUnderflowStrategy(strategy: UnderflowStrategy): void {
    this.underflowStrategy = strategy;
    console.log(`BufferManager: Underflow strategy set to ${strategy}`);
  }

  /**
   * Set max buffer size
   */
  setMaxSize(size: number): void {
    this.maxSize = Math.max(this.minSize, size);
    this.targetSize = Math.floor(this.maxSize * 0.5);
    console.log(`BufferManager: Max size set to ${this.maxSize}`);
  }

  /**
   * Enable or disable auto-resize
   */
  setAutoResize(enabled: boolean): void {
    this.enableAutoResize = enabled;
    console.log(`BufferManager: Auto-resize ${enabled ? 'enabled' : 'disabled'}`);
  }
}

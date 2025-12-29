/**
 * FrameSkipper - Intelligent frame dropping based on network conditions and processing capacity
 *
 * Features:
 * - Dynamic frame rate adjustment
 * - Priority-based frame selection
 * - Scene change detection (keep important frames)
 * - Motion-based dropping (drop similar frames)
 * - Processing capacity monitoring
 * - Statistics tracking
 *
 * Drop Strategies:
 * - Uniform: Drop frames uniformly (e.g., keep every Nth frame)
 * - Motion: Drop frames with low motion (similar to previous)
 * - Priority: Drop low-priority frames (based on content)
 * - Adaptive: Combine strategies based on conditions
 */

export interface FrameMetadata {
  frameNumber: number;
  timestamp: number;
  size: number; // Bytes
  motion?: number; // Motion score (0-1)
  priority?: number; // Priority score (0-1)
  isKeyFrame?: boolean;
  sceneChange?: boolean;
}

export interface SkipConfig {
  enabled?: boolean; // Default: true
  strategy?: SkipStrategy; // Default: 'adaptive'
  targetFrameRate?: number; // Target fps (e.g., 15)
  minFrameRate?: number; // Minimum fps (e.g., 1)
  maxFrameRate?: number; // Maximum fps (e.g., 30)
  motionThreshold?: number; // Motion threshold for dropping (0-1)
  priorityThreshold?: number; // Priority threshold (0-1)
  sceneChangeProtection?: boolean; // Always keep scene changes
  keyFrameProtection?: boolean; // Always keep key frames
}

export type SkipStrategy = 'uniform' | 'motion' | 'priority' | 'adaptive';

export interface SkipDecision {
  shouldSkip: boolean;
  reason: string;
  confidence: number; // 0-1
}

export class FrameSkipper {
  private enabled: boolean;
  private strategy: SkipStrategy;
  private targetFrameRate: number;
  private minFrameRate: number;
  private maxFrameRate: number;
  private motionThreshold: number;
  private priorityThreshold: number;
  private sceneChangeProtection: boolean;
  private keyFrameProtection: boolean;

  // State
  private lastKeptFrame: FrameMetadata | null = null;
  private frameHistory: FrameMetadata[] = [];
  private currentFrameRate = 0;
  private skipCounter = 0;
  private uniformSkipInterval = 1;

  // Statistics
  private stats = {
    totalFrames: 0,
    keptFrames: 0,
    skippedFrames: 0,
    skipRatePercent: 0,
    averageFrameRate: 0,
    sceneChangesKept: 0,
    keyFramesKept: 0,
    motionDrops: 0,
    priorityDrops: 0,
    uniformDrops: 0,
  };

  constructor(config: SkipConfig = {}) {
    this.enabled = config.enabled !== false;
    this.strategy = config.strategy || 'adaptive';
    this.targetFrameRate = config.targetFrameRate || 15;
    this.minFrameRate = config.minFrameRate || 1;
    this.maxFrameRate = config.maxFrameRate || 30;
    this.motionThreshold = config.motionThreshold || 0.1;
    this.priorityThreshold = config.priorityThreshold || 0.3;
    this.sceneChangeProtection = config.sceneChangeProtection !== false;
    this.keyFrameProtection = config.keyFrameProtection !== false;

    // Calculate initial skip interval for uniform strategy
    this.updateUniformSkipInterval();
  }

  /**
   * Decide whether to skip a frame
   */
  shouldSkipFrame(frame: FrameMetadata): SkipDecision {
    this.stats.totalFrames++;

    // Never skip if disabled
    if (!this.enabled) {
      this.keepFrame(frame);
      return {
        shouldSkip: false,
        reason: 'Frame skipper disabled',
        confidence: 1.0,
      };
    }

    // Always keep scene changes if protected
    if (this.sceneChangeProtection && frame.sceneChange) {
      this.keepFrame(frame);
      this.stats.sceneChangesKept++;
      return {
        shouldSkip: false,
        reason: 'Scene change detected (protected)',
        confidence: 1.0,
      };
    }

    // Always keep key frames if protected
    if (this.keyFrameProtection && frame.isKeyFrame) {
      this.keepFrame(frame);
      this.stats.keyFramesKept++;
      return {
        shouldSkip: false,
        reason: 'Key frame (protected)',
        confidence: 1.0,
      };
    }

    // Apply strategy-specific logic
    switch (this.strategy) {
      case 'uniform':
        return this.uniformSkip(frame);
      case 'motion':
        return this.motionBasedSkip(frame);
      case 'priority':
        return this.priorityBasedSkip(frame);
      case 'adaptive':
        return this.adaptiveSkip(frame);
      default:
        return this.uniformSkip(frame);
    }
  }

  /**
   * Uniform skip strategy - drop frames uniformly
   */
  private uniformSkip(frame: FrameMetadata): SkipDecision {
    this.skipCounter++;

    // Keep every Nth frame
    if (this.skipCounter >= this.uniformSkipInterval) {
      this.skipCounter = 0;
      this.keepFrame(frame);
      return {
        shouldSkip: false,
        reason: `Uniform skip (interval: ${this.uniformSkipInterval})`,
        confidence: 1.0,
      };
    }

    // Skip this frame
    this.skipFrame(frame);
    this.stats.uniformDrops++;
    return {
      shouldSkip: true,
      reason: 'Uniform skip',
      confidence: 1.0,
    };
  }

  /**
   * Motion-based skip strategy - drop low-motion frames
   */
  private motionBasedSkip(frame: FrameMetadata): SkipDecision {
    // If no motion data, fall back to uniform
    if (frame.motion === undefined) {
      return this.uniformSkip(frame);
    }

    // Keep high-motion frames
    if (frame.motion >= this.motionThreshold) {
      this.keepFrame(frame);
      return {
        shouldSkip: false,
        reason: `High motion (${frame.motion.toFixed(2)})`,
        confidence: frame.motion,
      };
    }

    // Skip low-motion frames (but check frame rate limits)
    if (this.canSkipBasedOnFrameRate()) {
      this.skipFrame(frame);
      this.stats.motionDrops++;
      return {
        shouldSkip: true,
        reason: `Low motion (${frame.motion.toFixed(2)})`,
        confidence: 1.0 - frame.motion,
      };
    }

    // Keep to maintain minimum frame rate
    this.keepFrame(frame);
    return {
      shouldSkip: false,
      reason: 'Minimum frame rate limit',
      confidence: 0.5,
    };
  }

  /**
   * Priority-based skip strategy - drop low-priority frames
   */
  private priorityBasedSkip(frame: FrameMetadata): SkipDecision {
    // If no priority data, fall back to uniform
    if (frame.priority === undefined) {
      return this.uniformSkip(frame);
    }

    // Keep high-priority frames
    if (frame.priority >= this.priorityThreshold) {
      this.keepFrame(frame);
      return {
        shouldSkip: false,
        reason: `High priority (${frame.priority.toFixed(2)})`,
        confidence: frame.priority,
      };
    }

    // Skip low-priority frames (but check frame rate limits)
    if (this.canSkipBasedOnFrameRate()) {
      this.skipFrame(frame);
      this.stats.priorityDrops++;
      return {
        shouldSkip: true,
        reason: `Low priority (${frame.priority.toFixed(2)})`,
        confidence: 1.0 - frame.priority,
      };
    }

    // Keep to maintain minimum frame rate
    this.keepFrame(frame);
    return {
      shouldSkip: false,
      reason: 'Minimum frame rate limit',
      confidence: 0.5,
    };
  }

  /**
   * Adaptive skip strategy - combine strategies based on conditions
   */
  private adaptiveSkip(frame: FrameMetadata): SkipDecision {
    // Calculate combined score from motion and priority
    const motionScore = frame.motion !== undefined ? frame.motion : 0.5;
    const priorityScore = frame.priority !== undefined ? frame.priority : 0.5;

    // Weighted combination (60% motion, 40% priority)
    const combinedScore = motionScore * 0.6 + priorityScore * 0.4;

    // Calculate dynamic threshold based on current frame rate
    const dynamicThreshold = this.calculateDynamicThreshold();

    // Keep frames above threshold
    if (combinedScore >= dynamicThreshold) {
      this.keepFrame(frame);
      return {
        shouldSkip: false,
        reason: `Adaptive keep (score: ${combinedScore.toFixed(2)}, threshold: ${dynamicThreshold.toFixed(2)})`,
        confidence: combinedScore,
      };
    }

    // Skip frames below threshold (but check frame rate limits)
    if (this.canSkipBasedOnFrameRate()) {
      this.skipFrame(frame);
      return {
        shouldSkip: true,
        reason: `Adaptive skip (score: ${combinedScore.toFixed(2)}, threshold: ${dynamicThreshold.toFixed(2)})`,
        confidence: 1.0 - combinedScore,
      };
    }

    // Keep to maintain minimum frame rate
    this.keepFrame(frame);
    return {
      shouldSkip: false,
      reason: 'Minimum frame rate limit',
      confidence: 0.5,
    };
  }

  /**
   * Calculate dynamic threshold for adaptive strategy
   */
  private calculateDynamicThreshold(): number {
    const currentFps = this.getCurrentFrameRate();

    // If current fps is below target, lower threshold (keep more frames)
    if (currentFps < this.targetFrameRate) {
      return 0.2; // Lower threshold = keep more frames
    }

    // If current fps is above target, raise threshold (skip more frames)
    if (currentFps > this.targetFrameRate * 1.2) {
      return 0.6; // Higher threshold = skip more frames
    }

    // Default threshold
    return 0.4;
  }

  /**
   * Check if we can skip frame based on frame rate limits
   */
  private canSkipBasedOnFrameRate(): boolean {
    const currentFps = this.getCurrentFrameRate();

    // Don't skip if we're at minimum frame rate
    if (currentFps <= this.minFrameRate) {
      return false;
    }

    return true;
  }

  /**
   * Keep frame (mark as not skipped)
   */
  private keepFrame(frame: FrameMetadata): void {
    this.lastKeptFrame = frame;
    this.frameHistory.push(frame);

    // Keep only recent frames (last 30 frames)
    if (this.frameHistory.length > 30) {
      this.frameHistory.shift();
    }

    this.stats.keptFrames++;
    this.updateFrameRate();
    this.updateStatistics();
  }

  /**
   * Skip frame (mark as skipped)
   */
  private skipFrame(frame: FrameMetadata): void {
    this.stats.skippedFrames++;
    this.updateStatistics();
  }

  /**
   * Update current frame rate estimate
   */
  private updateFrameRate(): void {
    if (this.frameHistory.length < 2) {
      this.currentFrameRate = 0;
      return;
    }

    // Calculate frame rate from last N frames
    const first = this.frameHistory[0];
    const last = this.frameHistory[this.frameHistory.length - 1];
    const timeSpan = (last.timestamp - first.timestamp) / 1000; // Convert to seconds

    if (timeSpan > 0) {
      this.currentFrameRate = this.frameHistory.length / timeSpan;
    }
  }

  /**
   * Get current frame rate
   */
  private getCurrentFrameRate(): number {
    return this.currentFrameRate;
  }

  /**
   * Update uniform skip interval based on target frame rate
   */
  private updateUniformSkipInterval(): void {
    // If target is 15 fps and source is 30 fps, keep every 2nd frame
    // Interval = source_fps / target_fps
    // For simplicity, assume 30 fps source
    const sourceFps = 30;
    this.uniformSkipInterval = Math.max(1, Math.floor(sourceFps / this.targetFrameRate));
  }

  /**
   * Update statistics
   */
  private updateStatistics(): void {
    this.stats.skipRatePercent =
      this.stats.totalFrames > 0
        ? (this.stats.skippedFrames / this.stats.totalFrames) * 100
        : 0;

    this.stats.averageFrameRate = this.currentFrameRate;
  }

  /**
   * Set target frame rate
   */
  setTargetFrameRate(fps: number): void {
    this.targetFrameRate = Math.max(this.minFrameRate, Math.min(fps, this.maxFrameRate));
    this.updateUniformSkipInterval();
    console.log(`FrameSkipper: Target frame rate set to ${this.targetFrameRate} fps`);
  }

  /**
   * Set skip strategy
   */
  setStrategy(strategy: SkipStrategy): void {
    this.strategy = strategy;
    console.log(`FrameSkipper: Strategy set to ${strategy}`);
  }

  /**
   * Enable or disable frame skipper
   */
  setEnabled(enabled: boolean): void {
    this.enabled = enabled;
    console.log(`FrameSkipper: ${enabled ? 'Enabled' : 'Disabled'}`);
  }

  /**
   * Get current statistics
   */
  getStats() {
    return {
      ...this.stats,
      currentFrameRate: this.currentFrameRate,
      targetFrameRate: this.targetFrameRate,
      strategy: this.strategy,
      enabled: this.enabled,
    };
  }

  /**
   * Reset skipper state
   */
  reset(): void {
    this.lastKeptFrame = null;
    this.frameHistory = [];
    this.currentFrameRate = 0;
    this.skipCounter = 0;
    this.stats = {
      totalFrames: 0,
      keptFrames: 0,
      skippedFrames: 0,
      skipRatePercent: 0,
      averageFrameRate: 0,
      sceneChangesKept: 0,
      keyFramesKept: 0,
      motionDrops: 0,
      priorityDrops: 0,
      uniformDrops: 0,
    };

    console.log('FrameSkipper: Reset');
  }
}

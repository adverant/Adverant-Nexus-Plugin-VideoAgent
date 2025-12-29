/**
 * AdaptiveBitrateController - Dynamically adjusts video streaming quality based on network conditions
 *
 * Features:
 * - Real-time bandwidth monitoring
 * - Latency tracking
 * - Frame rate adjustment (1-30 fps)
 * - Resolution scaling (240p-1080p)
 * - Quality presets (low, medium, high, auto)
 * - Smooth quality transitions
 * - Statistics reporting
 *
 * Quality Levels:
 * - Low: 240p, 1-5 fps, quality 40
 * - Medium: 480p, 10-15 fps, quality 60
 * - High: 720p, 20-25 fps, quality 80
 * - Ultra: 1080p, 30 fps, quality 90
 */

export interface QualityLevel {
  name: string;
  resolution: { width: number; height: number };
  frameRate: number;
  quality: number; // 0-100
  minBandwidth: number; // Bytes per second
  maxBandwidth: number; // Bytes per second
}

export interface NetworkMetrics {
  bandwidth: number; // Bytes per second
  latency: number; // Milliseconds
  packetLoss: number; // Percentage (0-100)
  jitter: number; // Milliseconds
  timestamp: number;
}

export interface AdaptiveConfig {
  enableAdaptive?: boolean; // Default: true
  initialQuality?: string; // Default: 'auto'
  minFrameRate?: number; // Default: 1 fps
  maxFrameRate?: number; // Default: 30 fps
  smoothingWindow?: number; // Default: 10 samples
  adjustmentThreshold?: number; // Default: 0.2 (20% change)
  stabilizationDelay?: number; // Default: 3000ms
}

export type QualityPreset = 'low' | 'medium' | 'high' | 'ultra' | 'auto';

const QUALITY_LEVELS: Record<string, QualityLevel> = {
  low: {
    name: 'Low',
    resolution: { width: 426, height: 240 },
    frameRate: 5,
    quality: 40,
    minBandwidth: 0,
    maxBandwidth: 128 * 1024, // 128 KB/s
  },
  medium: {
    name: 'Medium',
    resolution: { width: 854, height: 480 },
    frameRate: 15,
    quality: 60,
    minBandwidth: 128 * 1024,
    maxBandwidth: 512 * 1024, // 512 KB/s
  },
  high: {
    name: 'High',
    resolution: { width: 1280, height: 720 },
    frameRate: 25,
    quality: 80,
    minBandwidth: 512 * 1024,
    maxBandwidth: 2 * 1024 * 1024, // 2 MB/s
  },
  ultra: {
    name: 'Ultra',
    resolution: { width: 1920, height: 1080 },
    frameRate: 30,
    quality: 90,
    minBandwidth: 2 * 1024 * 1024,
    maxBandwidth: Infinity,
  },
};

export class AdaptiveBitrateController {
  private enableAdaptive: boolean;
  private currentQuality: QualityLevel;
  private targetQuality: QualityLevel | null = null;
  private networkHistory: NetworkMetrics[] = [];
  private smoothingWindow: number;
  private adjustmentThreshold: number;
  private stabilizationDelay: number;
  private lastAdjustment: number = 0;
  private transitionProgress = 0;
  private minFrameRate: number;
  private maxFrameRate: number;

  // Statistics
  private stats = {
    totalAdjustments: 0,
    upgrades: 0,
    downgrades: 0,
    averageBandwidth: 0,
    averageLatency: 0,
    currentLevel: 'medium',
  };

  constructor(config: AdaptiveConfig = {}) {
    this.enableAdaptive = config.enableAdaptive !== false;
    this.smoothingWindow = config.smoothingWindow || 10;
    this.adjustmentThreshold = config.adjustmentThreshold || 0.2;
    this.stabilizationDelay = config.stabilizationDelay || 3000;
    this.minFrameRate = config.minFrameRate || 1;
    this.maxFrameRate = config.maxFrameRate || 30;

    // Set initial quality
    const initialQuality = config.initialQuality || 'auto';
    this.currentQuality = this.selectInitialQuality(initialQuality);
  }

  /**
   * Select initial quality based on preset
   */
  private selectInitialQuality(preset: string): QualityLevel {
    if (preset === 'auto') {
      // Start with medium quality by default
      return QUALITY_LEVELS.medium;
    }

    return QUALITY_LEVELS[preset] || QUALITY_LEVELS.medium;
  }

  /**
   * Update network metrics and adjust quality if needed
   */
  updateNetworkMetrics(metrics: NetworkMetrics): void {
    // Add to history
    this.networkHistory.push(metrics);

    // Keep only recent samples
    if (this.networkHistory.length > this.smoothingWindow) {
      this.networkHistory.shift();
    }

    // Update statistics
    this.updateStatistics();

    // Adjust quality if adaptive is enabled
    if (this.enableAdaptive) {
      this.adjustQuality();
    }
  }

  /**
   * Adjust quality based on current network conditions
   */
  private adjustQuality(): void {
    const now = Date.now();

    // Don't adjust too frequently (wait for stabilization)
    if (now - this.lastAdjustment < this.stabilizationDelay) {
      return;
    }

    // Need enough samples to make a decision
    if (this.networkHistory.length < 3) {
      return;
    }

    // Calculate average bandwidth and latency
    const avgBandwidth = this.calculateAverageBandwidth();
    const avgLatency = this.calculateAverageLatency();

    // Determine optimal quality level
    const optimalQuality = this.determineOptimalQuality(avgBandwidth, avgLatency);

    // Check if adjustment is needed
    if (optimalQuality.name !== this.currentQuality.name) {
      const bandwidthChange = Math.abs(avgBandwidth - this.currentQuality.minBandwidth) /
        this.currentQuality.minBandwidth;

      // Only adjust if change is significant
      if (bandwidthChange >= this.adjustmentThreshold) {
        this.initiateQualityTransition(optimalQuality);
        this.lastAdjustment = now;

        // Update stats
        this.stats.totalAdjustments++;
        if (this.isUpgrade(this.currentQuality, optimalQuality)) {
          this.stats.upgrades++;
        } else {
          this.stats.downgrades++;
        }
      }
    }
  }

  /**
   * Determine optimal quality level based on network metrics
   */
  private determineOptimalQuality(bandwidth: number, latency: number): QualityLevel {
    // Factor in latency (reduce quality if latency is high)
    const latencyFactor = latency > 500 ? 0.7 : latency > 200 ? 0.85 : 1.0;
    const effectiveBandwidth = bandwidth * latencyFactor;

    // Find best matching quality level
    const levels = Object.values(QUALITY_LEVELS);

    for (let i = levels.length - 1; i >= 0; i--) {
      const level = levels[i];
      if (effectiveBandwidth >= level.minBandwidth) {
        return level;
      }
    }

    // Default to lowest quality
    return QUALITY_LEVELS.low;
  }

  /**
   * Initiate smooth transition to new quality level
   */
  private initiateQualityTransition(newQuality: QualityLevel): void {
    console.log(
      `AdaptiveBitrateController: Quality transition ${this.currentQuality.name} → ${newQuality.name}`
    );

    this.targetQuality = newQuality;
    this.transitionProgress = 0;

    // Start transition animation
    this.smoothTransition();
  }

  /**
   * Smooth transition between quality levels
   */
  private smoothTransition(): void {
    if (!this.targetQuality) {
      return;
    }

    const steps = 10;
    const interval = 100; // 100ms per step

    const transitionStep = () => {
      this.transitionProgress++;

      if (this.transitionProgress >= steps) {
        // Transition complete
        this.currentQuality = this.targetQuality!;
        this.targetQuality = null;
        this.transitionProgress = 0;
        this.stats.currentLevel = this.currentQuality.name.toLowerCase();

        console.log(`AdaptiveBitrateController: Transitioned to ${this.currentQuality.name}`);
      } else {
        // Continue transition
        setTimeout(transitionStep, interval);
      }
    };

    setTimeout(transitionStep, interval);
  }

  /**
   * Calculate average bandwidth from recent samples
   */
  private calculateAverageBandwidth(): number {
    if (this.networkHistory.length === 0) {
      return 0;
    }

    const sum = this.networkHistory.reduce((acc, m) => acc + m.bandwidth, 0);
    return sum / this.networkHistory.length;
  }

  /**
   * Calculate average latency from recent samples
   */
  private calculateAverageLatency(): number {
    if (this.networkHistory.length === 0) {
      return 0;
    }

    const sum = this.networkHistory.reduce((acc, m) => acc + m.latency, 0);
    return sum / this.networkHistory.length;
  }

  /**
   * Update statistics
   */
  private updateStatistics(): void {
    this.stats.averageBandwidth = this.calculateAverageBandwidth();
    this.stats.averageLatency = this.calculateAverageLatency();
  }

  /**
   * Check if quality change is an upgrade
   */
  private isUpgrade(current: QualityLevel, target: QualityLevel): boolean {
    return target.frameRate > current.frameRate ||
      target.resolution.width > current.resolution.width;
  }

  /**
   * Get current quality settings
   */
  getCurrentQuality(): QualityLevel {
    return { ...this.currentQuality };
  }

  /**
   * Get recommended frame rate for current quality
   */
  getRecommendedFrameRate(): number {
    if (this.targetQuality && this.transitionProgress > 0) {
      // During transition, interpolate frame rate
      const progress = this.transitionProgress / 10;
      const currentFps = this.currentQuality.frameRate;
      const targetFps = this.targetQuality.frameRate;
      return Math.round(currentFps + (targetFps - currentFps) * progress);
    }

    return this.currentQuality.frameRate;
  }

  /**
   * Get recommended resolution for current quality
   */
  getRecommendedResolution(): { width: number; height: number } {
    return { ...this.currentQuality.resolution };
  }

  /**
   * Get recommended quality value (0-100)
   */
  getRecommendedQuality(): number {
    return this.currentQuality.quality;
  }

  /**
   * Manually set quality level
   */
  setQualityLevel(preset: QualityPreset): void {
    if (preset === 'auto') {
      this.enableAdaptive = true;
      return;
    }

    // Disable adaptive and set fixed quality
    this.enableAdaptive = false;

    const quality = QUALITY_LEVELS[preset];
    if (quality) {
      this.initiateQualityTransition(quality);
    }
  }

  /**
   * Enable or disable adaptive bitrate
   */
  setAdaptive(enabled: boolean): void {
    this.enableAdaptive = enabled;

    if (enabled) {
      console.log('AdaptiveBitrateController: Adaptive mode enabled');
    } else {
      console.log('AdaptiveBitrateController: Adaptive mode disabled');
    }
  }

  /**
   * Get current statistics
   */
  getStats() {
    return {
      ...this.stats,
      currentQuality: this.currentQuality.name,
      targetQuality: this.targetQuality?.name || null,
      transitionProgress: this.transitionProgress,
      networkHistorySize: this.networkHistory.length,
      isAdaptive: this.enableAdaptive,
    };
  }

  /**
   * Reset controller state
   */
  reset(): void {
    this.networkHistory = [];
    this.targetQuality = null;
    this.transitionProgress = 0;
    this.lastAdjustment = 0;
    this.stats = {
      totalAdjustments: 0,
      upgrades: 0,
      downgrades: 0,
      averageBandwidth: 0,
      averageLatency: 0,
      currentLevel: 'medium',
    };

    console.log('AdaptiveBitrateController: Reset');
  }

  /**
   * Get all available quality levels
   */
  static getQualityLevels(): QualityLevel[] {
    return Object.values(QUALITY_LEVELS);
  }

  /**
   * Get quality level by name
   */
  static getQualityLevel(name: string): QualityLevel | undefined {
    return QUALITY_LEVELS[name];
  }
}

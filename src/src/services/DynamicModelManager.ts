import { MageAgentClient } from '../clients/MageAgentClient';
import { ProcessingOptions, SelectedModel } from '../types';

/**
 * DynamicModelManager - Zero hardcoded models
 * Selects optimal AI models dynamically based on task complexity
 */
export class DynamicModelManager {
  private mageAgent: MageAgentClient;
  private modelCache: Map<string, { model: SelectedModel; timestamp: number }>;
  private cacheTTL: number = 5 * 60 * 1000; // 5 minutes

  constructor(mageAgent: MageAgentClient) {
    this.mageAgent = mageAgent;
    this.modelCache = new Map();
  }

  /**
   * Select model for frame analysis (ZERO HARDCODED MODELS)
   */
  async selectFrameAnalysisModel(
    options: ProcessingOptions,
    videoMetadata: {
      quality: string;
      duration: number;
      width: number;
      height: number;
    }
  ): Promise<SelectedModel> {
    const complexity = this.calculateFrameComplexity(options, videoMetadata);
    const cacheKey = `frame_${complexity}_${options.qualityPreference}`;

    // Check cache
    const cached = this.getFromCache(cacheKey);
    if (cached) {
      return cached;
    }

    // Select model via MageAgent
    const model = await this.mageAgent.selectModel({
      taskType: 'vision',
      complexity,
      context: {
        task: 'frame_analysis',
        detect_objects: options.detectObjects,
        extract_text: options.extractText,
        classify_content: options.classifyContent,
        quality_preference: options.qualityPreference,
        video_quality: videoMetadata.quality,
      },
    });

    this.setCache(cacheKey, model);
    return model;
  }

  /**
   * Select model for audio transcription (ZERO HARDCODED MODELS)
   */
  async selectTranscriptionModel(
    options: ProcessingOptions,
    audioMetadata?: {
      duration: number;
      hasMultipleSpeakers?: boolean;
    }
  ): Promise<SelectedModel> {
    const complexity = this.calculateAudioComplexity(options, audioMetadata);
    const cacheKey = `audio_${complexity}_${options.qualityPreference}`;

    // Check cache
    const cached = this.getFromCache(cacheKey);
    if (cached) {
      return cached;
    }

    // Select model via MageAgent
    const model = await this.mageAgent.selectModel({
      taskType: 'transcription',
      complexity,
      context: {
        task: 'audio_transcription',
        enable_diarization: options.transcribeAudio,
        languages: options.targetLanguages,
        quality_preference: options.qualityPreference,
        duration: audioMetadata?.duration || 0,
      },
    });

    this.setCache(cacheKey, model);
    return model;
  }

  /**
   * Select model for content classification (ZERO HARDCODED MODELS)
   */
  async selectClassificationModel(
    frameCount: number,
    hasAudio: boolean
  ): Promise<SelectedModel> {
    const complexity = 0.6; // Base complexity for classification
    const cacheKey = `classification_${complexity}`;

    // Check cache
    const cached = this.getFromCache(cacheKey);
    if (cached) {
      return cached;
    }

    // Select model via MageAgent
    const model = await this.mageAgent.selectModel({
      taskType: 'classification',
      complexity,
      context: {
        task: 'content_classification',
        frame_count: frameCount,
        has_audio: hasAudio,
      },
    });

    this.setCache(cacheKey, model);
    return model;
  }

  /**
   * Calculate complexity for frame analysis
   */
  private calculateFrameComplexity(
    options: ProcessingOptions,
    videoMetadata: {
      quality: string;
      duration: number;
      width: number;
      height: number;
    }
  ): number {
    let complexity = 0.3; // Base complexity

    // Increase complexity based on options
    if (options.detectObjects) {
      complexity += 0.2;
    }

    if (options.extractText) {
      complexity += 0.15;
    }

    if (options.classifyContent) {
      complexity += 0.1;
    }

    if (options.detectScenes) {
      complexity += 0.15;
    }

    // Adjust for quality preference
    switch (options.qualityPreference) {
      case 'speed':
        complexity -= 0.1;
        break;
      case 'accuracy':
        complexity += 0.2;
        break;
    }

    // Adjust for video quality
    if (videoMetadata.quality === '4k') {
      complexity += 0.1;
    } else if (videoMetadata.quality === 'low') {
      complexity -= 0.05;
    }

    // Adjust for frame count (more frames = lower complexity per frame)
    if (options.maxFrames > 50) {
      complexity -= 0.1;
    }

    // Clamp between 0 and 1
    return Math.max(0, Math.min(1, complexity));
  }

  /**
   * Calculate complexity for audio transcription
   */
  private calculateAudioComplexity(
    options: ProcessingOptions,
    audioMetadata?: {
      duration: number;
      hasMultipleSpeakers?: boolean;
    }
  ): number {
    let complexity = 0.4; // Base complexity for transcription

    // Increase complexity if diarization is needed
    if (options.transcribeAudio) {
      complexity += 0.2;
    }

    // Increase complexity if multi-language
    if (options.targetLanguages.length > 1) {
      complexity += 0.1;
    }

    // Adjust for quality preference
    switch (options.qualityPreference) {
      case 'speed':
        complexity -= 0.1;
        break;
      case 'accuracy':
        complexity += 0.2;
        break;
    }

    // Additional analysis increases complexity
    if (options.classifyContent) {
      complexity += 0.1;
    }

    // Multiple speakers increases complexity
    if (audioMetadata?.hasMultipleSpeakers) {
      complexity += 0.15;
    }

    // Clamp between 0 and 1
    return Math.max(0, Math.min(1, complexity));
  }

  /**
   * Get model from cache
   */
  private getFromCache(key: string): SelectedModel | null {
    const cached = this.modelCache.get(key);
    if (!cached) {
      return null;
    }

    // Check if expired
    if (Date.now() - cached.timestamp > this.cacheTTL) {
      this.modelCache.delete(key);
      return null;
    }

    return cached.model;
  }

  /**
   * Set model in cache
   */
  private setCache(key: string, model: SelectedModel): void {
    this.modelCache.set(key, {
      model,
      timestamp: Date.now(),
    });
  }

  /**
   * Clear model cache
   */
  clearCache(): void {
    this.modelCache.clear();
  }

  /**
   * Get cache statistics
   */
  getCacheStats(): { size: number; keys: string[] } {
    return {
      size: this.modelCache.size,
      keys: Array.from(this.modelCache.keys()),
    };
  }
}

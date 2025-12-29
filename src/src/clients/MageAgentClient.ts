import axios, { AxiosInstance } from 'axios';
import { MageAgentModelRequest, MageAgentModelResponse, SelectedModel } from '../types';

/**
 * MageAgent Client for VideoAgent API
 * Provides zero-hardcoded-models integration with MageAgent service
 */
export class MageAgentClient {
  private client: AxiosInstance;
  private baseURL: string;

  constructor(baseURL: string) {
    this.baseURL = baseURL;
    this.client = axios.create({
      baseURL,
      timeout: 60000, // 60 seconds
      headers: {
        'Content-Type': 'application/json',
      },
    });

    // Add retry interceptor
    this.client.interceptors.response.use(
      (response) => response,
      async (error) => {
        const config = error.config;

        // Retry on network errors or 5xx
        if (!config.__retryCount) {
          config.__retryCount = 0;
        }

        if (config.__retryCount < 3 && this.isRetryable(error)) {
          config.__retryCount += 1;
          const delay = Math.pow(2, config.__retryCount) * 1000;
          await new Promise((resolve) => setTimeout(resolve, delay));
          return this.client(config);
        }

        return Promise.reject(error);
      }
    );
  }

  /**
   * Select the best AI model for a task (ZERO HARDCODED MODELS)
   * This is the core of zero-hardcoding architecture
   */
  async selectModel(request: MageAgentModelRequest): Promise<SelectedModel> {
    try {
      const response = await this.client.post<MageAgentModelResponse>(
        '/mageagent/api/model-select',
        {
          taskType: request.taskType,
          complexity: request.complexity,
          context: request.context,
          budget: request.budget,
        }
      );

      return {
        modelId: response.data.modelId,
        provider: response.data.modelProvider,
        complexity: request.complexity,
        estimatedCost: response.data.estimatedCost,
      };
    } catch (error) {
      throw new Error(`Model selection failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Analyze video frame with vision model
   */
  async analyzeFrame(
    imageBase64: string,
    modelId: string,
    prompt: string,
    context?: Record<string, unknown>
  ): Promise<any> {
    try {
      const response = await this.client.post('/mageagent/api/vision/analyze', {
        image: imageBase64,
        modelId,
        prompt,
        maxTokens: 500,
        additionalContext: context,
      });

      return response.data;
    } catch (error) {
      throw new Error(`Frame analysis failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Transcribe audio with speech-to-text model
   */
  async transcribeAudio(
    audioBase64: string,
    modelId: string,
    language: string = 'auto',
    enableDiarization: boolean = false
  ): Promise<any> {
    try {
      const response = await this.client.post('/mageagent/api/audio/transcribe', {
        audio: audioBase64,
        modelId,
        language,
        enableDiarization,
      });

      return response.data;
    } catch (error) {
      throw new Error(`Transcription failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Synthesize information from multiple sources
   */
  async synthesize(
    sources: string[],
    format: 'summary' | 'report' | 'analysis',
    objective?: string
  ): Promise<string> {
    try {
      const response = await this.client.post('/mageagent/api/synthesize', {
        sources,
        format,
        objective,
      });

      return response.data.result;
    } catch (error) {
      throw new Error(`Synthesis failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Orchestrate multiple AI agents for complex tasks
   */
  async orchestrate(
    task: string,
    maxAgents: number = 3,
    context?: Record<string, unknown>
  ): Promise<any> {
    try {
      const response = await this.client.post('/mageagent/api/orchestrate', {
        task,
        maxAgents,
        context,
        timeout: 60000,
      });

      return response.data;
    } catch (error) {
      throw new Error(`Orchestration failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Classify content using AI
   */
  async classifyContent(
    content: string,
    modelId: string,
    frames?: string[]
  ): Promise<any> {
    try {
      const response = await this.client.post('/mageagent/api/classify', {
        content,
        modelId,
        frames,
      });

      return response.data;
    } catch (error) {
      throw new Error(`Classification failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Extract topics from text
   */
  async extractTopics(text: string, modelId: string): Promise<string[]> {
    try {
      const response = await this.client.post('/mageagent/api/extract-topics', {
        text,
        modelId,
      });

      return response.data.topics;
    } catch (error) {
      throw new Error(`Topic extraction failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Analyze sentiment of text
   */
  async analyzeSentiment(
    text: string,
    modelId: string
  ): Promise<{ sentiment: string; confidence: number }> {
    try {
      const response = await this.client.post('/mageagent/api/sentiment', {
        text,
        modelId,
      });

      return {
        sentiment: response.data.sentiment,
        confidence: response.data.confidence,
      };
    } catch (error) {
      throw new Error(`Sentiment analysis failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Generate vector embedding for semantic search
   */
  async generateEmbedding(text: string, modelId: string): Promise<number[]> {
    try {
      const response = await this.client.post('/mageagent/api/embedding', {
        text,
        modelId,
      });

      return response.data.embedding;
    } catch (error) {
      throw new Error(`Embedding generation failed: ${this.getErrorMessage(error)}`);
    }
  }

  /**
   * Store memory in Nexus for learning
   */
  async storeMemory(
    content: string,
    tags: string[],
    metadata?: Record<string, unknown>
  ): Promise<void> {
    try {
      await this.client.post('/mageagent/api/memory', {
        content,
        tags,
        metadata,
      });
    } catch (error) {
      // Non-critical - log but don't throw
      console.error('Failed to store memory:', this.getErrorMessage(error));
    }
  }

  /**
   * Track model usage for learning and optimization
   */
  async trackModelUsage(usage: {
    taskType: string;
    modelId: string;
    modelProvider: string;
    complexity: number;
    cost: number;
    duration: number;
    success: boolean;
  }): Promise<void> {
    try {
      await this.client.post('/mageagent/api/track-usage', usage);
    } catch (error) {
      // Non-critical - log but don't throw
      console.error('Failed to track usage:', this.getErrorMessage(error));
    }
  }

  /**
   * Health check for MageAgent service
   */
  async healthCheck(): Promise<boolean> {
    try {
      const response = await this.client.get('/health');
      return response.status === 200;
    } catch (error) {
      return false;
    }
  }

  /**
   * Check if error is retryable
   */
  private isRetryable(error: any): boolean {
    if (!error.response) {
      return true; // Network error
    }

    const status = error.response.status;
    return status === 429 || status >= 500;
  }

  /**
   * Extract error message from error object
   */
  private getErrorMessage(error: unknown): string {
    if (error instanceof Error) {
      return error.message;
    }
    if (axios.isAxiosError(error)) {
      return error.response?.data?.message || error.message;
    }
    return String(error);
  }
}

// VideoAgent API Types - TypeScript definitions matching Go worker types

export interface ProcessingOptions {
  extractFrames: boolean;
  frameSamplingMode: 'keyframes' | 'uniform' | 'scene-based';
  frameSampleRate: number;
  maxFrames: number;
  extractAudio: boolean;
  transcribeAudio: boolean;
  detectScenes: boolean;
  detectObjects: boolean;
  extractText: boolean;
  classifyContent: boolean;
  generateSummary: boolean;
  customAnalysis?: string;
  targetLanguages: string[];
  qualityPreference: 'speed' | 'balanced' | 'accuracy';
  additionalMetadata?: Record<string, string>;
  // BullMQ-compatible fields
  extractMetadata?: boolean;
  analyzeFrames?: boolean;
  frameInterval?: number;
}

export interface JobPayload {
  jobId: string;
  filename: string;
  videoUrl?: string;
  videoBuffer?: Buffer;
  sourceType: 'url' | 'gdrive' | 'upload';
  userId: string;
  sessionId?: string;
  options: ProcessingOptions;
  metadata?: Record<string, unknown>;
  enqueuedAt: Date;
}

export interface VideoMetadata {
  duration: number;
  width: number;
  height: number;
  frameRate: number;
  codec: string;
  bitrate: number;
  size: number;
  format: string;
  audioCodec: string;
  audioTracks: number;
  hasSubtitles: boolean;
  quality: 'low' | 'medium' | 'high' | '4k';
}

export interface BoundingBox {
  x: number;
  y: number;
  width: number;
  height: number;
}

export interface ObjectDetection {
  objectId: string;
  label: string;
  confidence: number;
  boundingBox: BoundingBox;
  timestamp?: number;
}

export interface TextExtraction {
  textId: string;
  text: string;
  confidence: number;
  boundingBox: BoundingBox;
  language: string;
  timestamp?: number;
}

export interface FrameAnalysis {
  frameId: string;
  timestamp: number;
  frameNumber: number;
  filePath: string;
  objects: ObjectDetection[];
  text: TextExtraction[];
  description: string;
  embedding?: number[];
  modelUsed: string;
  confidence: number;
  metadata?: Record<string, unknown>;
}

export interface SpeakerSegment {
  speakerId: string;
  startTime: number;
  endTime: number;
  text: string;
  confidence: number;
}

export interface AudioAnalysis {
  transcription: string;
  language: string;
  confidence: number;
  speakers: SpeakerSegment[];
  sentiment: string;
  topics: string[];
  keywords: string[];
  audioFilePath: string;
  modelUsed: string;
  processingTime: number;
}

export interface SceneDetection {
  sceneId: string;
  startTime: number;
  endTime: number;
  startFrame: number;
  endFrame: number;
  description: string;
  keyFrameId: string;
  sceneType: string;
  confidence: number;
}

export interface ContentClassification {
  primaryCategory: string;
  categories: Record<string, number>;
  tags: string[];
  contentRating: string;
  isNsfw: boolean;
  confidence: number;
  modelUsed: string;
}

export interface ModelUsageRecord {
  taskType: string;
  modelId: string;
  modelProvider: string;
  complexity: number;
  cost: number;
  duration: number;
  success: boolean;
  timestamp: Date;
}

export interface ProcessingResult {
  jobId: string;
  status: 'pending' | 'processing' | 'completed' | 'failed';
  videoMetadata: VideoMetadata;
  frames: FrameAnalysis[];
  audioAnalysis?: AudioAnalysis;
  scenes: SceneDetection[];
  objects: ObjectDetection[];
  textExtraction: TextExtraction[];
  classification?: ContentClassification;
  summary: string;
  error?: string;
  processingTime: number;
  modelUsage: ModelUsageRecord[];
  startedAt: Date;
  completedAt: Date;
}

export interface ProgressUpdate {
  jobId: string;
  status: string;
  progress: number;
  currentStage: string;
  message: string;
  framesProcessed: number;
  totalFrames: number;
  elapsedTime: number;
  estimatedRemaining: number;
  timestamp: Date;
}

// MageAgent Types
export interface MageAgentModelRequest {
  taskType: 'vision' | 'transcription' | 'classification' | 'synthesis';
  complexity: number;
  context: Record<string, unknown>;
  budget?: number;
}

export interface MageAgentModelResponse {
  modelId: string;
  modelProvider: string;
  estimatedCost: number;
  reasoning: string;
}

export interface SelectedModel {
  modelId: string;
  provider: string;
  complexity: number;
  estimatedCost: number;
}

// API Request/Response Types
export interface ProcessVideoRequest {
  videoUrl?: string;
  filename: string;
  sourceType: 'url' | 'gdrive' | 'upload';
  userId: string;
  sessionId?: string;
  options: ProcessingOptions;
  priority?: number;
  delay?: number;
}

export interface ProcessVideoResponse {
  jobId: string;
  status: string;
  message: string;
  enqueuedAt: Date;
}

export interface GetJobStatusResponse {
  job: JobPayload;
  result?: ProcessingResult;
  progress?: ProgressUpdate;
}

// Google Drive Types
export interface GoogleDriveFile {
  id: string;
  name: string;
  mimeType: string;
  size: number;
  webViewLink?: string | null;
  thumbnailLink?: string | null;
}

export interface GoogleDriveFolder {
  id: string;
  name: string;
  files: GoogleDriveFile[];
  folders: GoogleDriveFolder[];
}

export interface GoogleDriveAuthRequest {
  code: string;
  redirectUri: string;
}

export interface GoogleDriveAuthResponse {
  accessToken: string;
  refreshToken: string;
  expiresIn: number;
}

// Configuration
export interface VideoAgentConfig {
  port: number;
  redisUrl: string;
  postgresUrl: string;
  mageAgentUrl: string;
  tempDir: string;
  maxVideoSize: number;
  enableGoogleDrive: boolean;
  googleClientId?: string;
  googleClientSecret?: string;
  googleRedirectUri?: string;
}

// Validation schemas helper types
export type ValidationResult<T> = {
  success: true;
  data: T;
} | {
  success: false;
  error: string;
};

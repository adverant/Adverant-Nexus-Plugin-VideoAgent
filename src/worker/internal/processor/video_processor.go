package processor

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/extractor"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
	"github.com/adverant/nexus/videoagent-worker/internal/storage"
	"github.com/adverant/nexus/videoagent-worker/internal/utils"
	"github.com/redis/go-redis/v9"
)

// VideoProcessor orchestrates video processing pipeline
type VideoProcessor struct {
	ffmpeg            *utils.FFmpegHelper
	mageAgent         *clients.MageAgentClient
	storage           *storage.StorageManager
	frameExtractor    *extractor.FrameExtractor
	audioExtractor    *extractor.AudioExtractor
	metadataExtractor *extractor.MetadataExtractor
	httpDownloader    *utils.HTTPDownloader
	youtubeDownloader *utils.YouTubeDownloader
	redisClient       *redis.Client
}

// NewVideoProcessor creates a new video processor
func NewVideoProcessor(
	ffmpeg *utils.FFmpegHelper,
	mageAgent *clients.MageAgentClient,
	storageManager *storage.StorageManager,
	redisClient *redis.Client,
	concurrency int,
) *VideoProcessor {
	// Initialize HTTP downloader with default config
	httpDownloader := utils.NewHTTPDownloader(&utils.HTTPDownloaderConfig{
		MaxRetries:   3,
		RetryDelay:   2 * time.Second,
		Timeout:      5 * time.Minute,
		MaxFileSize:  5 * 1024 * 1024 * 1024, // 5GB
		AllowedTypes: []string{"video/"},
		TempDir:      "/tmp",
	})

	// Initialize YouTube downloader
	youtubeDownloader, err := utils.NewYouTubeDownloader("/app/temp")
	if err != nil {
		// Log error but don't fail - system can work without YouTube support
		fmt.Printf("Warning: YouTube downloader initialization failed (YouTube videos will not be supported): %v\n", err)
		youtubeDownloader = nil
	}

	return &VideoProcessor{
		ffmpeg:            ffmpeg,
		mageAgent:         mageAgent,
		storage:           storageManager,
		frameExtractor:    extractor.NewFrameExtractor(ffmpeg, mageAgent, concurrency),
		audioExtractor:    extractor.NewAudioExtractor(ffmpeg, mageAgent),
		metadataExtractor: extractor.NewMetadataExtractor(ffmpeg),
		httpDownloader:    httpDownloader,
		youtubeDownloader: youtubeDownloader,
		redisClient:       redisClient,
	}
}

// SetYouTubeAuthClient configures the YouTube downloader with an OAuth auth client
// This enables authenticated downloads for private/unlisted videos
func (vp *VideoProcessor) SetYouTubeAuthClient(authClient utils.YouTubeAuthClient) {
	if vp.youtubeDownloader != nil {
		vp.youtubeDownloader.SetAuthClient(authClient)
	}
}

// Process processes a video job (main entry point)
func (vp *VideoProcessor) Process(ctx context.Context, job *models.JobPayload) error {
	startTime := time.Now()

	// Store job in database
	if err := vp.storage.StoreJob(ctx, job); err != nil {
		return fmt.Errorf("failed to store job: %w", err)
	}

	// Update job status to processing
	if err := vp.storage.UpdateJobStatus(ctx, job.JobID, "processing", ""); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	// Send progress update
	vp.sendProgress(ctx, job.JobID, 0, "started", "Initializing video processing")

	// Step 1: Prepare video file
	videoPath, err := vp.prepareVideo(ctx, job)
	if err != nil {
		vp.storage.UpdateJobStatus(ctx, job.JobID, "failed", err.Error())
		return fmt.Errorf("video preparation failed: %w", err)
	}
	defer vp.ffmpeg.Cleanup(videoPath)

	vp.sendProgress(ctx, job.JobID, 10, "processing", "Video file prepared")

	// Step 2: Validate video
	if err := vp.metadataExtractor.ValidateVideo(videoPath); err != nil {
		vp.storage.UpdateJobStatus(ctx, job.JobID, "failed", "Invalid video file: "+err.Error())
		return fmt.Errorf("video validation failed: %w", err)
	}

	vp.sendProgress(ctx, job.JobID, 15, "processing", "Video validated")

	// Step 3: Extract metadata
	metadata, err := vp.metadataExtractor.Extract(videoPath)
	if err != nil {
		vp.storage.UpdateJobStatus(ctx, job.JobID, "failed", err.Error())
		return fmt.Errorf("metadata extraction failed: %w", err)
	}

	if err := vp.storage.StoreVideoMetadata(ctx, job.JobID, metadata); err != nil {
		return fmt.Errorf("failed to store metadata: %w", err)
	}

	vp.sendProgress(ctx, job.JobID, 25, "processing", "Metadata extracted")

	// Step 4: Extract and analyze frames (if requested)
	var frames []models.FrameAnalysis
	shouldExtractFrames := job.Options.ExtractFrames != nil && *job.Options.ExtractFrames
	if shouldExtractFrames {
		frames, err = vp.frameExtractor.ExtractAndAnalyze(ctx, videoPath, job.JobID, job.Options, metadata.Duration)
		if err != nil {
			vp.storage.UpdateJobStatus(ctx, job.JobID, "failed", err.Error())
			return fmt.Errorf("frame analysis failed: %w", err)
		}

		// Store frames
		for _, frame := range frames {
			if err := vp.storage.StoreFrame(ctx, &frame, job.JobID); err != nil {
				return fmt.Errorf("failed to store frame: %w", err)
			}

			// Store objects
			if len(frame.Objects) > 0 {
				if err := vp.storage.StoreObjects(ctx, job.JobID, frame.FrameID, frame.Objects); err != nil {
					return fmt.Errorf("failed to store objects: %w", err)
				}
			}
		}

		vp.sendProgress(ctx, job.JobID, 60, "processing", fmt.Sprintf("Analyzed %d frames", len(frames)))
	}

	// Step 5: Extract and transcribe audio (if requested)
	var audioAnalysis *models.AudioAnalysis
	shouldExtractAudio := job.Options.ExtractAudio != nil && *job.Options.ExtractAudio
	if shouldExtractAudio {
		audioAnalysis, err = vp.audioExtractor.ExtractAndTranscribe(ctx, videoPath, job.JobID, job.Options)
		if err != nil {
			// Non-fatal - continue without audio
			audioAnalysis = nil
		} else {
			if err := vp.storage.StoreAudioAnalysis(ctx, job.JobID, audioAnalysis); err != nil {
				return fmt.Errorf("failed to store audio analysis: %w", err)
			}
			vp.sendProgress(ctx, job.JobID, 75, "processing", "Audio transcribed")
		}
	}

	// Step 6: Detect scenes (if requested)
	var scenes []models.SceneDetection
	if job.Options.ShouldDetectScenes() && len(frames) > 0 {
		// Select model for scene analysis
		modelReq := models.MageAgentModelRequest{
			TaskType:   "vision",
			Complexity: 0.5,
			Context: map[string]interface{}{
				"task": "scene_detection",
			},
		}

		modelResp, err := vp.mageAgent.SelectModel(ctx, modelReq)
		if err == nil {
			scenes, _ = vp.frameExtractor.DetectScenes(ctx, frames, modelResp.ModelID)
		}

		vp.sendProgress(ctx, job.JobID, 85, "processing", fmt.Sprintf("Detected %d scenes", len(scenes)))
	}

	// Step 7: Classify content (if requested)
	var classification *models.ContentClassification
	shouldClassifyContent := job.Options.ClassifyContent != nil && *job.Options.ClassifyContent
	if shouldClassifyContent {
		classification, err = vp.classifyContent(ctx, frames, audioAnalysis)
		if err == nil && classification != nil {
			// Store classification in database
			// (Implementation would go here)
		}

		vp.sendProgress(ctx, job.JobID, 90, "processing", "Content classified")
	}

	// Step 8: Generate summary (if requested)
	var summary string
	shouldGenerateSummary := job.Options.GenerateSummary != nil && *job.Options.GenerateSummary
	if shouldGenerateSummary {
		summary, err = vp.generateSummary(ctx, frames, audioAnalysis, metadata)
		if err != nil {
			summary = ""
		}

		vp.sendProgress(ctx, job.JobID, 95, "processing", "Summary generated")
	}

	// Step 9: Build final result
	processingTime := time.Since(startTime).Seconds()

	// Collect all objects
	allObjects := []models.ObjectDetection{}
	for _, frame := range frames {
		allObjects = append(allObjects, frame.Objects...)
	}

	result := &models.ProcessingResult{
		JobID:           job.JobID,
		Status:          "completed",
		VideoMetadata:   *metadata,
		Frames:          frames,
		AudioAnalysis:   audioAnalysis,
		Scenes:          scenes,
		Objects:         allObjects,
		Classification:  classification,
		Summary:         summary,
		ProcessingTime:  processingTime,
		StartedAt:       startTime,
		CompletedAt:     time.Now(),
	}

	// Store result
	if err := vp.storage.StoreProcessingResult(ctx, result); err != nil {
		return fmt.Errorf("failed to store result: %w", err)
	}

	// Store classification in PostgreSQL and GraphRAG (if classification was performed)
	if classification != nil {
		if err := vp.storage.StoreClassification(ctx, job.JobID, classification); err != nil {
			// Non-fatal - log but continue
			fmt.Printf("Warning: failed to store classification: %v\n", err)
		}
	}

	// Update job status
	if err := vp.storage.UpdateJobStatus(ctx, job.JobID, "completed", ""); err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	// Send final progress update
	vp.sendProgress(ctx, job.JobID, 100, "completed", "Processing complete")

	// Store processing result in Brain memory for learning
	vp.storeBrainMemory(ctx, result)

	return nil
}

// prepareVideo prepares video file from buffer or URL
func (vp *VideoProcessor) prepareVideo(ctx context.Context, job *models.JobPayload) (string, error) {
	// Priority 1: Check if video data provided directly (Google Drive, upload)
	if len(job.VideoBuffer) > 0 {
		return vp.ffmpeg.SaveVideoFromBuffer(job.VideoBuffer, job.JobID)
	}

	// Priority 1.5: Check if local file path provided (file:// protocol)
	// This handles pre-downloaded files from FileProcess
	if strings.HasPrefix(job.VideoURL, "file://") {
		localPath := strings.TrimPrefix(job.VideoURL, "file://")

		// Verify file exists
		if _, err := os.Stat(localPath); os.IsNotExist(err) {
			return "", fmt.Errorf("local file not found: %s", localPath)
		}

		// Validate it's a valid video file
		if err := vp.ffmpeg.ValidateVideo(localPath); err != nil {
			return "", fmt.Errorf("local file validation failed: %w", err)
		}

		return localPath, nil
	}

	// Priority 2: Check if YouTube URL provided - download via yt-dlp
	if job.VideoURL != "" && utils.IsYouTubeURL(job.VideoURL) {
		if vp.youtubeDownloader == nil {
			return "", fmt.Errorf("YouTube URL detected but YouTube downloader not available (yt-dlp not installed). URL: %s", job.VideoURL)
		}

		// Use authenticated download if userID is available
		// This provides access to private/unlisted videos and better rate limits
		var videoPath string
		var err error

		if job.UserID != "" {
			// Attempt authenticated download with user's OAuth token
			videoPath, err = vp.youtubeDownloader.DownloadWithUserAuth(ctx, job.VideoURL, job.JobID, job.UserID)
		} else {
			// Fall back to unauthenticated download
			videoPath, err = vp.youtubeDownloader.Download(ctx, job.VideoURL, job.JobID)
		}

		if err != nil {
			return "", fmt.Errorf("failed to download YouTube video from %s: %w", job.VideoURL, err)
		}

		// Validate downloaded file with FFmpeg
		if err := vp.ffmpeg.ValidateVideo(videoPath); err != nil {
			// Cleanup invalid file
			vp.youtubeDownloader.Cleanup(videoPath)
			return "", fmt.Errorf("downloaded YouTube video failed validation: %w", err)
		}

		return videoPath, nil
	}

	// Priority 3: Check if HTTP URL provided - download via HTTP
	if job.VideoURL != "" {
		videoPath, err := vp.httpDownloader.DownloadFile(ctx, job.VideoURL, job.JobID)
		if err != nil {
			return "", fmt.Errorf("failed to download video from URL %s: %w", job.VideoURL, err)
		}

		// Validate downloaded file with FFmpeg
		if err := vp.ffmpeg.ValidateVideo(videoPath); err != nil {
			// Cleanup invalid file
			vp.httpDownloader.CleanupFile(videoPath)
			return "", fmt.Errorf("downloaded file failed validation: %w", err)
		}

		return videoPath, nil
	}

	// No video data or URL provided
	return "", fmt.Errorf("no video data provided: neither VideoBuffer nor VideoURL specified")
}

// classifyContent classifies video content using AI
func (vp *VideoProcessor) classifyContent(
	ctx context.Context,
	frames []models.FrameAnalysis,
	audioAnalysis *models.AudioAnalysis,
) (*models.ContentClassification, error) {

	// Select model for classification
	modelReq := models.MageAgentModelRequest{
		TaskType:   "classification",
		Complexity: 0.6,
		Context: map[string]interface{}{
			"task": "content_classification",
		},
	}

	modelResp, err := vp.mageAgent.SelectModel(ctx, modelReq)
	if err != nil {
		return nil, err
	}

	// Build content description
	descriptions := []string{}
	for _, frame := range frames {
		if frame.Description != "" {
			descriptions = append(descriptions, frame.Description)
		}
	}

	if audioAnalysis != nil && audioAnalysis.Transcription != "" {
		descriptions = append(descriptions, "Transcription: "+audioAnalysis.Transcription)
	}

	// Classify using MageAgent
	classification, err := vp.mageAgent.ClassifyContent(ctx, "", descriptions, modelResp.ModelID)
	if err != nil {
		return nil, err
	}

	return classification, nil
}

// generateSummary generates video summary using AI
func (vp *VideoProcessor) generateSummary(
	ctx context.Context,
	frames []models.FrameAnalysis,
	audioAnalysis *models.AudioAnalysis,
	metadata *models.VideoMetadata,
) (string, error) {

	// Collect sources for synthesis
	sources := []string{}

	// Add metadata summary
	metadataSummary := fmt.Sprintf(
		"Video: %dx%d, %.1f seconds, %.1f fps, %s codec, %s quality",
		metadata.Width,
		metadata.Height,
		metadata.Duration,
		metadata.FrameRate,
		metadata.Codec,
		metadata.Quality,
	)
	sources = append(sources, metadataSummary)

	// Add frame descriptions (sample key frames)
	sampleCount := 5
	if len(frames) > 0 {
		step := len(frames) / sampleCount
		if step == 0 {
			step = 1
		}

		for i := 0; i < len(frames); i += step {
			if len(sources) >= sampleCount+1 {
				break
			}
			sources = append(sources, frames[i].Description)
		}
	}

	// Add transcription
	if audioAnalysis != nil && audioAnalysis.Transcription != "" {
		sources = append(sources, "Transcription: "+audioAnalysis.Transcription)
	}

	// Synthesize summary
	summary, err := vp.mageAgent.Synthesize(ctx, sources, "summary", "video summary")
	if err != nil {
		return "", err
	}

	return summary, nil
}

// sendProgress sends progress update via WebSocket (through Redis pub/sub)
func (vp *VideoProcessor) sendProgress(ctx context.Context, jobID string, progress float64, status, message string) {
	update := models.ProgressUpdate{
		JobID:        jobID,
		Status:       status,
		Progress:     progress,
		CurrentStage: message,
		Message:      message,
		Timestamp:    time.Now(),
	}

	// Publish to Redis channel for WebSocket server
	channel := fmt.Sprintf("videoagent:progress:%s", jobID)
	vp.redisClient.Publish(ctx, channel, update)
}

// storeBrainMemory stores processing insights in Brain for learning
func (vp *VideoProcessor) storeBrainMemory(ctx context.Context, result *models.ProcessingResult) {
	content := fmt.Sprintf(
		"Processed video: %d frames, %d objects, %d scenes, %.2f seconds processing time",
		len(result.Frames),
		len(result.Objects),
		len(result.Scenes),
		result.ProcessingTime,
	)

	tags := []string{"videoagent", "processing", "completed"}

	metadata := map[string]interface{}{
		"job_id":          result.JobID,
		"processing_time": result.ProcessingTime,
		"frame_count":     len(result.Frames),
		"quality":         result.VideoMetadata.Quality,
	}

	// Store in Brain (fire and forget - non-critical)
	vp.mageAgent.StoreMemory(ctx, content, tags, metadata)
}

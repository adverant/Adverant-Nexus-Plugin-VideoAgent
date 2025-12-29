package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/adverant/nexus/videoagent-worker/internal/clients"
	"github.com/adverant/nexus/videoagent-worker/internal/extractor"
	"github.com/adverant/nexus/videoagent-worker/internal/models"
	"github.com/adverant/nexus/videoagent-worker/internal/processor"
	"github.com/adverant/nexus/videoagent-worker/internal/queue"
	"github.com/adverant/nexus/videoagent-worker/internal/similarity"
	"github.com/adverant/nexus/videoagent-worker/internal/storage"
	"github.com/adverant/nexus/videoagent-worker/internal/utils"
)

func main() {
	// Check mode: "subprocess" or "standalone"
	mode := getEnv("WORKER_MODE", "standalone")

	if mode == "subprocess" {
		// Subprocess mode: Read JSON from stdin, process, write to stdout
		runSubprocessMode()
	} else {
		// Standalone mode: Original Asynq queue consumer
		runStandaloneMode()
	}
}

// runSubprocessMode reads JSON from stdin, processes video, writes result to stdout
func runSubprocessMode() {
	// Redirect logs to stderr to keep stdout clean for JSON output
	log.SetOutput(os.Stderr)
	log.SetFlags(0) // Remove timestamps for cleaner logs

	// Read input from stdin
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		sendError(fmt.Sprintf("Failed to read stdin: %v", err))
		os.Exit(1)
	}

	// Parse job payload
	var jobPayload models.JobPayload
	if err := json.Unmarshal(input, &jobPayload); err != nil {
		sendError(fmt.Sprintf("Failed to parse job payload: %v", err))
		os.Exit(1)
	}

	// Load configuration
	config := loadConfig()
	ctx := context.Background()

	// Initialize components
	ffmpeg, err := utils.NewFFmpegHelper(config.TempDir)
	if err != nil {
		sendError(fmt.Sprintf("Failed to initialize FFmpeg: %v", err))
		os.Exit(1)
	}
	log.Printf("✓ FFmpeg initialized")

	mageAgent := clients.NewMageAgentClient(config.MageAgentURL, 60*time.Second)
	if err := mageAgent.HealthCheck(ctx); err != nil {
		log.Printf("WARNING: MageAgent health check failed: %v", err)
	}

	graphragClient, err := clients.NewGraphRAGClient(config.GraphRAGURL)
	if err != nil {
		sendError(fmt.Sprintf("Failed to initialize GraphRAG client: %v", err))
		os.Exit(1)
	}

	// Initialize Nexus-Auth client for YouTube OAuth tokens (best effort)
	var authClient *clients.NexusAuthClient
	if config.NexusAuthURL != "" && config.InternalServiceAPIKey != "" {
		authClient, err = clients.NewNexusAuthClient(config.NexusAuthURL, config.InternalServiceAPIKey)
		if err != nil {
			log.Printf("WARNING: Failed to initialize Nexus-Auth client: %v", err)
			log.Printf("YouTube authenticated downloads will be disabled")
		} else {
			if err := authClient.HealthCheck(ctx); err != nil {
				log.Printf("WARNING: Nexus-Auth health check failed: %v", err)
			} else {
				log.Printf("✓ Nexus-Auth client initialized (YouTube OAuth enabled)")
			}
		}
	} else {
		log.Printf("INFO: Nexus-Auth not configured, YouTube authenticated downloads disabled")
	}

	// In subprocess mode, we skip PostgreSQL storage initialization
	// as results are returned via stdout to the Node.js worker
	// which handles persistence if needed
	log.Printf("Subprocess mode: skipping direct database storage")

	// Note: Redis is NOT needed in subprocess mode
	// The Node.js BullMQ worker handles all queue management
	// This Go binary only processes videos and returns results via stdout

	similarityModule, err := similarity.InitializeSimilarityModule(
		mageAgent,
		graphragClient,
		config.QdrantURL,
		"",
	)
	if err != nil {
		log.Printf("WARNING: Failed to initialize similarity module: %v", err)
	} else {
		// Initialize collections (best effort)
		if err := similarityModule.InitializeCollections(ctx); err != nil {
			log.Printf("WARNING: Failed to initialize Qdrant collections: %v", err)
		}
	}

	// In subprocess mode, we process video directly without PostgreSQL dependencies
	log.Printf("Processing job: %s", jobPayload.JobID)
	log.Printf("Video URL: %s", jobPayload.VideoURL)
	log.Printf("Options: ExtractMetadata=%v, DetectScenes=%v, AnalyzeFrames=%v, TranscribeAudio=%v",
		jobPayload.Options.ShouldExtractMetadata(),
		jobPayload.Options.ShouldDetectScenes(),
		jobPayload.Options.AnalyzeFrames,
		jobPayload.Options.ShouldTranscribeAudio())

	// Step 1: Download video file (with YouTube support)
	var videoPath string
	var cleanupFunc func()

	// Check if this is a YouTube URL
	if utils.IsYouTubeURL(jobPayload.VideoURL) {
		log.Printf("YouTube URL detected, using yt-dlp to download: %s", jobPayload.VideoURL)

		youtubeDownloader, err := utils.NewYouTubeDownloader(config.TempDir)
		if err != nil {
			sendError(fmt.Sprintf("Failed to initialize YouTube downloader (yt-dlp may not be installed): %v", err))
			os.Exit(1)
		}

		// Configure YouTube downloader with auth client if available
		if authClient != nil {
			youtubeDownloader.SetAuthClient(authClient)
			log.Printf("YouTube downloader configured with OAuth authentication")
		}

		// Use authenticated download if userID is available
		if jobPayload.UserID != "" {
			log.Printf("Attempting authenticated YouTube download for user: %s", jobPayload.UserID)
			videoPath, err = youtubeDownloader.DownloadWithUserAuth(ctx, jobPayload.VideoURL, jobPayload.JobID, jobPayload.UserID)
		} else {
			log.Printf("No userID provided, using unauthenticated download")
			videoPath, err = youtubeDownloader.Download(ctx, jobPayload.VideoURL, jobPayload.JobID)
		}
		if err != nil {
			sendError(fmt.Sprintf("Failed to download YouTube video: %v", err))
			os.Exit(1)
		}
		cleanupFunc = func() { youtubeDownloader.Cleanup(videoPath) }
		log.Printf("✓ YouTube video downloaded to: %s", videoPath)
	} else {
		// Regular HTTP download
		log.Printf("Downloading video via HTTP from: %s", jobPayload.VideoURL)
		downloader := utils.NewHTTPDownloader(&utils.HTTPDownloaderConfig{
			MaxFileSize:  config.MaxVideoSize,
			AllowedTypes: []string{"video/"},
			TempDir:      config.TempDir,
		})
		videoPath, err = downloader.DownloadFile(ctx, jobPayload.VideoURL, jobPayload.JobID)
		if err != nil {
			sendError(fmt.Sprintf("Failed to download video: %v", err))
			os.Exit(1)
		}
		cleanupFunc = func() { downloader.CleanupFile(videoPath) }
		log.Printf("✓ Video downloaded to: %s", videoPath)
	}
	defer cleanupFunc()

	// Step 2: Extract metadata
	log.Printf("Extracting video metadata...")
	metadataMap, err := ffmpeg.GetVideoMetadata(videoPath)
	if err != nil {
		sendError(fmt.Sprintf("Failed to extract metadata: %v", err))
		os.Exit(1)
	}

	// DEBUG: Print raw metadata map
	log.Printf("DEBUG: Raw metadata map: %+v", metadataMap)

	// Extract metadata fields
	duration, _ := metadataMap["duration"].(float64)
	width, _ := metadataMap["width"].(int)
	height, _ := metadataMap["height"].(int)
	codec, _ := metadataMap["codec"].(string)
	format, _ := metadataMap["format"].(string)
	bitrate, _ := metadataMap["bitrate"].(int64)

	// DEBUG: Print extracted values
	log.Printf("DEBUG: Extracted - duration=%.2f, width=%d, height=%d, codec=%s, format=%s",
		duration, width, height, codec, format)

	log.Printf("✓ Metadata extracted: %dx%d, %.2fs duration, %s codec",
		width, height, duration, codec)

	// Step 3: Extract and analyze frames (if requested)
	var frameResults []map[string]interface{}
	shouldAnalyzeFrames := jobPayload.Options.AnalyzeFrames != nil && *jobPayload.Options.AnalyzeFrames
	if shouldAnalyzeFrames {
		log.Printf("Extracting and analyzing frames...")
		frameExtractor := extractor.NewFrameExtractor(ffmpeg, mageAgent, config.WorkerConcurrency)
		frames, err := frameExtractor.ExtractAndAnalyze(ctx, videoPath, jobPayload.JobID, jobPayload.Options, duration)
		if err != nil {
			sendError(fmt.Sprintf("Failed to analyze frames: %v", err))
			os.Exit(1)
		}
		log.Printf("✓ Analyzed %d frames", len(frames))

		// Convert frames to serializable format
		for _, frame := range frames {
			frameResults = append(frameResults, map[string]interface{}{
				"frameId":     frame.FrameID,
				"timestamp":   frame.Timestamp,
				"frameNumber": frame.FrameNumber,
				"description": frame.Description,
				"objects":     frame.Objects,
				"text":        frame.Text,
				"confidence":  frame.Confidence,
				"modelUsed":   frame.ModelUsed,
			})
		}
	}

	// Step 4: Detect scenes (if requested)
	var sceneResults []map[string]interface{}
	if jobPayload.Options.ShouldDetectScenes() && len(frameResults) > 0 {
		log.Printf("Detecting scenes...")
		// Scene detection requires frames with embeddings
		// For now, we'll skip scene detection in subprocess mode
		// as it requires more complex coordination
		log.Printf("⚠️ Scene detection not yet implemented in subprocess mode")
	}

	// Step 5: Extract and transcribe audio (if requested)
	var audioResult map[string]interface{}
	shouldTranscribeAudio := jobPayload.Options.ShouldTranscribeAudio()
	if shouldTranscribeAudio {
		log.Printf("Extracting and transcribing audio...")
		audioExtractor := extractor.NewAudioExtractor(ffmpeg, mageAgent)
		audioAnalysis, err := audioExtractor.ExtractAndTranscribe(ctx, videoPath, jobPayload.JobID, jobPayload.Options)
		if err != nil {
			log.Printf("⚠️ Audio transcription failed: %v", err)
			// Non-fatal - continue without audio
		} else {
			log.Printf("✓ Audio transcribed: %d characters, language: %s", len(audioAnalysis.Transcription), audioAnalysis.Language)
			audioResult = map[string]interface{}{
				"transcription": audioAnalysis.Transcription,
				"language":      audioAnalysis.Language,
				"confidence":    audioAnalysis.Confidence,
				"speakers":      audioAnalysis.Speakers,
				"sentiment":     audioAnalysis.Sentiment,
				"topics":        audioAnalysis.Topics,
				"keywords":      audioAnalysis.Keywords,
				"modelUsed":     audioAnalysis.ModelUsed,
			}
		}
	}

	// Step 6: Build success response
	log.Printf("✅ Video processing complete for job: %s", jobPayload.JobID)
	successResponse := map[string]interface{}{
		"success": true,
		"jobId":   jobPayload.JobID,
		"status":  "completed",
		"message": "Video processing completed successfully",
		"results": map[string]interface{}{
			"metadata": map[string]interface{}{
				"width":    width,
				"height":   height,
				"duration": duration,
				"codec":    codec,
				"format":   format,
				"bitrate":  bitrate,
			},
			"frames": frameResults,
			"scenes": sceneResults,
			"audio":  audioResult,
		},
	}

	resultJSON, err := json.Marshal(successResponse)
	if err != nil {
		sendError(fmt.Sprintf("Failed to marshal result: %v", err))
		os.Exit(1)
	}

	fmt.Println(string(resultJSON))
	os.Exit(0)
}

// runStandaloneMode runs the original Asynq queue consumer
func runStandaloneMode() {
	log.Println("VideoAgent Worker starting...")

	// Load configuration from environment
	config := loadConfig()

	// Initialize components
	ctx := context.Background()

	// 1. FFmpeg helper
	ffmpeg, err := utils.NewFFmpegHelper(config.TempDir)
	if err != nil {
		log.Fatalf("Failed to initialize FFmpeg: %v", err)
	}
	log.Println("✓ FFmpeg initialized")

	// 2. MageAgent client (zero hardcoded models)
	mageAgent := clients.NewMageAgentClient(config.MageAgentURL, 60*time.Second)
	if err := mageAgent.HealthCheck(ctx); err != nil {
		log.Printf("WARNING: MageAgent health check failed: %v", err)
	} else {
		log.Println("✓ MageAgent connection established")
	}

	// 2b. GraphRAG client (for VoyageAI embeddings)
	graphragClient, err := clients.NewGraphRAGClient(config.GraphRAGURL)
	if err != nil {
		log.Fatalf("Failed to initialize GraphRAG client: %v", err)
	}
	log.Println("✓ GraphRAG client initialized (VoyageAI voyage-3, 1024-D)")

	// 2c. Nexus-Auth client for YouTube OAuth tokens (best effort)
	var authClient *clients.NexusAuthClient
	if config.NexusAuthURL != "" && config.InternalServiceAPIKey != "" {
		authClient, err = clients.NewNexusAuthClient(config.NexusAuthURL, config.InternalServiceAPIKey)
		if err != nil {
			log.Printf("WARNING: Failed to initialize Nexus-Auth client: %v", err)
			log.Println("YouTube authenticated downloads will be disabled")
		} else {
			if err := authClient.HealthCheck(ctx); err != nil {
				log.Printf("WARNING: Nexus-Auth health check failed: %v", err)
			} else {
				log.Println("✓ Nexus-Auth client initialized (YouTube OAuth enabled)")
			}
		}
	} else {
		log.Println("INFO: Nexus-Auth not configured, YouTube authenticated downloads disabled")
	}

	// 3. Storage manager (PostgreSQL + Qdrant)
	storageManager, err := storage.NewStorageManager(
		config.PostgresURL,
		config.QdrantURL,
		config.QdrantCollection,
	)
	if err != nil {
		log.Fatalf("Failed to initialize storage: %v", err)
	}
	defer storageManager.Close()
	log.Println("✓ Storage manager initialized (PostgreSQL + Qdrant)")

	// 4. Redis client for progress updates
	// Parse Redis URL properly (handles "redis://host:port" format)
	redisOpts, err := redis.ParseURL(config.RedisURL)
	if err != nil {
		log.Fatalf("Failed to parse Redis URL: %v", err)
	}

	redisClient := redis.NewClient(redisOpts)
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	log.Println("✓ Redis connection established")

	// 5. Similarity module (video/scene embeddings + search)
	similarityModule, err := similarity.InitializeSimilarityModule(
		mageAgent,
		graphragClient,
		config.QdrantURL,
		"", // No API key for local Qdrant
	)
	if err != nil {
		log.Fatalf("Failed to initialize similarity module: %v", err)
	}

	// Initialize Qdrant collections (1024-D vectors)
	if err := similarityModule.InitializeCollections(ctx); err != nil {
		log.Printf("WARNING: Failed to initialize Qdrant collections: %v", err)
		log.Println("  Collections may already exist or Qdrant may be unavailable")
	}

	// 6. Video processor
	videoProcessor := processor.NewVideoProcessor(
		ffmpeg,
		mageAgent,
		storageManager,
		redisClient,
		config.WorkerConcurrency,
	)

	// Configure video processor with YouTube auth client if available
	if authClient != nil {
		videoProcessor.SetYouTubeAuthClient(authClient)
		log.Println("✓ Video processor configured with YouTube OAuth authentication")
	}

	log.Println("✓ Video processor initialized")

	// 7. Queue consumer
	queueConsumer, err := queue.NewRedisConsumer(&queue.RedisConsumerConfig{
		RedisURL:    config.RedisURL,
		Concurrency: config.WorkerConcurrency,
		Processor:   videoProcessor,
	})
	if err != nil {
		log.Fatalf("Failed to initialize queue consumer: %v", err)
	}
	log.Println("✓ Queue consumer initialized")

	// Setup graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Start consumer in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := queueConsumer.Start(); err != nil {
			errChan <- err
		}
	}()

	log.Println("✓ VideoAgent Worker ready - waiting for jobs...")
	log.Printf("  - Concurrency: %d workers", config.WorkerConcurrency)
	log.Printf("  - Temp directory: %s", config.TempDir)
	log.Printf("  - MageAgent URL: %s", config.MageAgentURL)

	// Wait for shutdown signal or error
	select {
	case <-sigChan:
		log.Println("Shutdown signal received, stopping gracefully...")
		queueConsumer.Stop()
	case err := <-errChan:
		log.Fatalf("Worker error: %v", err)
	}

	log.Println("VideoAgent Worker stopped")
}

// sendError sends error response to stdout as JSON
func sendError(message string) {
	errorResponse := map[string]interface{}{
		"error": message,
		"success": false,
	}
	errorJSON, _ := json.Marshal(errorResponse)
	fmt.Println(string(errorJSON))
}

// loadConfig loads configuration from environment variables
func loadConfig() models.Config {
	config := models.Config{
		RedisURL:              getEnv("REDIS_URL", "redis://localhost:6379"),
		PostgresURL:           getEnv("POSTGRES_URL", "postgresql://unified_brain:graphrag123@localhost:5432/nexus_graphrag?sslmode=disable"),
		QdrantURL:             getEnv("QDRANT_URL", "localhost"),
		QdrantCollection:      getEnv("QDRANT_COLLECTION", "videoagent_frames"),
		MageAgentURL:          getEnv("MAGEAGENT_URL", "http://localhost:3000"),
		GraphRAGURL:           getEnv("GRAPHRAG_URL", "http://localhost:8090"),
		NexusAuthURL:          getEnv("NEXUS_AUTH_URL", "http://nexus-auth:8080"),
		InternalServiceAPIKey: getEnv("INTERNAL_SERVICE_API_KEY", ""),
		WorkerConcurrency:     getEnvInt("WORKER_CONCURRENCY", 3),
		TempDir:               getEnv("TEMP_DIR", "/tmp/videoagent"),
		MaxVideoSize:          getEnvInt64("MAX_VIDEO_SIZE", 2*1024*1024*1024), // 2GB default
		EnableGoogleDrive:     getEnvBool("ENABLE_GOOGLE_DRIVE", true),
	}

	return config
}

// getEnv gets environment variable with default
func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// getEnvInt gets integer environment variable with default
func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var intValue int
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvInt64 gets int64 environment variable with default
func getEnvInt64(key string, defaultValue int64) int64 {
	if value := os.Getenv(key); value != "" {
		var intValue int64
		if _, err := fmt.Sscanf(value, "%d", &intValue); err == nil {
			return intValue
		}
	}
	return defaultValue
}

// getEnvBool gets boolean environment variable with default
func getEnvBool(key string, defaultValue bool) bool {
	if value := os.Getenv(key); value != "" {
		return value == "true" || value == "1" || value == "yes"
	}
	return defaultValue
}
